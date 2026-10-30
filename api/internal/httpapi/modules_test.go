package httpapi_test

import (
	"testing"
)

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
