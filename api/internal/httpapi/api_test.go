package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/auth"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/httpapi"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/seed"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
	"github.com/vinit-tyagi-soft/taskforce/api/migrations"
)

var secret = []byte("test-secret-test-secret-test-secret!")

type env struct {
	t     *testing.T
	srv   *httptest.Server
	owner *pgxpool.Pool
	app   *pgxpool.Pool
	api   *httpapi.Server
	hooks *hookTransport
}

// hookTransport stands in for webhook receivers so tests never reach the network.
type hookTransport struct {
	status   int
	requests []*http.Request
	bodies   [][]byte
}

func (h *hookTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	h.requests = append(h.requests, r)
	h.bodies = append(h.bodies, body)
	return &http.Response{StatusCode: h.status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	ownerURL := envOr("TF_TEST_MIGRATE_DATABASE_URL", "postgres://"+os.Getenv("USER")+"@localhost:5432/taskforce_test?sslmode=disable")
	appURL := envOr("TF_TEST_DATABASE_URL", "postgres://taskforce_app:taskforce_dev@localhost:5432/taskforce_test?sslmode=disable")
	owner, err := store.Open(ctx, ownerURL)
	if err != nil {
		t.Skipf("test database unavailable: %v", err)
	}
	if err := store.Migrate(ctx, owner, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Load(ctx, owner, true); err != nil {
		t.Fatal(err)
	}
	app, err := store.Open(ctx, appURL)
	if err != nil {
		t.Fatalf("app role connection: %v", err)
	}
	hooks := &hookTransport{status: 200}
	api := httpapi.New(&store.Store{Pool: app}, httpapi.Config{SessionSecret: secret, DevAuth: true, WebhookTransport: hooks},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(func() { srv.Close(); app.Close(); owner.Close() })
	return &env{t: t, srv: srv, owner: owner, app: app, api: api, hooks: hooks}
}

func (e *env) id(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.owner.QueryRow(context.Background(), query, args...).Scan(&id); err != nil {
		e.t.Fatalf("lookup %q: %v", query, err)
	}
	return id
}

func (e *env) user(email string) uuid.UUID {
	return e.id(`SELECT id FROM users WHERE email = $1`, email)
}
func (e *env) org(slug string) uuid.UUID {
	return e.id(`SELECT id FROM organizations WHERE slug = $1`, slug)
}
func (e *env) task(title string) uuid.UUID {
	return e.id(`SELECT id FROM tasks WHERE title = $1`, title)
}
func (e *env) project(name string) uuid.UUID {
	return e.id(`SELECT id FROM projects WHERE name = $1`, name)
}

func token(user uuid.UUID) string {
	now := time.Now()
	tok, _ := auth.Sign(secret, auth.Claims{Subject: user.String(), Issuer: auth.Issuer, Audience: auth.Audience,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()})
	return tok
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
}

func (e *env) do(user uuid.UUID, method, path string, body any, headers ...string) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token(user))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out := resp{status: res.StatusCode, header: res.Header, body: map[string]any{}}
	_ = json.NewDecoder(res.Body).Decode(&out.body)
	return out
}

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(map[string]any))
	}
	return out
}

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: got %d want %d (%v)", what, r.status, status, r.body)
	}
}

// T01 + journey D: tenant isolation across IDs, reports and active-tenant switching.
func TestTenantIsolation(t *testing.T) {
	e := setup(t)
	arjun, vinit := e.user("arjun.mehta@example.com"), e.user("vinit@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	vertexTask := e.task("Review partner portal prototype")
	vertexProject := e.project("Partner onboarding portal")
	var vertexWS uuid.UUID = e.id(`SELECT id FROM workspaces WHERE tenant_id = $1`, vertex)

	expect(t, e.do(arjun, "GET", "/api/v1/tasks/"+vertexTask.String(), nil), 404, "cross-tenant task by id")
	expect(t, e.do(arjun, "GET", "/api/v1/projects/"+vertexProject.String(), nil), 404, "cross-tenant project by id")
	expect(t, e.do(arjun, "GET", "/api/v1/projects/"+vertexProject.String()+"/tasks", nil), 404, "cross-tenant task list")
	expect(t, e.do(arjun, "GET", "/api/v1/tasks/"+vertexTask.String()+"/comments", nil), 404, "cross-tenant comments")
	expect(t, e.do(arjun, "GET", "/api/v1/workspaces/"+vertexWS.String()+"/projects", nil), 404, "cross-tenant workspace")
	expect(t, e.do(arjun, "GET", "/api/v1/organizations/"+vertex.String()+"/executive-summary", nil), 404, "cross-tenant report")
	expect(t, e.do(arjun, "GET", "/api/v1/me/work-items?org="+vertex.String(), nil), 404, "cross-tenant work context")
	expect(t, e.do(arjun, "PATCH", "/api/v1/tasks/"+vertexTask.String(), map[string]any{"status": "DONE"}, "If-Match", `"1"`), 404, "cross-tenant write")

	// Multi-org user: the active tenant decides which organization's work appears.
	acmeWork := items(e.do(vinit, "GET", "/api/v1/me/work-items?org="+acme.String(), nil))
	vertexWork := items(e.do(vinit, "GET", "/api/v1/me/work-items?org="+vertex.String(), nil))
	for _, it := range acmeWork {
		if tid, _ := it["tenant_id"].(string); tid == vertex.String() {
			t.Fatalf("Vertex task leaked into Acme context: %v", it["title"])
		}
	}
	found := false
	for _, it := range vertexWork {
		if tid, _ := it["tenant_id"].(string); tid == acme.String() {
			t.Fatalf("Acme task leaked into Vertex context: %v", it["title"])
		}
		if it["title"] == "Review partner portal prototype" {
			found = true
		}
	}
	if !found {
		t.Fatal("Vertex assignment missing from Vertex context")
	}
}

// T02: private personal work never reaches employers, reports or other users.
func TestPersonalPrivacy(t *testing.T) {
	e := setup(t)
	vinit, meera := e.user("vinit@example.com"), e.user("meera.iyer@example.com")
	acme := e.org("acme")
	private := e.task("Read a chapter on leadership")
	personalWS := e.id(`SELECT id FROM workspaces WHERE kind = 'PERSONAL' AND owner_user_id = $1`, vinit)

	expect(t, e.do(meera, "GET", "/api/v1/tasks/"+private.String(), nil), 404, "executive reading private task")
	expect(t, e.do(meera, "GET", "/api/v1/workspaces/"+personalWS.String()+"/projects", nil), 404, "executive listing private projects")

	summary := e.do(meera, "GET", "/api/v1/organizations/"+acme.String()+"/executive-summary", nil)
	expect(t, summary, 200, "executive summary")
	raw, _ := json.Marshal(summary.body)
	for _, leak := range []string{"Leadership learning journal", "Half marathon preparation", "Read a chapter on leadership", "Partner onboarding portal"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("executive summary leaked %q", leak)
		}
	}
	var acmeTasks int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM tasks WHERE tenant_id = $1 AND status <> 'CANCELLED'`, acme).Scan(&acmeTasks)
	metrics := summary.body["metrics"].(map[string]any)
	if int(metrics["eligible"].(float64)) != acmeTasks {
		t.Fatalf("eligible = %v, want %d org-only tasks", metrics["eligible"], acmeTasks)
	}

	// The owner still sees their private work in My Work.
	mine := items(e.do(vinit, "GET", "/api/v1/me/work-items?org="+acme.String(), nil))
	seen := false
	for _, it := range mine {
		if it["id"] == private.String() {
			seen = true
		}
	}
	if !seen {
		t.Fatal("private task missing from owner's My Work")
	}

	// Defense in depth: even an unfiltered query as the app role cannot see it.
	ctx := context.Background()
	s := &store.Store{Pool: e.app}
	var visible int
	err := s.InTx(ctx, store.Scope{UserID: meera}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE workspace_id = $1`, personalWS).Scan(&visible)
	})
	if err != nil || visible != 0 {
		t.Fatalf("RLS exposed %d private tasks (err %v)", visible, err)
	}
}

// T03: role and project scope limit writes, assignment and audit access.
func TestPrivilegeBoundaries(t *testing.T) {
	e := setup(t)
	ishaan, meera, arjun := e.user("ishaan.rao@example.com"), e.user("meera.iyer@example.com"), e.user("arjun.mehta@example.com")
	acme := e.org("acme")
	arjunsTask := e.task("Complete API integration tests")
	p1, p2 := e.project("Enterprise workspace launch"), e.project("Zero Trust rollout")

	expect(t, e.do(ishaan, "PATCH", "/api/v1/tasks/"+arjunsTask.String(), map[string]any{"status": "DONE"}, "If-Match", `"1"`), 403, "contributor editing another owner's task")
	expect(t, e.do(ishaan, "POST", "/api/v1/projects/"+p1.String()+"/tasks", map[string]any{"title": "Assign to Arjun", "owner_id": arjun}), 403, "contributor assigning others")
	expect(t, e.do(ishaan, "POST", "/api/v1/projects/"+p2.String()+"/tasks", map[string]any{"title": "Sneak in"}), 404, "non-member creating in project")
	expect(t, e.do(ishaan, "POST", "/api/v1/projects/"+p1.String()+"/tasks", map[string]any{"title": "My own follow-up"}), 201, "contributor creating own task")
	expect(t, e.do(meera, "PATCH", "/api/v1/tasks/"+arjunsTask.String(), map[string]any{"status": "DONE"}, "If-Match", `"1"`), 403, "executive is read-only")
	expect(t, e.do(ishaan, "GET", "/api/v1/organizations/"+acme.String()+"/audit-events", nil), 403, "member reading audit")
	expect(t, e.do(ishaan, "GET", "/api/v1/organizations/"+acme.String()+"/executive-summary", nil), 403, "member reading executive report")
	expect(t, e.do(arjun, "PATCH", "/api/v1/tasks/"+arjunsTask.String(), map[string]any{"owner_id": ishaan}, "If-Match", `"1"`), 200, "project manager reassigning")
}

// T04: suspension applies to existing sessions.
func TestSuspensionRevokesAccess(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	arjun, acme := e.user("arjun.mehta@example.com"), e.org("acme")
	expect(t, e.do(arjun, "GET", "/api/v1/organizations/"+acme.String(), nil), 200, "active member")
	_, _ = e.owner.Exec(ctx, `UPDATE org_memberships SET state = 'SUSPENDED' WHERE org_id = $1 AND user_id = $2`, acme, arjun)
	expect(t, e.do(arjun, "GET", "/api/v1/organizations/"+acme.String(), nil), 404, "suspended membership")
	_, _ = e.owner.Exec(ctx, `UPDATE users SET state = 'SUSPENDED' WHERE id = $1`, arjun)
	expect(t, e.do(arjun, "GET", "/api/v1/me", nil), 401, "suspended account")
}

// T06 + lifecycle: optimistic concurrency and server-enforced transitions.
func TestConcurrencyAndTransitions(t *testing.T) {
	e := setup(t)
	vinit := e.user("vinit@example.com")
	task := e.task("Review enterprise authentication flow")
	path := "/api/v1/tasks/" + task.String()

	expect(t, e.do(vinit, "PATCH", path, map[string]any{"priority": "URGENT"}), 428, "missing If-Match")
	r := e.do(vinit, "GET", path, nil)
	v := r.header.Get("ETag")
	expect(t, e.do(vinit, "PATCH", path, map[string]any{"status": "IN_REVIEW"}, "If-Match", v), 200, "fresh version")
	expect(t, e.do(vinit, "PATCH", path, map[string]any{"status": "DONE"}, "If-Match", v), 412, "stale version")

	blocked := e.task("Resolve identity provider dependency")
	bpath := "/api/v1/tasks/" + blocked.String()
	bv := e.do(vinit, "GET", bpath, nil).header.Get("ETag")
	r = e.do(vinit, "PATCH", bpath, map[string]any{"status": "DONE"}, "If-Match", bv)
	expect(t, r, 422, "blocked straight to done")
	if r.body["code"] != "INVALID_TRANSITION" {
		t.Fatalf("code = %v", r.body["code"])
	}

	done := e.task("Approve architecture decisions")
	dpath := "/api/v1/tasks/" + done.String()
	dv := e.do(vinit, "GET", dpath, nil).header.Get("ETag")
	expect(t, e.do(vinit, "PATCH", dpath, map[string]any{"status": "TODO"}, "If-Match", dv), 200, "reopen")
	var n int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'task.reopened' AND target_id = $1`, done).Scan(&n)
	if n != 1 {
		t.Fatalf("reopen audit events = %d", n)
	}
	if _, err := e.owner.Exec(context.Background(), `DELETE FROM audit_events`); err == nil {
		t.Fatal("audit events must be append-only")
	}
}

// T08: retried creates with the same Idempotency-Key do not duplicate work.
func TestIdempotentCreate(t *testing.T) {
	e := setup(t)
	vinit := e.user("vinit@example.com")
	p1 := e.project("Enterprise workspace launch")
	path := "/api/v1/projects/" + p1.String() + "/tasks"
	body := map[string]any{"title": "Review pilot authorization tests", "priority": "HIGH", "due_on": "2026-11-10"}

	first := e.do(vinit, "POST", path, body, "Idempotency-Key", "retry-key-123")
	expect(t, first, 201, "first create")
	second := e.do(vinit, "POST", path, body, "Idempotency-Key", "retry-key-123")
	expect(t, second, 200, "retried create")
	if first.body["id"] != second.body["id"] {
		t.Fatal("retry created a duplicate task")
	}
	body["title"] = "Something else"
	expect(t, e.do(vinit, "POST", path, body, "Idempotency-Key", "retry-key-123"), 422, "key reuse with different body")
	var n int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM tasks WHERE title = 'Review pilot authorization tests'`).Scan(&n)
	if n != 1 {
		t.Fatalf("tasks created = %d", n)
	}
	if loc := first.header.Get("Location"); !strings.HasSuffix(loc, first.body["id"].(string)) {
		t.Fatalf("Location = %q", loc)
	}
}
