package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/httpapi"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

// Reports are org-only, exports are permissioned, audited and formula-safe, saved views are private.
func TestReportsAndExports(t *testing.T) {
	e := setup(t)
	vinit, meera, ishaan := e.user("vinit@example.com"), e.user("meera.iyer@example.com"), e.user("ishaan.rao@example.com")
	acme := e.org("acme")
	base := "/api/v1/organizations/" + acme.String()
	p1 := e.project("Enterprise workspace launch")
	expect(t, e.do(vinit, "POST", "/api/v1/projects/"+p1.String()+"/tasks", map[string]any{"title": "=HYPERLINK(\"http://x\")", "due_on": "2026-10-30"}), 201, "task with formula title")

	expect(t, e.do(ishaan, "GET", base+"/reports/tasks", nil), 403, "member reading reports")
	rep := e.do(meera, "GET", base+"/reports/tasks?period=30", nil)
	expect(t, rep, 200, "executive report")
	if strings.Contains(fmtBody(rep), "Read a chapter on leadership") {
		t.Fatal("report leaked private work")
	}
	expect(t, e.do(meera, "GET", base+"/reports/tasks?period=90", nil), 422, "invalid period")

	req, _ := http.NewRequest("GET", e.srv.URL+base+"/reports/tasks.csv", nil)
	req.Header.Set("Authorization", "Bearer "+token(meera))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	csvText := string(raw)
	if res.StatusCode != 200 || !strings.Contains(csvText, `'=HYPERLINK`) || strings.Contains(csvText, "Read a chapter on leadership") {
		t.Fatalf("csv export: status %d, formula-safe %v", res.StatusCode, strings.Contains(csvText, `'=HYPERLINK`))
	}
	var exports, audits int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM report_exports WHERE actor_id = $1`, meera).Scan(&exports)
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE actor_id = $1 AND action = 'report.exported'`, meera).Scan(&audits)
	if exports != 1 || audits != 1 {
		t.Fatalf("export history %d, audit events %d; want 1 each", exports, audits)
	}

	view := e.do(meera, "POST", base+"/report-views", map[string]any{"name": "Monthly review", "period": "30"})
	expect(t, view, 201, "save view")
	if n := len(items(e.do(vinit, "GET", base+"/report-views", nil))); n != 0 {
		t.Fatalf("another user's saved views visible: %d", n)
	}
	expect(t, e.do(vinit, "DELETE", "/api/v1/report-views/"+view.body["id"].(string), nil), 404, "deleting another user's view")
	expect(t, e.do(meera, "DELETE", "/api/v1/report-views/"+view.body["id"].(string), nil), 204, "deleting own view")
}

// Threads are visible only to participants, even to org owners and executives.
func TestMessagingPrivacy(t *testing.T) {
	e := setup(t)
	vinit, arjun, ishaan, meera := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("ishaan.rao@example.com"), e.user("meera.iyer@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	base := "/api/v1/organizations/" + acme.String() + "/threads"
	launch := e.id(`SELECT id FROM message_threads WHERE subject LIKE 'Launch readiness%'`).String()

	if n := len(items(e.do(vinit, "GET", base, nil))); n != 3 {
		t.Fatalf("vinit acme threads = %d, want 3", n)
	}
	if n := len(items(e.do(meera, "GET", base, nil))); n != 0 {
		t.Fatalf("executive sees %d threads they are not part of", n)
	}
	expect(t, e.do(ishaan, "GET", "/api/v1/threads/"+launch, nil), 404, "non-participant reading thread")
	expect(t, e.do(ishaan, "POST", "/api/v1/threads/"+launch+"/messages", map[string]any{"body": "hi"}), 404, "non-participant replying")
	expect(t, e.do(ishaan, "PATCH", "/api/v1/threads/"+launch+"/state", map[string]any{"starred": true}), 404, "non-participant starring")
	expect(t, e.do(arjun, "POST", "/api/v1/organizations/"+vertex.String()+"/threads", map[string]any{"subject": "x", "body": "y", "participant_ids": []string{vinit.String()}}), 404, "cross-tenant thread")
	p2 := e.project("Zero Trust rollout").String()
	expect(t, e.do(ishaan, "POST", base, map[string]any{"subject": "x", "body": "y", "participant_ids": []string{vinit.String()}, "project_id": p2}), 422, "linking an unreadable project")

	thread := func(id string) map[string]any { return e.do(vinit, "GET", "/api/v1/threads/"+id, nil).body }
	if thread(launch)["unread"] != true {
		t.Fatal("seed thread should start unread")
	}
	expect(t, e.do(vinit, "PATCH", "/api/v1/threads/"+launch+"/state", map[string]any{"read": true}), 204, "mark read")
	if thread(launch)["unread"] != false {
		t.Fatal("thread still unread after marking read")
	}
	expect(t, e.do(arjun, "POST", "/api/v1/threads/"+launch+"/messages", map[string]any{"body": "Following up"}), 201, "participant reply")
	if thread(launch)["unread"] != true {
		t.Fatal("new reply should mark thread unread for others")
	}

	created := e.do(vinit, "POST", base, map[string]any{"subject": "Security sync", "body": "Can we meet?", "participant_ids": []string{arjun.String()}, "project_id": p2})
	expect(t, created, 201, "create thread")
	if created.body["project"] == nil || len(created.body["messages"].([]any)) != 1 {
		t.Fatalf("created thread missing project or message: %v", created.body)
	}

	ctx := context.Background()
	var visible int
	s := &store.Store{Pool: e.app}
	err := s.InTx(ctx, store.Scope{UserID: meera}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&visible)
	})
	if err != nil || visible != 0 {
		t.Fatalf("RLS exposed %d messages to a non-participant (err %v)", visible, err)
	}
}

// Rules are admin-only, runs are idempotent per day, and alerts reach only people who can read the task.
func TestAutomations(t *testing.T) {
	e := setup(t)
	vinit, arjun, ishaan, sara := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("ishaan.rao@example.com"), e.user("sara.khan@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	base := "/api/v1/organizations/" + acme.String()

	expect(t, e.do(arjun, "GET", base+"/automation-rules", nil), 403, "manager listing rules")
	expect(t, e.do(arjun, "POST", base+"/automation-runs", nil), 403, "manager running rules")
	expect(t, e.do(vinit, "POST", base+"/automation-rules", map[string]any{"name": "x", "trigger": "DROP TABLE", "action": "NOTIFY_TASK_OWNER"}), 422, "unknown trigger")
	vertexRule := e.do(vinit, "POST", "/api/v1/organizations/"+vertex.String()+"/automation-rules", map[string]any{"name": "Vertex rule", "trigger": "TASK_BLOCKED", "action": "NOTIFY_TASK_OWNER", "enabled": true})
	expect(t, vertexRule, 201, "owner creating vertex rule")
	expect(t, e.do(arjun, "PUT", "/api/v1/automation-rules/"+vertexRule.body["id"].(string), map[string]any{"name": "x", "trigger": "TASK_BLOCKED", "action": "NOTIFY_TASK_OWNER"}, "If-Match", `"1"`), 404, "cross-tenant rule write")

	run := e.do(vinit, "POST", base+"/automation-runs", nil)
	expect(t, run, 200, "run rules")
	notified := 0
	for _, r := range items(run) {
		notified += int(r["notified"].(float64))
	}
	if notified == 0 {
		t.Fatal("first run created no notifications")
	}
	for _, r := range items(e.do(vinit, "POST", base+"/automation-runs", nil)) {
		if r["notified"].(float64) != 0 {
			t.Fatalf("second run on the same day re-notified %v", r["notified"])
		}
	}

	// The blocked task sits in "Zero Trust rollout" (owner Sara); Arjun is a project manager there.
	saraInbox := e.do(sara, "GET", base+"/notifications", nil)
	if saraInbox.body["unread"].(float64) == 0 {
		t.Fatal("project owner received no alerts")
	}
	if n := len(items(e.do(ishaan, "GET", base+"/notifications", nil))); n != 0 {
		t.Fatalf("uninvolved member received %d alerts", n)
	}
	var personal int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM notifications n JOIN tasks t ON t.id = n.task_id WHERE t.tenant_id IS NULL`).Scan(&personal)
	if personal != 0 {
		t.Fatalf("%d alerts reference private tasks", personal)
	}
	expect(t, e.do(sara, "POST", base+"/notifications/read", map[string]any{"all": true}), 204, "mark all read")
	if e.do(sara, "GET", base+"/notifications", nil).body["unread"].(float64) != 0 {
		t.Fatal("alerts still unread after mark all")
	}
}

// API tokens are hashed, org-scoped, route-limited and revocable; webhooks are
// SSRF-validated, enqueued with the change, signed, and never carry private work.
func TestIntegrations(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	vinit, arjun := e.user("vinit@example.com"), e.user("arjun.mehta@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	base := "/api/v1/organizations/" + acme.String()
	task := e.task("Review enterprise authentication flow").String()
	private := e.task("Read a chapter on leadership").String()

	expect(t, e.do(arjun, "POST", base+"/api-tokens", map[string]any{"name": "ci", "scope": "read", "expires_in_days": 30}), 403, "non-admin creating token")
	created := e.do(vinit, "POST", base+"/api-tokens", map[string]any{"name": "BI export", "scope": "read", "expires_in_days": 30})
	expect(t, created, 201, "create read token")
	readTok := created.body["token"].(string)
	var stored int
	_ = e.owner.QueryRow(ctx, `SELECT count(*) FROM api_tokens WHERE token_hash = $1 OR prefix = $1`, readTok).Scan(&stored)
	if stored != 0 {
		t.Fatal("token stored in plaintext")
	}
	bearer := func(tok string) []string { return []string{"Authorization", "Bearer " + tok} }
	expect(t, e.do(vinit, "GET", base+"/reports/tasks", nil, bearer(readTok)...), 200, "token reading reports")
	expect(t, e.do(vinit, "GET", "/api/v1/tasks/"+task, nil, bearer(readTok)...), 200, "token reading org task")
	expect(t, e.do(vinit, "GET", "/api/v1/tasks/"+private, nil, bearer(readTok)...), 404, "token reading private task")
	expect(t, e.do(vinit, "GET", "/api/v1/organizations/"+vertex.String(), nil, bearer(readTok)...), 404, "token reaching another org")
	expect(t, e.do(vinit, "GET", "/api/v1/me", nil, bearer(readTok)...), 403, "token on /me")
	expect(t, e.do(vinit, "GET", base+"/threads", nil, bearer(readTok)...), 403, "token reading conversations")
	expect(t, e.do(vinit, "PATCH", "/api/v1/tasks/"+task, map[string]any{"priority": "LOW"}, append(bearer(readTok), "If-Match", `"1"`)...), 403, "read token writing")

	writeTok := e.do(vinit, "POST", base+"/api-tokens", map[string]any{"name": "Sync", "scope": "write", "expires_in_days": 7}).body["token"].(string)
	expect(t, e.do(vinit, "PATCH", "/api/v1/tasks/"+task, map[string]any{"priority": "LOW"}, append(bearer(writeTok), "If-Match", `"1"`)...), 200, "write token updating task")
	expect(t, e.do(vinit, "POST", "/api/v1/api-tokens/"+created.body["id"].(string)+"/revoke", nil), 204, "revoke token")
	expect(t, e.do(vinit, "GET", base+"/reports/tasks", nil, bearer(readTok)...), 401, "revoked token")

	hook := map[string]any{"name": "Ops", "url": "https://127.0.0.1/hook", "events": []string{"task.status_changed"}}
	expect(t, e.do(vinit, "POST", base+"/webhooks", hook), 422, "loopback webhook")
	hook["url"] = "http://hooks.example.com/tf"
	expect(t, e.do(vinit, "POST", base+"/webhooks", hook), 422, "plain http webhook")
	hook["url"] = "https://hooks.example.com/tf"
	wh := e.do(vinit, "POST", base+"/webhooks", hook)
	expect(t, wh, 201, "create webhook")
	whSecret := wh.body["secret"].(string)
	if list := e.do(vinit, "GET", base+"/webhooks", nil); strings.Contains(fmtBody(list), whSecret) {
		t.Fatal("webhook secret returned after creation")
	}

	expect(t, e.do(vinit, "PATCH", "/api/v1/tasks/"+task, map[string]any{"status": "IN_REVIEW"}, "If-Match", `"2"`), 200, "status change")
	expect(t, e.do(vinit, "PATCH", "/api/v1/tasks/"+private, map[string]any{"status": "IN_PROGRESS"}, "If-Match", `"1"`), 200, "private status change")
	n, err := e.api.DeliverWebhooks(ctx)
	if err != nil || n != 1 || len(e.hooks.requests) != 1 {
		t.Fatalf("deliveries attempted %d (err %v), received %d; want exactly the org event", n, err, len(e.hooks.requests))
	}
	req, body := e.hooks.requests[0], e.hooks.bodies[0]
	ts, _ := strconv.ParseInt(req.Header.Get("X-TaskForce-Timestamp"), 10, 64)
	if req.Header.Get("X-TaskForce-Event") != "task.status_changed" || req.Header.Get("X-TaskForce-Signature") != httpapi.Sign(whSecret, ts, body) {
		t.Fatalf("bad delivery headers: %v", req.Header)
	}
	if strings.Contains(string(body), "leadership") {
		t.Fatal("private work leaked into a webhook")
	}

	e.hooks.status = 500
	expect(t, e.do(vinit, "POST", "/api/v1/webhooks/"+wh.body["id"].(string)+"/test", nil), 202, "test webhook")
	if _, err := e.api.DeliverWebhooks(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	_ = e.owner.QueryRow(ctx, `SELECT status, attempts FROM webhook_deliveries WHERE event = 'ping'`).Scan(&status, &attempts)
	if status != "PENDING" || attempts != 1 {
		t.Fatalf("failed delivery should be retried later: status %s attempts %d", status, attempts)
	}
}

func fmtBody(r resp) string {
	raw, _ := json.Marshal(r.body)
	return string(raw)
}

// Teams are org-scoped, writes need team.manage, and workload visibility narrows by role.
func TestTeamsAndWorkload(t *testing.T) {
	e := setup(t)
	vinit, arjun, ishaan, sara := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("ishaan.rao@example.com"), e.user("sara.khan@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")

	list := e.do(ishaan, "GET", "/api/v1/organizations/"+acme.String()+"/teams", nil)
	expect(t, list, 200, "member lists teams")
	if n := len(items(list)); n != 4 {
		t.Fatalf("acme teams = %d, want 4", n)
	}
	body := map[string]any{"name": "Platform guild", "member_ids": []string{ishaan.String()}, "lead_user_id": arjun.String()}
	expect(t, e.do(ishaan, "POST", "/api/v1/organizations/"+acme.String()+"/teams", body), 403, "member creating team")
	expect(t, e.do(arjun, "POST", "/api/v1/organizations/"+acme.String()+"/teams", body), 403, "project manager creating team")
	created := e.do(vinit, "POST", "/api/v1/organizations/"+acme.String()+"/teams", body)
	expect(t, created, 201, "owner creating team")
	id := created.body["id"].(string)
	if n := len(created.body["members"].([]any)); n != 2 {
		t.Fatalf("lead should be added as member, got %d members", n)
	}
	expect(t, e.do(vinit, "POST", "/api/v1/organizations/"+acme.String()+"/teams", body), 422, "duplicate team name")

	vertexTeam := e.id(`SELECT id FROM teams WHERE tenant_id = $1`, vertex)
	expect(t, e.do(vinit, "PUT", "/api/v1/teams/"+vertexTeam.String(), map[string]any{"name": "Partner experience", "member_ids": []string{arjun.String()}}, "If-Match", `"1"`), 422, "adding a non-member of the org")
	expect(t, e.do(arjun, "PUT", "/api/v1/teams/"+vertexTeam.String(), map[string]any{"name": "x"}, "If-Match", `"1"`), 404, "cross-tenant team write")
	expect(t, e.do(vinit, "PUT", "/api/v1/teams/"+id, map[string]any{"name": "Platform guild"}, "If-Match", `"9"`), 412, "stale team version")
	expect(t, e.do(vinit, "DELETE", "/api/v1/teams/"+id, nil, "If-Match", `"1"`), 204, "delete team")

	wl := e.do(arjun, "GET", "/api/v1/organizations/"+acme.String()+"/workload", nil)
	expect(t, wl, 200, "manager workload")
	if wl.body["visibility"] != "organization" || len(items(wl)) != 9 {
		t.Fatalf("manager workload: visibility %v rows %d", wl.body["visibility"], len(items(wl)))
	}
	var want int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM tasks WHERE owner_id = $1 AND tenant_id = $2
		AND status NOT IN ('DONE','CANCELLED') AND due_on BETWEEN $3 AND $4`, vinit, acme, wl.body["from"], wl.body["to"]).Scan(&want)
	for _, row := range items(wl) {
		if row["user"].(map[string]any)["id"] == vinit.String() && int(row["assigned_tasks"].(float64)) != want {
			t.Fatalf("Vinit workload counts %v tasks, want %d org-only", row["assigned_tasks"], want)
		}
	}
	lead := e.do(sara, "GET", "/api/v1/organizations/"+acme.String()+"/workload", nil)
	if lead.body["visibility"] != "team" || len(items(lead)) != 3 {
		t.Fatalf("team lead workload: visibility %v rows %d", lead.body["visibility"], len(items(lead)))
	}
	self := e.do(ishaan, "GET", "/api/v1/organizations/"+acme.String()+"/workload", nil)
	if self.body["visibility"] != "self" || len(items(self)) != 1 {
		t.Fatalf("member workload: visibility %v rows %d", self.body["visibility"], len(items(self)))
	}
}

func titles(list []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, it := range list {
		out[it["title"].(string)] = true
	}
	return out
}

// Calendar scopes keep private work private and respect tenant membership.
func TestCalendarScopes(t *testing.T) {
	e := setup(t)
	vinit, meera, arjun := e.user("vinit@example.com"), e.user("meera.iyer@example.com"), e.user("arjun.mehta@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	rng := "from=2026-10-15&to=2026-12-14"

	personal := e.do(vinit, "GET", "/api/v1/me/calendar?scope=personal&"+rng, nil)
	expect(t, personal, 200, "personal calendar")
	got := titles(items(personal))
	if !got["Read a chapter on leadership"] || got["Review enterprise authentication flow"] {
		t.Fatalf("personal scope wrong: %v", got)
	}

	mine := titles(items(e.do(vinit, "GET", "/api/v1/me/calendar?scope=mine&org="+acme.String()+"&"+rng, nil)))
	if !mine["Read a chapter on leadership"] || !mine["Review enterprise authentication flow"] || mine["Review partner portal prototype"] {
		t.Fatalf("mine scope wrong: %v", mine)
	}

	company := e.do(meera, "GET", "/api/v1/me/calendar?scope=company&org="+acme.String()+"&"+rng, nil)
	expect(t, company, 200, "company calendar")
	got = titles(items(company))
	if got["Read a chapter on leadership"] || !got["Resolve identity provider dependency"] {
		t.Fatalf("company scope wrong: %v", got)
	}
	if ms, _ := company.body["milestones"].([]any); len(ms) == 0 {
		t.Fatal("company calendar has no milestones")
	}

	expect(t, e.do(arjun, "GET", "/api/v1/me/calendar?scope=company&org="+vertex.String()+"&"+rng, nil), 404, "cross-tenant calendar")
	expect(t, e.do(vinit, "GET", "/api/v1/me/calendar?scope=mine&from=2026-01-01&to=2026-12-31", nil), 422, "oversized range")
}

func TestHiringAndDevelopment(t *testing.T) {
	e := setup(t)
	vinit, arjun, ishaan, ananya := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("ishaan.rao@example.com"), e.user("ananya.sen@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	base := "/api/v1/organizations/" + acme.String()

	hr := e.do(ananya, "GET", base+"/hiring", nil)
	expect(t, hr, 200, "HR listing hiring")
	if len(items(hr)) != 3 || hr.body["can_manage"] != true {
		t.Fatalf("HR sees %d requisitions, can_manage=%v", len(items(hr)), hr.body["can_manage"])
	}
	mgr := items(e.do(arjun, "GET", base+"/hiring", nil))
	if len(mgr) != 1 || mgr[0]["title"] != "Senior frontend engineer" || len(mgr[0]["candidates"].([]any)) != 2 {
		t.Fatalf("hiring manager should see only their requisition with its candidates, got %v", titles(mgr))
	}
	if n := len(items(e.do(ishaan, "GET", base+"/hiring", nil))); n != 0 {
		t.Fatalf("member sees %d requisitions", n)
	}
	expect(t, e.do(ananya, "GET", "/api/v1/organizations/"+vertex.String()+"/hiring", nil), 404, "non-member org")

	frontend := mgr[0]["id"].(string)
	expect(t, e.do(arjun, "PATCH", "/api/v1/requisitions/"+frontend, map[string]any{"stage": "OFFER"}, "If-Match", `"1"`), 403, "manager changing stage")
	expect(t, e.do(ishaan, "PATCH", "/api/v1/requisitions/"+frontend, map[string]any{"stage": "OFFER"}, "If-Match", `"1"`), 404, "member changing stage")
	expect(t, e.do(ananya, "PATCH", "/api/v1/requisitions/"+frontend, map[string]any{"stage": "OFFER"}, "If-Match", `"1"`), 200, "HR changing stage")

	c := e.do(ananya, "POST", "/api/v1/requisitions/"+frontend+"/candidates", map[string]any{"full_name": "Zoya Confidential", "email": "zoya@example.net"})
	expect(t, c, 201, "HR adding candidate")
	cid := c.body["id"].(string)
	expect(t, e.do(ananya, "PATCH", "/api/v1/candidates/"+cid, map[string]any{"stage": "INTERVIEW", "next_interview_at": "2026-11-05T10:00:00Z"}, "If-Match", `"1"`), 200, "move candidate")
	expect(t, e.do(ananya, "PATCH", "/api/v1/candidates/"+cid, map[string]any{"stage": "OFFER"}, "If-Match", `"1"`), 412, "stale candidate write")
	expect(t, e.do(arjun, "PATCH", "/api/v1/candidates/"+cid, map[string]any{"stage": "HIRED"}, "If-Match", `"2"`), 403, "manager moving candidate")
	var leaked int
	_ = e.owner.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE summary ILIKE '%zoya%'`).Scan(&leaked)
	if leaked != 0 {
		t.Fatal("candidate name leaked into the audit log")
	}

	g := e.do(vinit, "POST", base+"/goals", map[string]any{"title": "Mentor two engineers", "target_on": "2026-12-01"})
	expect(t, g, 201, "create goal")
	gid := g.body["id"].(string)
	if ts := titles(items(e.do(ananya, "GET", base+"/goals", nil))); ts["Mentor two engineers"] {
		t.Fatal("HR can read another person's development goals")
	}
	expect(t, e.do(ananya, "PATCH", "/api/v1/goals/"+gid, map[string]any{"progress": 100}), 404, "editing someone else's goal")
	if p := e.do(vinit, "PATCH", "/api/v1/goals/"+gid, map[string]any{"progress": 60}); p.status != 200 || p.body["progress"] != float64(60) {
		t.Fatalf("owner update: %d %s", p.status, fmtBody(p))
	}
}

func TestAIAssistant(t *testing.T) {
	for q, want := range map[string]string{
		"What is blocked?": "BLOCKED", "Anything overdue?": "OVERDUE", "My highest priorities": "PRIORITIES",
		"Who is overloaded?": "WORKLOAD", "Summarize the portfolio": "SUMMARY", "Tell me a joke": "UNKNOWN",
	} {
		if got := httpapi.ClassifyQuestion(q); got != want {
			t.Errorf("ClassifyQuestion(%q) = %s, want %s", q, got, want)
		}
	}

	e := setup(t)
	vinit, arjun, meera := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("meera.iyer@example.com")
	acme, vertex := e.org("acme"), e.org("vertex")
	base := "/api/v1/organizations/" + acme.String()

	expect(t, e.do(arjun, "POST", base+"/ai/ask", map[string]any{"question": "What is blocked?"}), 403, "member without ai.query")
	for _, q := range []string{"What is blocked?", "My highest priorities", "Summarize the portfolio", "Who is overloaded?"} {
		for _, org := range []string{acme.String(), vertex.String()} {
			a := e.do(vinit, "POST", "/api/v1/organizations/"+org+"/ai/ask", map[string]any{"question": q})
			expect(t, a, 200, "ask "+q)
			for _, src := range a.body["sources"].([]any) {
				if tid, _ := src.(map[string]any)["tenant_id"].(string); tid != org {
					t.Fatalf("%q in %s cited a source outside the organization (tenant %q)", q, org, tid)
				}
			}
		}
	}
	blocked := e.do(vinit, "POST", base+"/ai/ask", map[string]any{"question": "What is blocked?"})
	if len(blocked.body["sources"].([]any)) == 0 || !strings.Contains(blocked.body["answer"].(string), "blocked") {
		t.Fatalf("blocked answer has no sources: %s", fmtBody(blocked))
	}
	if n := len(items(e.do(vinit, "GET", base+"/ai/history", nil))); n == 0 {
		t.Fatal("asker has no history")
	}
	if n := len(items(e.do(meera, "GET", base+"/ai/history", nil))); n != 0 {
		t.Fatalf("another executive sees %d of the owner's questions", n)
	}
}

func TestAdministration(t *testing.T) {
	e := setup(t)
	vinit, arjun, ishaan := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("ishaan.rao@example.com")
	acme := e.org("acme")
	base := "/api/v1/organizations/" + acme.String()
	invite := func(by uuid.UUID, email, role string) resp {
		return e.do(by, "POST", base+"/invitations", map[string]any{"email": email, "display_name": "New Person", "role": role})
	}

	expect(t, invite(arjun, "new.person@example.com", "MEMBER"), 403, "manager inviting")
	expect(t, invite(vinit, "new.person@example.com", "MEMBER"), 201, "owner inviting")
	expect(t, invite(vinit, "NEW.person@example.com", "MEMBER"), 422, "duplicate invitation")
	newbie := e.user("new.person@example.com")
	expect(t, e.do(newbie, "GET", base, nil), 404, "invitee before accepting")
	if n := len(items(e.do(newbie, "GET", "/api/v1/me/invitations", nil))); n != 1 {
		t.Fatalf("invitee sees %d invitations", n)
	}
	expect(t, e.do(newbie, "POST", base+"/invitations/accept", nil), 200, "accept")
	expect(t, e.do(newbie, "GET", base, nil), 200, "member after accepting")
	expect(t, e.do(ishaan, "POST", base+"/invitations/accept", nil), 404, "accepting without an invitation")

	member := base + "/members/" + newbie.String()
	expect(t, e.do(ishaan, "PUT", member+"/roles", map[string]any{"roles": []string{"ORG_ADMIN"}}), 403, "member granting roles")
	expect(t, e.do(vinit, "PUT", member+"/roles", map[string]any{"roles": []string{"ROOT"}}), 422, "unknown role")
	expect(t, e.do(vinit, "PUT", member+"/roles", map[string]any{"roles": []string{"ORG_ADMIN"}}), 200, "owner granting admin")
	expect(t, e.do(newbie, "PATCH", base+"/members/"+vinit.String(), map[string]any{"state": "SUSPENDED"}), 403, "admin suspending owner")
	expect(t, invite(newbie, "second.owner@example.com", "TENANT_OWNER"), 403, "admin inviting an owner")
	expect(t, e.do(vinit, "PUT", base+"/members/"+vinit.String()+"/roles", map[string]any{"roles": []string{"ORG_ADMIN"}}), 422, "removing the last owner")
	expect(t, e.do(vinit, "PATCH", base+"/members/"+vinit.String(), map[string]any{"state": "SUSPENDED"}), 403, "suspending yourself")

	expect(t, e.do(vinit, "PATCH", member, map[string]any{"state": "SUSPENDED"}), 200, "suspend")
	expect(t, e.do(newbie, "GET", base, nil), 404, "suspended member on an existing session")
	expect(t, e.do(vinit, "PATCH", member, map[string]any{"state": "ACTIVE"}), 200, "reactivate")
	expect(t, e.do(newbie, "GET", base, nil), 200, "reactivated member")

	expect(t, e.do(arjun, "PATCH", base, map[string]any{"name": "Hijacked"}), 403, "manager editing org")
	expect(t, e.do(vinit, "PATCH", base, map[string]any{"reporting_timezone": "Mars/Base"}), 422, "bad timezone")
	if o := e.do(vinit, "PATCH", base, map[string]any{"reporting_timezone": "Europe/London"}); o.status != 200 || o.body["reporting_timezone"] != "Europe/London" {
		t.Fatalf("org update: %d %s", o.status, fmtBody(o))
	}

	expect(t, e.do(ishaan, "POST", "/api/v1/organizations", map[string]any{"name": "Taken", "slug": "acme"}), 422, "duplicate slug")
	created := e.do(ishaan, "POST", "/api/v1/organizations", map[string]any{"name": "Ishaan Labs", "slug": "ishaan-labs", "reporting_timezone": "Asia/Kolkata"})
	expect(t, created, 201, "create organization")
	o := e.do(ishaan, "GET", "/api/v1/organizations/"+created.body["id"].(string), nil)
	if o.status != 200 || !strings.Contains(fmtBody(o), "TENANT_OWNER") {
		t.Fatalf("creator is not the owner: %d %s", o.status, fmtBody(o))
	}
	expect(t, e.do(vinit, "GET", "/api/v1/organizations/"+created.body["id"].(string), nil), 404, "outsider reading new org")
}

func TestMilestonesAndProjectAccess(t *testing.T) {
	e := setup(t)
	vinit, arjun, ishaan := e.user("vinit@example.com"), e.user("arjun.mehta@example.com"), e.user("ishaan.rao@example.com")
	zt := e.project("Zero Trust rollout").String()
	personal := e.id(`SELECT p.id FROM projects p JOIN workspaces w ON w.id = p.workspace_id WHERE w.kind = 'PERSONAL' AND w.owner_user_id = $1 LIMIT 1`, vinit).String()
	vertexProject := e.id(`SELECT p.id FROM projects p JOIN organizations o ON o.id = p.tenant_id WHERE o.slug = 'vertex' LIMIT 1`).String()
	access := func(by uuid.UUID, project string, who uuid.UUID, level string) resp {
		return e.do(by, "PUT", "/api/v1/projects/"+project+"/access/"+who.String(), map[string]any{"access_level": level})
	}

	expect(t, e.do(ishaan, "GET", "/api/v1/projects/"+zt, nil), 404, "non-member before access")
	expect(t, access(ishaan, zt, ishaan, "MANAGER"), 404, "outsider granting themselves access")
	expect(t, access(vinit, zt, ishaan, "VIEWER"), 200, "owner adds viewer")
	expect(t, e.do(ishaan, "GET", "/api/v1/projects/"+zt, nil), 200, "viewer reads project")
	expect(t, e.do(ishaan, "POST", "/api/v1/projects/"+zt+"/milestones", map[string]any{"title": "x", "target_on": "2026-12-01"}), 403, "viewer adding milestone")
	expect(t, access(vinit, zt, ishaan, "MANAGER"), 200, "promote to manager")
	ms := e.do(ishaan, "POST", "/api/v1/projects/"+zt+"/milestones", map[string]any{"title": "Pilot sign-off", "target_on": "2026-12-01"})
	expect(t, ms, 201, "manager adds milestone")
	mid := ms.body["id"].(string)
	if d := e.do(ishaan, "PATCH", "/api/v1/milestones/"+mid, map[string]any{"state": "DONE"}); d.status != 200 || d.body["state"] != "DONE" {
		t.Fatalf("complete milestone: %d %s", d.status, fmtBody(d))
	}
	expect(t, e.do(ishaan, "DELETE", "/api/v1/projects/"+zt+"/access/"+ishaan.String(), nil), 403, "removing yourself")
	expect(t, access(vinit, personal, ishaan, "VIEWER"), 422, "sharing a personal project")
	expect(t, e.do(arjun, "PATCH", "/api/v1/milestones/"+mid, map[string]any{"title": "x"}), 403, "contributor editing milestone")
	expect(t, access(vinit, vertexProject, arjun, "VIEWER"), 422, "adding a non-member of the organization")
	expect(t, e.do(vinit, "DELETE", "/api/v1/projects/"+zt+"/access/"+ishaan.String(), nil), 204, "remove member")
	expect(t, e.do(ishaan, "GET", "/api/v1/projects/"+zt, nil), 404, "removed member")
	expect(t, e.do(ishaan, "DELETE", "/api/v1/milestones/"+mid, nil), 404, "removed member deleting milestone")
	expect(t, e.do(vinit, "DELETE", "/api/v1/milestones/"+mid, nil), 204, "owner deletes milestone")
}
