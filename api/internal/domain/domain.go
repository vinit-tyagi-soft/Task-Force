// Package domain holds task lifecycle rules and authoritative metric definitions.
package domain

import (
	"time"
)

const (
	StatusTodo       = "TODO"
	StatusInProgress = "IN_PROGRESS"
	StatusInReview   = "IN_REVIEW"
	StatusBlocked    = "BLOCKED"
	StatusDone       = "DONE"
	StatusCancelled  = "CANCELLED"
)

var Statuses = []string{StatusTodo, StatusInProgress, StatusInReview, StatusBlocked, StatusDone, StatusCancelled}
var Priorities = []string{"URGENT", "HIGH", "MEDIUM", "LOW"}

// Default workflow (blueprint 4.2). Blocked is entered from active states;
// Done and Cancelled can be reopened, which is audited separately.
var transitions = map[string][]string{
	StatusTodo:       {StatusInProgress, StatusBlocked, StatusDone, StatusCancelled},
	StatusInProgress: {StatusTodo, StatusInReview, StatusBlocked, StatusDone, StatusCancelled},
	StatusInReview:   {StatusInProgress, StatusBlocked, StatusDone, StatusCancelled},
	StatusBlocked:    {StatusTodo, StatusInProgress, StatusInReview, StatusCancelled},
	StatusDone:       {StatusTodo, StatusInProgress},
	StatusCancelled:  {StatusTodo},
}

func ValidStatus(s string) bool   { return contains(Statuses, s) }
func ValidPriority(p string) bool { return contains(Priorities, p) }

func CanTransition(from, to string) bool {
	return from == to || contains(transitions[from], to)
}

func IsTerminal(status string) bool { return status == StatusDone || status == StatusCancelled }

func IsReopen(from, to string) bool { return IsTerminal(from) && !IsTerminal(to) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// TaskFacts is the subset of a task the metric definitions read.
type TaskFacts struct {
	Status          string
	DueOn           *time.Time // calendar date
	CompletedOn     *time.Time // calendar date in reporting timezone
	EstimateMinutes int
}

// Overdue: due date has passed in the reporting timezone and the task is not terminal.
func (t TaskFacts) Overdue(today time.Time) bool {
	return t.DueOn != nil && t.DueOn.Before(today) && !IsTerminal(t.Status)
}

type Metrics struct {
	Eligible    int `json:"eligible"`
	Done        int `json:"done"`
	Open        int `json:"open"`
	Blocked     int `json:"blocked"`
	Overdue     int `json:"overdue"`
	Attention   int `json:"attention"`
	RatePercent int `json:"rate_percent"`
	OpenHours   int `json:"open_hours"`
}

// Compute applies section 12: Cancelled is excluded from the denominator,
// and Needs attention is the unique union of blocked and overdue.
func Compute(tasks []TaskFacts, today time.Time) Metrics {
	var m Metrics
	openMinutes := 0
	for _, t := range tasks {
		if t.Status == StatusCancelled {
			continue
		}
		m.Eligible++
		if t.Status == StatusDone {
			m.Done++
			continue
		}
		openMinutes += t.EstimateMinutes
		blocked := t.Status == StatusBlocked
		overdue := t.Overdue(today)
		if blocked {
			m.Blocked++
		}
		if overdue {
			m.Overdue++
		}
		if blocked || overdue {
			m.Attention++
		}
	}
	m.Open = m.Eligible - m.Done
	if m.Eligible > 0 {
		m.RatePercent = m.Done * 100 / m.Eligible
	}
	m.OpenHours = (openMinutes + 30) / 60
	return m
}

const (
	HealthOnTrack        = "ON_TRACK"
	HealthNeedsAttention = "NEEDS_ATTENTION"
	HealthAtRisk         = "AT_RISK"
)

// Health uses transparent rules (no predictive score): any blocked task => at risk,
// otherwise any overdue task => needs attention.
func Health(m Metrics) string {
	switch {
	case m.Blocked > 0:
		return HealthAtRisk
	case m.Overdue > 0:
		return HealthNeedsAttention
	default:
		return HealthOnTrack
	}
}

// Today returns the current calendar date in the given IANA timezone.
func Today(now time.Time, tz string) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	y, mo, d := now.In(loc).Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
}

// LocalDate converts a timestamp to its calendar date in tz.
func LocalDate(ts time.Time, tz string) time.Time { return Today(ts, tz) }
