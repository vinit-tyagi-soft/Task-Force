package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

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
