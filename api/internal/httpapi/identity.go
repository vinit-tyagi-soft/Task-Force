package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/auth"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

func actorFrom(ctx context.Context) *authz.Actor {
	a, _ := ctx.Value(actorKey).(*authz.Actor)
	return a
}

// authenticated verifies the session, then re-checks live account and membership
// state on every request so suspension and role changes apply to existing sessions.
func (s *Server) authenticated(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		fromCookie := false
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			token = strings.TrimSpace(bearer)
			if strings.HasPrefix(token, apiTokenPrefix) {
				actor, err := s.tokenActor(r, token)
				if err != nil {
					s.fail(w, r, err)
					return
				}
				h(w, r.WithContext(context.WithValue(r.Context(), actorKey, actor)))
				return
			}
		} else if c, err := r.Cookie(auth.CookieName); err == nil {
			token, fromCookie = c.Value, true
		}
		claims, err := auth.Verify(s.cfg.SessionSecret, token, s.now())
		if err != nil {
			writeError(w, r, 401, "UNAUTHENTICATED", "Sign in to continue.", nil)
			return
		}
		// Cookie sessions must prove same-site intent on state-changing requests.
		if fromCookie && r.Method != http.MethodGet && r.Header.Get("X-TaskForce-Client") != "web" {
			writeError(w, r, 403, "CSRF_REJECTED", "Missing client header.", nil)
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			writeError(w, r, 401, "UNAUTHENTICATED", "Sign in to continue.", nil)
			return
		}
		actor, err := s.loadActor(r.Context(), userID)
		if err != nil {
			if err == errInactive {
				writeError(w, r, 401, "UNAUTHENTICATED", "Your account is not active.", nil)
				return
			}
			s.fail(w, r, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), actorKey, actor)))
	})
}

var errInactive = &httpError{401, "UNAUTHENTICATED", "Your account is not active.", nil}

func (s *Server) loadActor(ctx context.Context, userID uuid.UUID) (*authz.Actor, error) {
	actor := &authz.Actor{UserID: userID, Roles: map[uuid.UUID][]authz.Role{}}
	err := s.store.InTx(ctx, store.Scope{UserID: userID}, func(tx pgx.Tx) error {
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM users WHERE id = $1`, userID).Scan(&state); err != nil {
			if store.IsNoRows(err) {
				return errInactive
			}
			return err
		}
		if state != "ACTIVE" {
			return errInactive
		}
		rows, err := tx.Query(ctx, `
			SELECT m.org_id, rb.role_key
			FROM org_memberships m
			JOIN organizations o ON o.id = m.org_id AND o.status = 'ACTIVE'
			LEFT JOIN role_bindings rb ON rb.tenant_id = m.org_id AND rb.user_id = m.user_id AND rb.scope_type = 'ORG'
			WHERE m.user_id = $1 AND m.state = 'ACTIVE'`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var org uuid.UUID
			var role *string
			if err := rows.Scan(&org, &role); err != nil {
				return err
			}
			if _, ok := actor.Roles[org]; !ok {
				actor.Roles[org] = []authz.Role{}
			}
			if role != nil {
				actor.Roles[org] = append(actor.Roles[org], authz.Role(*role))
			}
		}
		return rows.Err()
	})
	if err == errInactive {
		return nil, errInactive
	}
	return actor, err
}

func (s *Server) devLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := decode(r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	var id uuid.UUID
	var state string
	err := s.store.InTx(r.Context(), store.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT id, state FROM tf_auth_lookup_user($1)`, strings.TrimSpace(body.Email)).Scan(&id, &state)
	})
	if err != nil || state != "ACTIVE" {
		writeError(w, r, 401, "UNAUTHENTICATED", "No active account matches that email.", nil)
		return
	}
	now := s.now()
	token, err := auth.Sign(s.cfg.SessionSecret, auth.Claims{
		Subject: id.String(), Issuer: auth.Issuer, Audience: auth.Audience,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(s.cfg.SessionTTL).Unix(),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_ = s.store.InTx(r.Context(), store.Scope{UserID: id}, func(tx pgx.Tx) error {
		return audit(r.Context(), tx, auditEntry{Actor: id, Action: "session.created", TargetType: "user", TargetID: &id, Summary: "signed in with development login"})
	})
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode,
		Expires: now.Add(s.cfg.SessionTTL),
	})
	writeJSON(w, r, 200, map[string]any{"user_id": id, "expires_at": now.Add(s.cfg.SessionTTL).UTC()})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, Expires: time.Unix(0, 0), MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

type userJSON struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Initials    string    `json:"initials"`
	Title       string    `json:"title"`
	AvatarColor string    `json:"avatar_color"`
}

type orgJSON struct {
	ID                uuid.UUID          `json:"id"`
	Slug              string             `json:"slug"`
	Name              string             `json:"name"`
	ShortName         string             `json:"short_name"`
	Region            string             `json:"region"`
	ReportingTimezone string             `json:"reporting_timezone"`
	AccentColor       string             `json:"accent_color"`
	WorkspaceID       uuid.UUID          `json:"workspace_id"`
	Roles             []authz.Role       `json:"roles"`
	Permissions       []authz.Permission `json:"permissions"`
}

var uiPermissions = []authz.Permission{authz.ProjectCreate, authz.ProjectReadAll, authz.ReportRead, authz.ReportExport, authz.AuditRead, authz.OrgMembersInvite, authz.AIQuery, authz.TeamManage, authz.WorkloadRead, authz.AutomationManage, authz.IntegrationManage, authz.HiringManage}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	var resp struct {
		User struct {
			userJSON
			Email    string `json:"email"`
			Timezone string `json:"timezone"`
		} `json:"user"`
		PersonalWorkspaceID *uuid.UUID `json:"personal_workspace_id"`
		Today               string     `json:"today"`
		Organizations       []orgJSON  `json:"organizations"`
	}
	err := s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		u := &resp.User
		if err := tx.QueryRow(ctx, `SELECT id, display_name, initials, title, avatar_color, email, timezone FROM users WHERE id = $1`, actor.UserID).
			Scan(&u.ID, &u.DisplayName, &u.Initials, &u.Title, &u.AvatarColor, &u.Email, &u.Timezone); err != nil {
			return err
		}
		resp.Today = domain.Today(s.now(), u.Timezone).Format(time.DateOnly)
		var ws uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE kind = 'PERSONAL' AND owner_user_id = $1`, actor.UserID).Scan(&ws); err == nil {
			resp.PersonalWorkspaceID = &ws
		} else if !store.IsNoRows(err) {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT o.id, o.slug, o.name, o.short_name, o.region, o.reporting_timezone, o.accent_color, w.id
			FROM organizations o JOIN workspaces w ON w.tenant_id = o.id AND w.kind = 'ORG'
			ORDER BY o.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		resp.Organizations = []orgJSON{}
		for rows.Next() {
			var o orgJSON
			if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.ShortName, &o.Region, &o.ReportingTimezone, &o.AccentColor, &o.WorkspaceID); err != nil {
				return err
			}
			if !actor.IsMember(o.ID) {
				continue
			}
			o.Roles = actor.Roles[o.ID]
			o.Permissions = []authz.Permission{}
			for _, p := range uiPermissions {
				if actor.Can(o.ID, p) {
					o.Permissions = append(o.Permissions, p)
				}
			}
			resp.Organizations = append(resp.Organizations, o)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, resp)
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	type wsJSON struct {
		ID       uuid.UUID  `json:"id"`
		Kind     string     `json:"kind"`
		Name     string     `json:"name"`
		TenantID *uuid.UUID `json:"tenant_id"`
	}
	out := []wsJSON{}
	err := s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, kind, name, tenant_id FROM workspaces ORDER BY kind DESC, name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x wsJSON
			if err := rows.Scan(&x.ID, &x.Kind, &x.Name, &x.TenantID); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

// orgScope verifies the caller is an active member before any tenant query runs.
func orgScope(actor *authz.Actor, org uuid.UUID) (store.Scope, error) {
	if !actor.IsMember(org) {
		return store.Scope{}, errNotFound()
	}
	return store.Scope{UserID: actor.UserID, TenantID: &org}, nil
}

func (s *Server) getOrganization(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	scope, err := orgScope(actor, org)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var o orgJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			SELECT o.id, o.slug, o.name, o.short_name, o.region, o.reporting_timezone, o.accent_color, w.id
			FROM organizations o JOIN workspaces w ON w.tenant_id = o.id AND w.kind = 'ORG' WHERE o.id = $1`, org).
			Scan(&o.ID, &o.Slug, &o.Name, &o.ShortName, &o.Region, &o.ReportingTimezone, &o.AccentColor, &o.WorkspaceID)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	o.Roles = actor.Roles[org]
	o.Permissions = []authz.Permission{}
	for _, p := range uiPermissions {
		if actor.Can(org, p) {
			o.Permissions = append(o.Permissions, p)
		}
	}
	writeJSON(w, r, 200, o)
}

func (s *Server) orgMembers(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	scope, err := orgScope(actor, org)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type memberJSON struct {
		userJSON
		State string   `json:"state"`
		Roles []string `json:"roles"`
	}
	out := []memberJSON{}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT u.id, u.display_name, u.initials, u.title, u.avatar_color, m.state,
			       coalesce(array_agg(rb.role_key ORDER BY rb.role_key) FILTER (WHERE rb.role_key IS NOT NULL), '{}')
			FROM org_memberships m
			JOIN users u ON u.id = m.user_id
			LEFT JOIN role_bindings rb ON rb.tenant_id = m.org_id AND rb.user_id = m.user_id AND rb.scope_type = 'ORG'
			WHERE m.org_id = $1 AND m.state <> 'REMOVED'
			GROUP BY u.id, m.state ORDER BY u.display_name`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m memberJSON
			if err := rows.Scan(&m.ID, &m.DisplayName, &m.Initials, &m.Title, &m.AvatarColor, &m.State, &m.Roles); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

func (s *Server) orgDepartments(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	scope, err := orgScope(actor, org)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type deptJSON struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	out := []deptJSON{}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, name FROM departments WHERE tenant_id = $1 ORDER BY name`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d deptJSON
			if err := rows.Scan(&d.ID, &d.Name); err != nil {
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

// loadUsers returns display records for ids visible under RLS (self or shared org).
func loadUsers(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]userJSON, error) {
	out := map[uuid.UUID]userJSON{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, display_name, initials, title, avatar_color FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u userJSON
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Initials, &u.Title, &u.AvatarColor); err != nil {
			return nil, err
		}
		out[u.ID] = u
	}
	return out, rows.Err()
}
