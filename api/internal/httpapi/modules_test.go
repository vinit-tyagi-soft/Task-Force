package httpapi_test

import (
	"context"
	"testing"
)

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
