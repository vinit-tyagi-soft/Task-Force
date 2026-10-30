package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
)

var metricDefinitions = map[string]string{
	"completion":  "Done ÷ eligible tasks; Cancelled tasks are excluded from the denominator.",
	"overdue":     "Due date before today in the organization's reporting timezone, and not Done or Cancelled.",
	"blocked":     "Status is Blocked, regardless of due date.",
	"attention":   "Unique union of blocked and overdue tasks; a task that is both is counted once.",
	"health":      "At risk if any task is blocked; Needs attention if any task is overdue; otherwise On track. Rules-based, not predictive.",
	"open_effort": "Sum of estimates on eligible tasks that are not Done.",
	"scope":       "Organization-owned work only. Personal workspaces are never included.",
}

// executiveSummary returns the authorized portfolio snapshot (blueprint 11.1, 12).
func (s *Server) executiveSummary(w http.ResponseWriter, r *http.Request) {
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
	if !actor.Can(org, authz.ReportRead) {
		s.fail(w, r, errForbidden("Your role does not include executive reporting for this organization."))
		return
	}

	type deptJSON struct {
		Name    string         `json:"name"`
		Metrics domain.Metrics `json:"metrics"`
	}
	type trendPoint struct {
		Date      string `json:"date"`
		Planned   int    `json:"planned"`
		Completed int    `json:"completed"`
	}
	var resp struct {
		Organization struct {
			ID       uuid.UUID `json:"id"`
			Name     string    `json:"name"`
			Timezone string    `json:"timezone"`
		} `json:"organization"`
		GeneratedAt       time.Time         `json:"generated_at"`
		AsOf              string            `json:"as_of"`
		Metrics           domain.Metrics    `json:"metrics"`
		ProjectsAtRisk    int               `json:"projects_at_risk"`
		Projects          []projectJSON     `json:"projects"`
		Departments       []deptJSON        `json:"departments"`
		InterventionQueue []taskJSON        `json:"intervention_queue"`
		Trend             []trendPoint      `json:"trend"`
		Definitions       map[string]string `json:"definitions"`
	}
	resp.GeneratedAt = s.now().UTC()
	resp.Definitions = metricDefinitions

	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		o := &resp.Organization
		if err := tx.QueryRow(ctx, `SELECT id, name, reporting_timezone FROM organizations WHERE id = $1`, org).
			Scan(&o.ID, &o.Name, &o.Timezone); err != nil {
			return err
		}
		today := domain.Today(s.now(), o.Timezone)
		resp.AsOf = today.Format(time.DateOnly)

		rows, err := tx.Query(ctx, projectSelect+` AND p.tenant_id = $2 AND p.state = 'ACTIVE' ORDER BY p.due_on, p.name`, actor.UserID, org)
		if err != nil {
			return err
		}
		var ps []*projectRow
		for rows.Next() {
			p, err := scanProject(rows, actor)
			if err != nil {
				rows.Close()
				return err
			}
			ps = append(ps, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if resp.Projects, err = s.renderProjects(ctx, tx, actor, ps); err != nil {
			return err
		}

		trows, err := tx.Query(ctx, taskSelect+` AND t.tenant_id = $1 AND p.state = 'ACTIVE' ORDER BY t.due_on NULLS LAST`, org)
		if err != nil {
			return err
		}
		ts, err := scanTasks(trows)
		if err != nil {
			return err
		}

		deptOf := map[uuid.UUID]string{}
		access := map[uuid.UUID]authz.Access{}
		for _, p := range ps {
			deptOf[p.ID] = p.DepartmentName
			if deptOf[p.ID] == "" {
				deptOf[p.ID] = "Unassigned"
			}
			access[p.ID] = p.Access
		}
		all := []domain.TaskFacts{}
		byDept := map[string][]domain.TaskFacts{}
		window := map[string]*trendPoint{}
		for i := 6; i >= 0; i-- {
			d := today.AddDate(0, 0, -i).Format(time.DateOnly)
			resp.Trend = append(resp.Trend, trendPoint{Date: d})
		}
		for i := range resp.Trend {
			window[resp.Trend[i].Date] = &resp.Trend[i]
		}
		var queue []*taskRow
		for _, t := range ts {
			f := t.facts()
			all = append(all, f)
			byDept[deptOf[t.ProjectID]] = append(byDept[deptOf[t.ProjectID]], f)
			if t.Status == domain.StatusBlocked || f.Overdue(today) {
				queue = append(queue, t)
			}
			if t.DueOn != nil && t.Status != domain.StatusCancelled {
				if pt, ok := window[t.DueOn.Format(time.DateOnly)]; ok {
					pt.Planned++
				}
			}
			if t.CompletedAt != nil {
				if pt, ok := window[domain.LocalDate(*t.CompletedAt, o.Timezone).Format(time.DateOnly)]; ok {
					pt.Completed++
				}
			}
		}
		resp.Metrics = domain.Compute(all, today)
		for _, p := range resp.Projects {
			if p.Health == domain.HealthAtRisk {
				resp.ProjectsAtRisk++
			}
		}
		resp.Departments = []deptJSON{}
		for name, facts := range byDept {
			resp.Departments = append(resp.Departments, deptJSON{Name: name, Metrics: domain.Compute(facts, today)})
		}
		sort.Slice(resp.Departments, func(i, j int) bool { return resp.Departments[i].Name < resp.Departments[j].Name })
		if len(queue) > 20 {
			queue = queue[:20]
		}
		resp.InterventionQueue, err = s.renderTasks(ctx, tx, actor, queue, access)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, resp)
}
