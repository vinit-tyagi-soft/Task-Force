package httpapi

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

const aiSourceLimit = 8

// aiIntents routes a question by keyword, checked in order. No model is involved:
// every answer is computed from tasks the caller can already read.
var aiIntents = []struct {
	key   string
	words []string
}{
	{"BLOCKED", []string{"block", "stuck", "depend", "waiting"}},
	{"OVERDUE", []string{"overdue", "late", "behind", "slip", "missed"}},
	{"WORKLOAD", []string{"workload", "capacity", "overload", "busy", "bandwidth"}},
	{"PRIORITIES", []string{"priorit", "focus", "attention", "my ", "this week", "next"}},
	{"SUMMARY", []string{"summar", "portfolio", "risk", "health", "overview", "status"}},
}

var priorityRank = map[string]int{"URGENT": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3}

// ClassifyQuestion returns the intent for a question, or UNKNOWN.
func ClassifyQuestion(q string) string {
	q = " " + strings.ToLower(q) + " "
	for _, in := range aiIntents {
		for _, w := range in.words {
			if strings.Contains(q, w) {
				return in.key
			}
		}
	}
	return "UNKNOWN"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func (s *Server) askAI(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.aiAccess(w, r)
	if !ok {
		return
	}
	var in struct {
		Question string `json:"question"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Question = strings.TrimSpace(in.Question)
	if in.Question == "" || len([]rune(in.Question)) > 300 {
		s.fail(w, r, errValidation(map[string]string{"question": "Ask a question of up to 300 characters."}))
		return
	}
	intent := ClassifyQuestion(in.Question)
	resp := map[string]any{"question": in.Question, "intent": intent}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		policy, err := loadAIPolicy(ctx, tx, org)
		if err != nil {
			return err
		}
		if !policy.Enabled {
			return errForbidden("The AI Intelligence Center is turned off by your organization's AI policy.")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ai_queries WHERE tenant_id = $1 AND user_id = $2 AND created_at < now() - make_interval(days => $3)`,
			org, actor.UserID, policy.RetentionDays); err != nil {
			return err
		}
		d, err := s.loadReport(ctx, tx, actor, org, reportFilter{Period: "all"})
		if err != nil {
			return err
		}
		var answer string
		var sources []*taskRow
		if intent != "UNKNOWN" && !slices.Contains(policy.AllowedIntents, intent) {
			answer = "Your organization's AI policy does not allow " + strings.ToLower(intent) + " questions. Ask an administrator if you need them."
			resp["policy_blocked"] = true
		} else if answer, sources, err = s.answer(ctx, tx, actor, org, intent, d); err != nil {
			return err
		}
		if len(sources) > aiSourceLimit {
			sources = sources[:aiSourceLimit]
		}
		items, err := s.renderTasks(ctx, tx, actor, sources, d.access)
		if err != nil {
			return err
		}
		resp["answer"] = answer
		resp["sources"] = items
		resp["method"] = fmt.Sprintf("Keyword-routed rules (%s) over %s in %s you can read", intent,
			plural(len(d.tasks), "task"), plural(len(d.access), "project"))
		resp["scope"] = d.orgName + " · organization work you can access · private work excluded"
		resp["as_of"] = d.today.Format(time.DateOnly)
		if _, err := tx.Exec(ctx, `INSERT INTO ai_queries (tenant_id, user_id, question, intent, source_count) VALUES ($1, $2, $3, $4, $5)`,
			org, actor.UserID, in.Question, intent, len(items)); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "ai.queried", TargetType: "ai_query",
			Summary: "asked the rules-based assistant (" + strings.ToLower(intent) + ")"})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, resp)
}

func (s *Server) answer(ctx context.Context, tx pgx.Tx, actor *authz.Actor, org uuid.UUID, intent string, d *reportData) (string, []*taskRow, error) {
	var open []*taskRow
	for _, t := range d.tasks {
		if !domain.IsTerminal(t.Status) {
			open = append(open, t)
		}
	}
	byDue := func(a, b *taskRow) int {
		if a.DueOn == nil || b.DueOn == nil {
			return cmp.Compare(boolInt(a.DueOn == nil), boolInt(b.DueOn == nil))
		}
		return a.DueOn.Compare(*b.DueOn)
	}
	switch intent {
	case "BLOCKED":
		var blocked []*taskRow
		projects := map[uuid.UUID]bool{}
		for _, t := range open {
			if t.Status == domain.StatusBlocked {
				blocked = append(blocked, t)
				projects[t.ProjectID] = true
			}
		}
		if len(blocked) == 0 {
			return "Nothing is blocked right now in the work you can see.", nil, nil
		}
		slices.SortFunc(blocked, byDue)
		answer := fmt.Sprintf("%s blocked across %s.", taskCount(len(blocked)), plural(len(projects), "project"))
		first := blocked[0]
		answer += fmt.Sprintf(" The most time-sensitive is “%s” in %s", first.Title, first.ProjectName)
		if first.BlockedReason != "" {
			answer += ": " + first.BlockedReason
		}
		return answer + ".", blocked, nil
	case "OVERDUE":
		var late []*taskRow
		for _, t := range open {
			if t.facts().Overdue(d.today) {
				late = append(late, t)
			}
		}
		if len(late) == 0 {
			return "Nothing is past its due date. Every open task you can see is on schedule.", nil, nil
		}
		slices.SortFunc(late, byDue)
		days := int(d.today.Sub(*late[0].DueOn).Hours() / 24)
		return fmt.Sprintf("%s past the due date. The most overdue is “%s” in %s, %s late.",
			taskCount(len(late)), late[0].Title, late[0].ProjectName, plural(days, "day")), late, nil
	case "PRIORITIES":
		var mine []*taskRow
		for _, t := range open {
			if t.OwnerID == actor.UserID {
				mine = append(mine, t)
			}
		}
		if len(mine) == 0 {
			return "You have no open organization tasks assigned to you. Ask “What is blocked?” to see where you could help.", nil, nil
		}
		attention := func(t *taskRow) int {
			return boolInt(!(t.Status == domain.StatusBlocked || t.facts().Overdue(d.today)))
		}
		slices.SortFunc(mine, func(a, b *taskRow) int {
			return cmp.Or(cmp.Compare(attention(a), attention(b)), cmp.Compare(priorityRank[a.Priority], priorityRank[b.Priority]), byDue(a, b))
		})
		return fmt.Sprintf("You own %s. Start with “%s” (%s priority%s), ranked by blocked or overdue first, then priority, then due date.",
			plural(len(mine), "open task"), mine[0].Title, strings.ToLower(mine[0].Priority), dueText(mine[0])), mine, nil
	case "WORKLOAD":
		if !actor.Can(org, authz.WorkloadRead) {
			return "Your role does not include team workload. Ask about your priorities instead.", nil, nil
		}
		counts := map[uuid.UUID]int{}
		for _, t := range open {
			counts[t.OwnerID]++
		}
		if len(counts) == 0 {
			return "There is no open work assigned in the projects you can see.", nil, nil
		}
		ids := make([]uuid.UUID, 0, len(counts))
		for id := range counts {
			ids = append(ids, id)
		}
		names := map[uuid.UUID]string{}
		rows, err := tx.Query(ctx, `SELECT id, display_name FROM users WHERE id = ANY($1)`, ids)
		if err != nil {
			return "", nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var n string
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return "", nil, err
			}
			names[id] = n
		}
		rows.Close()
		slices.SortFunc(ids, func(a, b uuid.UUID) int {
			return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(names[a], names[b]))
		})
		parts := []string{}
		for _, id := range ids[:min(3, len(ids))] {
			parts = append(parts, fmt.Sprintf("%s (%d)", names[id], counts[id]))
		}
		return fmt.Sprintf("%s spread across %s. The heaviest open loads: %s. See Teams & Workload for capacity.",
			plural(len(open), "open task"), plural(len(ids), "person"), strings.Join(parts, ", ")), nil, rows.Err()
	case "SUMMARY":
		facts := make([]domain.TaskFacts, 0, len(d.tasks))
		var risk []*taskRow
		for _, t := range d.tasks {
			facts = append(facts, t.facts())
			if !domain.IsTerminal(t.Status) && (t.Status == domain.StatusBlocked || t.facts().Overdue(d.today)) {
				risk = append(risk, t)
			}
		}
		m := domain.Compute(facts, d.today)
		answer := fmt.Sprintf("%s across %s, with a %d%% completion rate. %d need attention (%d blocked, %d overdue).",
			plural(m.Open, "open task"), plural(len(d.access), "project"), m.RatePercent, m.Attention, m.Blocked, m.Overdue)
		answer += " Overall health: " + strings.ToLower(strings.ReplaceAll(domain.Health(m), "_", " ")) + "."
		slices.SortFunc(risk, byDue)
		return answer, risk, nil
	}
	return "I can answer questions about blocked work, overdue tasks, your priorities, team workload and a portfolio summary. Try one of the suggestions.", nil, nil
}

// taskCount renders "1 task is" or "N tasks are".
func taskCount(n int) string {
	if n == 1 {
		return "1 task is"
	}
	return fmt.Sprintf("%d tasks are", n)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func dueText(t *taskRow) string {
	if t.DueOn == nil {
		return ""
	}
	return ", due " + t.DueOn.Format("2 Jan")
}

func (s *Server) aiAccess(w http.ResponseWriter, r *http.Request) (*authz.Actor, uuid.UUID, store.Scope, bool) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err == nil {
		var scope store.Scope
		if scope, err = orgScope(actor, org); err == nil {
			if actor.Can(org, authz.AIQuery) {
				return actor, org, scope, true
			}
			err = errForbidden("Your role does not include the AI Intelligence Center.")
		}
	}
	s.fail(w, r, err)
	return nil, org, store.Scope{}, false
}

func (s *Server) aiHistory(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.aiAccess(w, r)
	if !ok {
		return
	}
	type entry struct {
		ID        uuid.UUID `json:"id"`
		Question  string    `json:"question"`
		Intent    string    `json:"intent"`
		Sources   int       `json:"source_count"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []entry{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, question, intent, source_count, created_at FROM ai_queries
			WHERE tenant_id = $1 AND user_id = $2
			  AND created_at >= now() - make_interval(days => coalesce((SELECT retention_days FROM ai_policies WHERE tenant_id = $1), 90))
			ORDER BY created_at DESC LIMIT 10`, org, actor.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.ID, &e.Question, &e.Intent, &e.Sources, &e.CreatedAt); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

var aiPolicyIntents = []string{"BLOCKED", "OVERDUE", "PRIORITIES", "WORKLOAD", "SUMMARY"}

type aiPolicy struct {
	Enabled        bool       `json:"enabled"`
	AllowedIntents []string   `json:"allowed_intents"`
	RetentionDays  int        `json:"retention_days"`
	Version        int        `json:"version"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

// loadAIPolicy returns the org's policy, or the defaults when none has been saved.
func loadAIPolicy(ctx context.Context, tx pgx.Tx, org uuid.UUID) (aiPolicy, error) {
	p := aiPolicy{Enabled: true, AllowedIntents: aiPolicyIntents, RetentionDays: 90, Version: 1}
	err := tx.QueryRow(ctx, `SELECT enabled, allowed_intents, retention_days, version, updated_at FROM ai_policies WHERE tenant_id = $1`, org).
		Scan(&p.Enabled, &p.AllowedIntents, &p.RetentionDays, &p.Version, &p.UpdatedAt)
	if store.IsNoRows(err) {
		return p, nil
	}
	return p, err
}

func (s *Server) getAIPolicy(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	scope, err := orgScope(actor, org)
	if err == nil && !actor.Can(org, authz.AIQuery) && !actor.Can(org, authz.AIManagePolicy) {
		err = errForbidden("Your role does not include the AI Intelligence Center.")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var p aiPolicy
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		p, err = loadAIPolicy(r.Context(), tx, org)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, p.Version)
	writeJSON(w, r, 200, map[string]any{"policy": p, "intents": aiPolicyIntents, "can_manage": actor.Can(org, authz.AIManagePolicy)})
}

func (s *Server) updateAIPolicy(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.adminScope(w, r, authz.AIManagePolicy)
	if !ok {
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Enabled        bool     `json:"enabled"`
		AllowedIntents []string `json:"allowed_intents"`
		RetentionDays  int      `json:"retention_days"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	allowed := []string{}
	for _, i := range aiPolicyIntents {
		if slices.Contains(in.AllowedIntents, i) {
			allowed = append(allowed, i)
		}
	}
	if len(allowed) != len(in.AllowedIntents) {
		errs["allowed_intents"] = "Choose from " + strings.Join(aiPolicyIntents, ", ") + "."
	}
	if !slices.Contains([]int{7, 30, 90, 365}, in.RetentionDays) {
		errs["retention_days"] = "Keep history for 7, 30, 90 or 365 days."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	var out aiPolicy
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		current, err := loadAIPolicy(ctx, tx, org)
		if err != nil {
			return err
		}
		if current.Version != version {
			return errVersionConflict()
		}
		if err := tx.QueryRow(ctx, `INSERT INTO ai_policies (tenant_id, enabled, allowed_intents, retention_days, version, updated_by)
			VALUES ($1, $2, $3, $4, 2, $5)
			ON CONFLICT (tenant_id) DO UPDATE SET enabled = EXCLUDED.enabled, allowed_intents = EXCLUDED.allowed_intents,
				retention_days = EXCLUDED.retention_days, version = ai_policies.version + 1, updated_by = EXCLUDED.updated_by, updated_at = now()
			RETURNING enabled, allowed_intents, retention_days, version, updated_at`, org, in.Enabled, allowed, in.RetentionDays, actor.UserID).
			Scan(&out.Enabled, &out.AllowedIntents, &out.RetentionDays, &out.Version, &out.UpdatedAt); err != nil {
			return err
		}
		state := "on"
		if !out.Enabled {
			state = "off"
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "ai.policy_updated", TargetType: "organization", TargetID: &org,
			Summary: fmt.Sprintf("set AI %s, %d question types, %d-day history", state, len(allowed), in.RetentionDays)})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out.Version)
	writeJSON(w, r, 200, map[string]any{"policy": out, "intents": aiPolicyIntents, "can_manage": true})
}
