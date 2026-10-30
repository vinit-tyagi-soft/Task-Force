package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

const maxCalendarDays = 62

// calendar returns dated work in [from, to] for one scope: "mine" (my assignments,
// private and in the selected organization), "company" (visible organization work)
// or "personal" (private work only).
func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	q := r.URL.Query()
	errs := map[string]string{}
	from := parseDate("from", q.Get("from"), errs)
	to := parseDate("to", q.Get("to"), errs)
	if len(errs) == 0 && (to.Before(from) || to.Sub(from) > maxCalendarDays*24*time.Hour) {
		errs["to"] = "Use a range of at most 62 days ending on or after the start."
	}
	mode := q.Get("scope")
	if mode == "" {
		mode = "mine"
	}
	if mode != "mine" && mode != "company" && mode != "personal" {
		errs["scope"] = "Use mine, company or personal."
	}
	var org *uuid.UUID
	if v := q.Get("org"); v != "" && mode != "personal" {
		id, err := uuid.Parse(v)
		if err != nil || !actor.IsMember(id) {
			s.fail(w, r, errNotFound())
			return
		}
		org = &id
	}
	if mode == "company" && org == nil {
		errs["org"] = "Select an organization."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}

	type calProject struct {
		ID    uuid.UUID `json:"id"`
		Name  string    `json:"name"`
		Color string    `json:"color"`
	}
	type calMilestone struct {
		ID       uuid.UUID  `json:"id"`
		Title    string     `json:"title"`
		TargetOn string     `json:"target_on"`
		State    string     `json:"state"`
		Project  calProject `json:"project"`
	}
	projects := []calProject{}
	milestones := []calMilestone{}
	var items []taskJSON
	var today time.Time
	err := s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID, TenantID: org}, func(tx pgx.Tx) error {
		ctx := r.Context()
		personal := `(w.kind = 'PERSONAL' AND w.owner_user_id = $1)`
		var where string
		switch mode {
		case "personal":
			where = personal
		case "company":
			where = `p.tenant_id = $2`
		default:
			where = personal + ` OR ($2::uuid IS NOT NULL AND p.tenant_id = $2 AND (p.owner_id = $1 OR
				EXISTS (SELECT 1 FROM project_memberships pm WHERE pm.project_id = p.id AND pm.user_id = $1)))`
		}
		args := []any{actor.UserID}
		if mode != "personal" {
			args = append(args, org)
		}
		rows, err := tx.Query(ctx, projectSelect+` AND p.state = 'ACTIVE' AND (`+where+`) ORDER BY p.name`, args...)
		if err != nil {
			return err
		}
		access := map[uuid.UUID]authz.Access{}
		var visible []uuid.UUID
		for rows.Next() {
			p, err := scanProject(rows, actor)
			if err != nil {
				rows.Close()
				return err
			}
			if p.Access >= authz.AccessRead {
				access[p.ID] = p.Access
				visible = append(visible, p.ID)
				projects = append(projects, calProject{p.ID, p.Name, p.Color})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		args = []any{actor.UserID, from, to}
		var taskWhere string
		switch mode {
		case "personal":
			taskWhere = `w.kind = 'PERSONAL' AND w.owner_user_id = $1`
		case "company":
			taskWhere = `t.tenant_id = $4 AND (t.project_id = ANY($5) OR t.owner_id = $1)`
			args = append(args, org, visible)
		default:
			taskWhere = `t.owner_id = $1 AND (w.kind = 'PERSONAL' OR ($4::uuid IS NOT NULL AND t.tenant_id = $4))`
			args = append(args, org)
		}
		rows, err = tx.Query(ctx, taskSelect+` AND p.state = 'ACTIVE' AND t.due_on BETWEEN $2 AND $3 AND `+taskWhere+`
			ORDER BY t.due_on, CASE t.priority WHEN 'URGENT' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 ELSE 3 END, t.number`,
			args...)
		if err != nil {
			return err
		}
		ts, err := scanTasks(rows)
		if err != nil {
			return err
		}
		readable := ts[:0]
		for _, t := range ts {
			if read, _ := taskPermissions(actor, access[t.ProjectID], t); read {
				readable = append(readable, t)
			}
		}
		if items, err = s.renderTasks(ctx, tx, actor, readable, access); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT m.id, m.title, m.target_on, m.state, p.id, p.name, p.color
			FROM milestones m JOIN projects p ON p.id = m.project_id
			WHERE m.project_id = ANY($1) AND m.target_on BETWEEN $2 AND $3 ORDER BY m.target_on, p.name`, visible, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m calMilestone
			var target time.Time
			if err := rows.Scan(&m.ID, &m.Title, &target, &m.State, &m.Project.ID, &m.Project.Name, &m.Project.Color); err != nil {
				return err
			}
			m.TargetOn = target.Format(time.DateOnly)
			milestones = append(milestones, m)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		z, err := loadZones(ctx, tx, actor.UserID)
		if err != nil {
			return err
		}
		today = domain.Today(s.now(), z.user)
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{
		"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "today": today.Format(time.DateOnly),
		"scope": mode, "items": items, "milestones": milestones, "projects": projects,
	})
}
