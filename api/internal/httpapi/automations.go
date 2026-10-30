package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

// ruleTriggers maps each trigger to a fixed SQL predicate over open org tasks ($2 = today).
var ruleTriggers = map[string]struct{ predicate, prefix string }{
	"TASK_OVERDUE":  {`t.due_on < $2`, "Overdue: "},
	"TASK_BLOCKED":  {`t.status = 'BLOCKED'`, "Blocked: "},
	"TASK_DUE_SOON": {`t.priority IN ('URGENT', 'HIGH') AND t.due_on BETWEEN $2 AND $2 + 2`, "Due soon: "},
}

// Every action notifies someone who can already read the task, so alerts never widen access.
var ruleActions = map[string]bool{"NOTIFY_TASK_OWNER": true, "NOTIFY_PROJECT_OWNER": true, "NOTIFY_PROJECT_MANAGERS": true}

type ruleJSON struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Trigger   string     `json:"trigger"`
	Action    string     `json:"action"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	Version   int        `json:"version"`
}

const ruleSelect = `SELECT id, name, trigger, action, enabled, last_run_at, version FROM automation_rules`

func scanRule(row pgx.Row) (ruleJSON, error) {
	var r ruleJSON
	err := row.Scan(&r.ID, &r.Name, &r.Trigger, &r.Action, &r.Enabled, &r.LastRunAt, &r.Version)
	return r, err
}

func (s *Server) manageScope(w http.ResponseWriter, r *http.Request) (*authz.Actor, uuid.UUID, store.Scope, bool) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err == nil {
		var scope store.Scope
		if scope, err = orgScope(actor, org); err == nil {
			if actor.Can(org, authz.AutomationManage) {
				return actor, org, scope, true
			}
			err = errForbidden("Only organization administrators can manage automations.")
		}
	}
	s.fail(w, r, err)
	return nil, org, store.Scope{}, false
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.manageScope(w, r)
	if !ok {
		return
	}
	out := []ruleJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), ruleSelect+` WHERE tenant_id = $1 ORDER BY created_at`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			rule, err := scanRule(rows)
			if err != nil {
				return err
			}
			out = append(out, rule)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

type ruleInput struct {
	Name    string `json:"name"`
	Trigger string `json:"trigger"`
	Action  string `json:"action"`
	Enabled bool   `json:"enabled"`
}

func (in *ruleInput) validate() error {
	errs := map[string]string{}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		errs["name"] = "Name this rule (up to 80 characters)."
	}
	if _, ok := ruleTriggers[in.Trigger]; !ok {
		errs["trigger"] = "Choose TASK_OVERDUE, TASK_BLOCKED or TASK_DUE_SOON."
	}
	if !ruleActions[in.Action] {
		errs["action"] = "Choose NOTIFY_TASK_OWNER, NOTIFY_PROJECT_OWNER or NOTIFY_PROJECT_MANAGERS."
	}
	if len(errs) > 0 {
		return errValidation(errs)
	}
	return nil
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.manageScope(w, r)
	if !ok {
		return
	}
	var in ruleInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var out ruleJSON
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var err error
		out, err = scanRule(tx.QueryRow(ctx, `INSERT INTO automation_rules (tenant_id, name, trigger, action, enabled, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, name, trigger, action, enabled, last_run_at, version`,
			org, in.Name, in.Trigger, in.Action, in.Enabled, actor.UserID))
		if err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "automation.created", TargetType: "automation_rule",
			TargetID: &out.ID, Summary: "created automation " + in.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out.Version)
	writeJSON(w, r, 201, out)
}

// ruleForWrite resolves a visible rule and enforces manage rights and version.
func ruleForWrite(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID, version int) (uuid.UUID, ruleJSON, error) {
	var org uuid.UUID
	var rule ruleJSON
	err := tx.QueryRow(ctx, `SELECT tenant_id, id, name, trigger, action, enabled, last_run_at, version FROM automation_rules WHERE id = $1`, id).
		Scan(&org, &rule.ID, &rule.Name, &rule.Trigger, &rule.Action, &rule.Enabled, &rule.LastRunAt, &rule.Version)
	switch {
	case store.IsNoRows(err):
		return org, rule, errNotFound()
	case err != nil:
		return org, rule, err
	case !actor.IsMember(org):
		return org, rule, errNotFound()
	case !actor.Can(org, authz.AutomationManage):
		return org, rule, errForbidden("Only organization administrators can manage automations.")
	case rule.Version != version:
		return org, rule, errVersionConflict()
	}
	return org, rule, nil
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "ruleId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in ruleInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		s.fail(w, r, err)
		return
	}
	var out ruleJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, before, err := ruleForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		out, err = scanRule(tx.QueryRow(ctx, `UPDATE automation_rules SET name = $1, trigger = $2, action = $3, enabled = $4,
			version = version + 1, updated_at = now() WHERE id = $5 AND version = $6
			RETURNING id, name, trigger, action, enabled, last_run_at, version`, in.Name, in.Trigger, in.Action, in.Enabled, id, version))
		if store.IsNoRows(err) {
			return errVersionConflict()
		}
		if err != nil {
			return err
		}
		action := "automation.updated"
		if before.Enabled != in.Enabled {
			action = map[bool]string{true: "automation.enabled", false: "automation.disabled"}[in.Enabled]
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: action, TargetType: "automation_rule",
			TargetID: &id, Summary: action[len("automation."):] + " automation " + in.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out.Version)
	writeJSON(w, r, 200, out)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "ruleId")
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
		org, rule, err := ruleForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM automation_rules WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "automation.deleted", TargetType: "automation_rule",
			TargetID: &id, Summary: "deleted automation " + rule.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type runJSON struct {
	ID        uuid.UUID `json:"id"`
	RuleID    uuid.UUID `json:"rule_id"`
	RuleName  string    `json:"rule_name"`
	StartedBy *userJSON `json:"started_by"`
	Matched   int       `json:"matched"`
	Notified  int       `json:"notified"`
	CreatedAt time.Time `json:"created_at"`
}

// runRules evaluates every enabled rule against open org tasks. Notifications are
// de-duplicated per rule, task, recipient and day, so repeated runs are safe.
func (s *Server) runRules(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.manageScope(w, r)
	if !ok {
		return
	}
	runs := []runJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var tz string
		if err := tx.QueryRow(ctx, `SELECT reporting_timezone FROM organizations WHERE id = $1`, org).Scan(&tz); err != nil {
			return err
		}
		today := domain.Today(s.now(), tz)
		rows, err := tx.Query(ctx, ruleSelect+` WHERE tenant_id = $1 AND enabled ORDER BY created_at`, org)
		if err != nil {
			return err
		}
		var rules []ruleJSON
		for rows.Next() {
			rule, err := scanRule(rows)
			if err != nil {
				rows.Close()
				return err
			}
			rules = append(rules, rule)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, rule := range rules {
			trig := ruleTriggers[rule.Trigger]
			matched := `SELECT t.id, t.title, t.owner_id, p.id AS project_id, p.name AS project_name, p.owner_id AS project_owner
				FROM tasks t JOIN projects p ON p.id = t.project_id
				WHERE t.tenant_id = $1 AND t.deleted_at IS NULL AND p.deleted_at IS NULL AND p.state = 'ACTIVE'
				  AND t.status NOT IN ('DONE', 'CANCELLED') AND $2::date IS NOT NULL AND ` + trig.predicate
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM (`+matched+`) m`, org, today).Scan(&count); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `
				WITH m AS (`+matched+`),
				recipients AS (
					SELECT m.id, m.title, m.project_name, x.user_id FROM m CROSS JOIN LATERAL (
						SELECT m.owner_id AS user_id WHERE $3 = 'NOTIFY_TASK_OWNER'
						UNION SELECT m.project_owner WHERE $3 IN ('NOTIFY_PROJECT_OWNER', 'NOTIFY_PROJECT_MANAGERS')
						UNION SELECT pm.user_id FROM project_memberships pm
						      WHERE pm.project_id = m.project_id AND pm.access_level = 'MANAGER' AND $3 = 'NOTIFY_PROJECT_MANAGERS'
					) x
					JOIN org_memberships om ON om.org_id = $1 AND om.user_id = x.user_id AND om.state = 'ACTIVE'
				)
				INSERT INTO notifications (tenant_id, user_id, rule_id, task_id, title, detail, dedupe_on)
				SELECT $1, user_id, $4, id, left($5 || title, 300), project_name || ' · ' || $6, $2 FROM recipients
				ON CONFLICT DO NOTHING`, org, today, rule.Action, rule.ID, trig.prefix, rule.Name)
			if err != nil {
				return err
			}
			run := runJSON{RuleID: rule.ID, RuleName: rule.Name, Matched: count, Notified: int(tag.RowsAffected())}
			if err := tx.QueryRow(ctx, `INSERT INTO automation_runs (tenant_id, rule_id, rule_name, started_by, matched, notified)
				VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`, org, rule.ID, rule.Name, actor.UserID, run.Matched, run.Notified).
				Scan(&run.ID, &run.CreatedAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE automation_rules SET last_run_at = now() WHERE id = $1`, rule.ID); err != nil {
				return err
			}
			runs = append(runs, run)
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "automation.run", TargetType: "organization", TargetID: &org,
			Summary: "ran " + strconv.Itoa(len(runs)) + " automation rules"})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": runs})
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.manageScope(w, r)
	if !ok {
		return
	}
	out := []runJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, `SELECT id, rule_id, rule_name, started_by, matched, notified, created_at FROM automation_runs
			WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 50`, org)
		if err != nil {
			return err
		}
		var starters []uuid.UUID
		for rows.Next() {
			var run runJSON
			var u uuid.UUID
			if err := rows.Scan(&run.ID, &run.RuleID, &run.RuleName, &u, &run.Matched, &run.Notified, &run.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			starters = append(starters, u)
			out = append(out, run)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		users, err := loadUsers(ctx, tx, starters)
		if err != nil {
			return err
		}
		for i := range out {
			if u, ok := users[starters[i]]; ok {
				out[i].StartedBy = &u
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

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
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
	type notificationJSON struct {
		ID        uuid.UUID  `json:"id"`
		Title     string     `json:"title"`
		Detail    string     `json:"detail"`
		TaskID    *uuid.UUID `json:"task_id"`
		CreatedAt time.Time  `json:"created_at"`
		Read      bool       `json:"read"`
	}
	out := []notificationJSON{}
	var unread int
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, `SELECT id, title, detail, task_id, created_at, read_at IS NOT NULL FROM notifications
			WHERE tenant_id = $1 AND user_id = $2 ORDER BY created_at DESC LIMIT 100`, org, actor.UserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n notificationJSON
			if err := rows.Scan(&n.ID, &n.Title, &n.Detail, &n.TaskID, &n.CreatedAt, &n.Read); err != nil {
				rows.Close()
				return err
			}
			out = append(out, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE tenant_id = $1 AND user_id = $2 AND read_at IS NULL`, org, actor.UserID).Scan(&unread)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out, "unread": unread})
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
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
	var in struct {
		IDs []uuid.UUID `json:"ids"`
		All bool        `json:"all"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE notifications SET read_at = now()
			WHERE tenant_id = $1 AND user_id = $2 AND read_at IS NULL AND ($3 OR id = ANY($4))`, org, actor.UserID, in.All, in.IDs)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
