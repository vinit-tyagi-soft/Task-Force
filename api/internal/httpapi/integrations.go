package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

const (
	apiTokenPrefix    = "tf_pat_"
	webhookSecretPref = "whsec_"
	maxTokenDays      = 365
)

// WebhookEvents are the audit actions an endpoint may subscribe to. Only
// organization events are ever delivered; personal workspace activity is not.
var WebhookEvents = []string{
	"task.created", "task.updated", "task.status_changed", "task.reopened", "task.assigned", "comment.created",
	"project.created", "project.updated", "project.archived", "project.restored",
}

// API tokens reach only organization delivery data: never personal workspaces,
// conversations, notifications, administration or token management.
var tokenRoutes = func() []*regexp.Regexp {
	id := `[0-9a-fA-F-]{36}`
	var out []*regexp.Regexp
	for _, p := range []string{
		`^/api/v1/organizations/` + id + `(/(members|departments|teams|workload|executive-summary|reports/tasks|reports/tasks\.csv))?$`,
		`^/api/v1/workspaces/` + id + `/projects$`,
		`^/api/v1/projects/` + id + `(/(members|tasks|board))?$`,
		`^/api/v1/tasks/` + id + `(/comments)?$`,
	} {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}()

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func randomSecret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// tokenActor authenticates an API token and narrows the actor to the token's org.
func (s *Server) tokenActor(r *http.Request, token string) (*authz.Actor, error) {
	unauth := &httpError{401, "UNAUTHENTICATED", "The API token is invalid, expired or revoked.", nil}
	var user, tenant uuid.UUID
	var scope string
	err := s.store.Pool.QueryRow(r.Context(), `SELECT user_id, tenant_id, scope FROM tf_auth_use_token($1)`, hashToken(token)).Scan(&user, &tenant, &scope)
	if store.IsNoRows(err) {
		return nil, unauth
	}
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(tokenRoutes, func(re *regexp.Regexp) bool { return re.MatchString(r.URL.Path) }) {
		return nil, &httpError{403, "TOKEN_SCOPE", "API tokens cannot use this endpoint.", nil}
	}
	switch {
	case r.Method == http.MethodGet:
	case scope == "write" && (r.Method == http.MethodPost || r.Method == http.MethodPatch):
	default:
		return nil, &httpError{403, "TOKEN_SCOPE", "This API token does not allow " + r.Method + " requests.", nil}
	}
	actor, err := s.loadActor(r.Context(), user)
	if err == errInactive {
		return nil, unauth
	}
	if err != nil {
		return nil, err
	}
	roles, ok := actor.Roles[tenant]
	if !ok {
		return nil, unauth
	}
	actor.Roles = map[uuid.UUID][]authz.Role{tenant: roles}
	actor.TokenScoped = true
	return actor, nil
}

func (s *Server) integrationScope(w http.ResponseWriter, r *http.Request) (*authz.Actor, uuid.UUID, store.Scope, bool) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err == nil {
		var scope store.Scope
		if scope, err = orgScope(actor, org); err == nil {
			if actor.Can(org, authz.IntegrationManage) {
				return actor, org, scope, true
			}
			err = errForbidden("Only organization administrators can manage integrations.")
		}
	}
	s.fail(w, r, err)
	return nil, org, store.Scope{}, false
}

type tokenJSON struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"`
	Owner      *userJSON  `json:"owner"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Token      string     `json:"token,omitempty"`
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.integrationScope(w, r)
	if !ok {
		return
	}
	out := []tokenJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, `SELECT id, name, prefix, scope, user_id, expires_at, last_used_at, revoked_at, created_at
			FROM api_tokens WHERE tenant_id = $1 ORDER BY created_at DESC`, org)
		if err != nil {
			return err
		}
		var owners []uuid.UUID
		for rows.Next() {
			var t tokenJSON
			var owner uuid.UUID
			if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.Scope, &owner, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt, &t.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			owners = append(owners, owner)
			out = append(out, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		users, err := loadUsers(ctx, tx, owners)
		if err != nil {
			return err
		}
		for i := range out {
			if u, ok := users[owners[i]]; ok {
				out[i].Owner = &u
			}
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

// createToken issues a token acting as the caller within this org. The secret is
// returned once; only its SHA-256 hash is stored.
func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.integrationScope(w, r)
	if !ok {
		return
	}
	var in struct {
		Name          string `json:"name"`
		Scope         string `json:"scope"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		errs["name"] = "Name this token (up to 80 characters)."
	}
	if in.Scope != "read" && in.Scope != "write" {
		errs["scope"] = "Choose read or write."
	}
	if in.ExpiresInDays < 1 || in.ExpiresInDays > maxTokenDays {
		errs["expires_in_days"] = "Tokens expire within 1 to 365 days."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	secret, err := randomSecret(apiTokenPrefix)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := tokenJSON{Name: in.Name, Scope: in.Scope, Prefix: secret[:len(apiTokenPrefix)+6], Token: secret}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `INSERT INTO api_tokens (tenant_id, user_id, name, token_hash, prefix, scope, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(days => $7)) RETURNING id, expires_at, created_at`,
			org, actor.UserID, in.Name, hashToken(secret), out.Prefix, in.Scope, in.ExpiresInDays).Scan(&out.ID, &out.ExpiresAt, &out.CreatedAt); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "api_token.created", TargetType: "api_token", TargetID: &out.ID,
			Summary: "created " + in.Scope + " API token " + in.Name, Metadata: map[string]any{"expires_in_days": in.ExpiresInDays}})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "tokenId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		var org uuid.UUID
		var name string
		if err := tx.QueryRow(ctx, `SELECT tenant_id, name FROM api_tokens WHERE id = $1`, id).Scan(&org, &name); err != nil {
			if store.IsNoRows(err) {
				return errNotFound()
			}
			return err
		}
		if !actor.IsMember(org) {
			return errNotFound()
		}
		if !actor.Can(org, authz.IntegrationManage) {
			return errForbidden("Only organization administrators can manage integrations.")
		}
		if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "api_token.revoked", TargetType: "api_token", TargetID: &id,
			Summary: "revoked API token " + name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type webhookJSON struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Secret    string    `json:"secret,omitempty"`
}

const webhookSelect = `SELECT id, name, url, events, enabled, version, created_at FROM webhook_endpoints`

func scanWebhook(row pgx.Row) (webhookJSON, error) {
	var h webhookJSON
	err := row.Scan(&h.ID, &h.Name, &h.URL, &h.Events, &h.Enabled, &h.Version, &h.CreatedAt)
	return h, err
}

type webhookInput struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Enabled *bool    `json:"enabled"`
}

func (in *webhookInput) validate() error {
	errs := map[string]string{}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		errs["name"] = "Name this endpoint (up to 80 characters)."
	}
	if msg := validateWebhookURL(in.URL); msg != "" {
		errs["url"] = msg
	}
	in.Events = slices.Compact(slices.Sorted(slices.Values(in.Events)))
	if len(in.Events) == 0 {
		errs["events"] = "Subscribe to at least one event."
	}
	for _, e := range in.Events {
		if !slices.Contains(WebhookEvents, e) {
			errs["events"] = "Unknown event " + strconv.Quote(e) + "."
		}
	}
	if len(errs) > 0 {
		return errValidation(errs)
	}
	return nil
}

func validateWebhookURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil || len(raw) > 500:
		return "Use a valid URL (up to 500 characters)."
	case u.Scheme != "https":
		return "Webhook endpoints must use https://."
	case u.Host == "" || u.User != nil:
		return "Use a host without embedded credentials."
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !isPublicIP(ip) {
		return "Webhook endpoints must be publicly reachable."
	}
	if h := strings.ToLower(u.Hostname()); h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") {
		return "Webhook endpoints must be publicly reachable."
	}
	return ""
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || cgnat.Contains(ip))
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.integrationScope(w, r)
	if !ok {
		return
	}
	out := []webhookJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), webhookSelect+` WHERE tenant_id = $1 ORDER BY created_at`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h, err := scanWebhook(rows)
			if err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out, "events": WebhookEvents})
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.integrationScope(w, r)
	if !ok {
		return
	}
	var in webhookInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	secret, err := randomSecret(webhookSecretPref)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var out webhookJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var err error
		out, err = scanWebhook(tx.QueryRow(ctx, `INSERT INTO webhook_endpoints (tenant_id, name, url, events, secret, enabled, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, name, url, events, enabled, version, created_at`,
			org, in.Name, strings.TrimSpace(in.URL), in.Events, secret, in.Enabled == nil || *in.Enabled, actor.UserID))
		if err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "webhook.created", TargetType: "webhook", TargetID: &out.ID,
			Summary: "added webhook " + in.Name, Metadata: map[string]any{"events": in.Events}})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out.Secret = secret
	setETag(w, out.Version)
	writeJSON(w, r, 201, out)
}

func webhookForWrite(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID, version int) (uuid.UUID, webhookJSON, error) {
	var org uuid.UUID
	var h webhookJSON
	err := tx.QueryRow(ctx, `SELECT tenant_id, id, name, url, events, enabled, version, created_at FROM webhook_endpoints WHERE id = $1`, id).
		Scan(&org, &h.ID, &h.Name, &h.URL, &h.Events, &h.Enabled, &h.Version, &h.CreatedAt)
	switch {
	case store.IsNoRows(err):
		return org, h, errNotFound()
	case err != nil:
		return org, h, err
	case !actor.IsMember(org):
		return org, h, errNotFound()
	case !actor.Can(org, authz.IntegrationManage):
		return org, h, errForbidden("Only organization administrators can manage integrations.")
	case version >= 0 && h.Version != version:
		return org, h, errVersionConflict()
	}
	return org, h, nil
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "webhookId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in webhookInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var out webhookJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, before, err := webhookForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		enabled := before.Enabled
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		out, err = scanWebhook(tx.QueryRow(ctx, `UPDATE webhook_endpoints SET name = $1, url = $2, events = $3, enabled = $4,
			version = version + 1, updated_at = now() WHERE id = $5 AND version = $6
			RETURNING id, name, url, events, enabled, version, created_at`, in.Name, strings.TrimSpace(in.URL), in.Events, enabled, id, version))
		if store.IsNoRows(err) {
			return errVersionConflict()
		}
		if err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "webhook.updated", TargetType: "webhook", TargetID: &id,
			Summary: "updated webhook " + in.Name, Metadata: map[string]any{"events": in.Events, "enabled": enabled}})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out.Version)
	writeJSON(w, r, 200, out)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "webhookId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, h, err := webhookForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "webhook.deleted", TargetType: "webhook", TargetID: &id,
			Summary: "removed webhook " + h.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testWebhook queues a ping delivery; the worker sends it like any other event.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "webhookId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, _, err := webhookForWrite(ctx, tx, actor, id, -1)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"event": "ping", "occurred_at": s.now().UTC(), "organization_id": org, "actor_id": actor.UserID})
		_, err = tx.Exec(ctx, `INSERT INTO webhook_deliveries (tenant_id, endpoint_id, event, payload) VALUES ($1, $2, 'ping', $3)`, org, id, payload)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 202, map[string]string{"status": "queued"})
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.integrationScope(w, r)
	if !ok {
		return
	}
	type deliveryJSON struct {
		ID           uuid.UUID  `json:"id"`
		Endpoint     string     `json:"endpoint"`
		Event        string     `json:"event"`
		Status       string     `json:"status"`
		Attempts     int        `json:"attempts"`
		ResponseCode *int       `json:"response_code"`
		Error        *string    `json:"error"`
		CreatedAt    time.Time  `json:"created_at"`
		DeliveredAt  *time.Time `json:"delivered_at"`
	}
	out := []deliveryJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT d.id, e.name, d.event, d.status, d.attempts, d.response_code, d.error, d.created_at, d.delivered_at
			FROM webhook_deliveries d JOIN webhook_endpoints e ON e.id = d.endpoint_id
			WHERE d.tenant_id = $1 ORDER BY d.created_at DESC LIMIT 50`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d deliveryJSON
			if err := rows.Scan(&d.ID, &d.Endpoint, &d.Event, &d.Status, &d.Attempts, &d.ResponseCode, &d.Error, &d.CreatedAt, &d.DeliveredAt); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

// enqueueWebhooks adds an outbox delivery for every enabled endpoint subscribed to
// an organization event, inside the same transaction as the change.
func enqueueWebhooks(ctx context.Context, tx pgx.Tx, e auditEntry, at time.Time) error {
	if e.Tenant == nil || !slices.Contains(WebhookEvents, e.Action) {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"event": e.Action, "occurred_at": at.UTC(), "organization_id": e.Tenant, "actor_id": e.Actor,
		"target": map[string]any{"type": e.TargetType, "id": e.TargetID}, "summary": e.Summary, "metadata": e.Metadata,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO webhook_deliveries (tenant_id, endpoint_id, event, payload)
		SELECT $1, id, $2, $3 FROM webhook_endpoints WHERE tenant_id = $1 AND enabled AND $2 = ANY(events)`, e.Tenant, e.Action, payload)
	return err
}

// guardedTransport refuses connections to non-public addresses after DNS
// resolution, so webhooks cannot be aimed at internal services (SSRF).
func guardedTransport() http.RoundTripper {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip == nil || !isPublicIP(ip) {
			return errors.New("destination is not a public address")
		}
		return nil
	}}
	return &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConns: 10}
}

// Sign returns the X-TaskForce-Signature value for a delivery body.
func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// DeliverWebhooks sends one batch of due deliveries and reports how many were attempted.
func (s *Server) DeliverWebhooks(ctx context.Context) (int, error) {
	type job struct {
		id                 uuid.UUID
		url, secret, event string
		payload            []byte
		attempts           int
	}
	rows, err := s.store.Pool.Query(ctx, `SELECT id, url, secret, event, payload, attempts FROM tf_webhook_claim(20)`)
	if err != nil {
		return 0, err
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.url, &j.secret, &j.event, &j.payload, &j.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, j := range jobs {
		code, sendErr := s.sendWebhook(ctx, j.id, j.url, j.secret, j.event, j.payload)
		ok := sendErr == nil && code >= 200 && code < 300
		msg := ""
		if sendErr != nil {
			msg = sendErr.Error()
		} else if !ok {
			msg = "endpoint responded " + strconv.Itoa(code)
		}
		var codeArg *int
		if code != 0 {
			codeArg = &code
		}
		if _, err := s.store.Pool.Exec(ctx, `SELECT tf_webhook_complete($1, $2, $3, $4)`, j.id, ok, codeArg, msg); err != nil {
			return len(jobs), err
		}
	}
	return len(jobs), nil
}

func (s *Server) sendWebhook(ctx context.Context, id uuid.UUID, target, secret, event string, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	ts := s.now().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TaskForce-Webhooks/1")
	req.Header.Set("X-TaskForce-Event", event)
	req.Header.Set("X-TaskForce-Delivery", id.String())
	req.Header.Set("X-TaskForce-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-TaskForce-Signature", Sign(secret, ts, body))
	res, err := s.webhookClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	return res.StatusCode, nil
}

// RunWebhookWorker delivers webhooks until ctx is cancelled.
func (s *Server) RunWebhookWorker(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.DeliverWebhooks(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("webhook delivery failed", "error", err)
			} else if n > 0 {
				s.log.Info("webhooks attempted", "count", n)
			}
		}
	}
}
