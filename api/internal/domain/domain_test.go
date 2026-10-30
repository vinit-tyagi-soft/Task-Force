package domain

import (
	"testing"
	"time"
)

func date(s string) *time.Time {
	d, _ := time.Parse(time.DateOnly, s)
	return &d
}

// T07: a blocked-and-overdue task is counted once in Needs attention, and
// Cancelled is excluded from the completion denominator.
func TestComputeAttentionUnionAndEligibility(t *testing.T) {
	today := *date("2026-10-30")
	m := Compute([]TaskFacts{
		{Status: StatusBlocked, DueOn: date("2026-10-29")},    // blocked + overdue
		{Status: StatusInProgress, DueOn: date("2026-10-28")}, // overdue
		{Status: StatusBlocked, DueOn: date("2026-11-02")},    // blocked
		{Status: StatusDone, DueOn: date("2026-10-01")},       // done, never overdue
		{Status: StatusCancelled, DueOn: date("2026-10-01")},  // excluded
		{Status: StatusTodo, DueOn: date("2026-10-30")},       // due today, not overdue
	}, today)
	want := Metrics{Eligible: 5, Done: 1, Open: 4, Blocked: 2, Overdue: 2, Attention: 3, RatePercent: 20}
	if m != want {
		t.Fatalf("got %+v want %+v", m, want)
	}
	if Health(m) != HealthAtRisk {
		t.Fatalf("health = %s", Health(m))
	}
}

func TestTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusTodo, StatusInProgress, true},
		{StatusBlocked, StatusDone, false},
		{StatusDone, StatusTodo, true},
		{StatusCancelled, StatusInProgress, false},
		{StatusInReview, StatusBlocked, true},
	}
	for _, c := range cases {
		if CanTransition(c.from, c.to) != c.ok {
			t.Errorf("%s -> %s: want %v", c.from, c.to, c.ok)
		}
	}
}

func TestTodayUsesTimezone(t *testing.T) {
	now := time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC) // 01:30 on 31 Oct in India
	if got := Today(now, "Asia/Kolkata").Format(time.DateOnly); got != "2026-10-31" {
		t.Fatalf("Asia/Kolkata today = %s", got)
	}
	if got := Today(now, "UTC").Format(time.DateOnly); got != "2026-10-30" {
		t.Fatalf("UTC today = %s", got)
	}
}
