package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

const reportItemLimit = 200

// reportFilter narrows the org task register. Period counts days ending today by due date.
type reportFilter struct {
	Period       string     `json:"period"`
	DepartmentID *uuid.UUID `json:"department_id,omitempty"`
}

func parseReportFilter(q url.Values) (reportFilter, error) {
	f := reportFilter{Period: q.Get("period")}
	if f.Period == "" {
		f.Period = "all"
	}
	errs := map[string]string{}
	if f.Period != "all" && f.Period != "30" && f.Period != "7" {
		errs["period"] = "Use all, 30 or 7."
	}
	if v := q.Get("department_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			errs["department_id"] = "Unknown department."
		} else {
			f.DepartmentID = &id
		}
	}
	if len(errs) > 0 {
		return f, errValidation(errs)
	}
	return f, nil
}

type trendPoint struct {
	Date      string `json:"date"`
	Planned   int    `json:"planned"`
	Completed int    `json:"completed"`
}

// buildTrend counts planned (due) and completed tasks per day for the 7 days ending today.
func buildTrend(ts []*taskRow, today time.Time, tz string) []trendPoint {
	out := make([]trendPoint, 0, 7)
	index := map[string]int{}
	for i := 6; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format(time.DateOnly)
		index[d] = len(out)
		out = append(out, trendPoint{Date: d})
	}
	for _, t := range ts {
		if t.DueOn != nil && t.Status != domain.StatusCancelled {
			if i, ok := index[t.DueOn.Format(time.DateOnly)]; ok {
				out[i].Planned++
			}
		}
		if t.CompletedAt != nil {
			if i, ok := index[domain.LocalDate(*t.CompletedAt, tz).Format(time.DateOnly)]; ok {
				out[i].Completed++
			}
		}
	}
	return out
}

type reportData struct {
	orgName string
	tz      string
	today   time.Time
	tasks   []*taskRow
	access  map[uuid.UUID]authz.Access
	deptOf  map[uuid.UUID]string
}

// loadReport returns org-only tasks in readable active projects matching the filter.
func (s *Server) loadReport(ctx context.Context, tx pgx.Tx, actor *authz.Actor, org uuid.UUID, f reportFilter) (*reportData, error) {
	d := &reportData{access: map[uuid.UUID]authz.Access{}, deptOf: map[uuid.UUID]string{}}
	if err := tx.QueryRow(ctx, `SELECT name, reporting_timezone FROM organizations WHERE id = $1`, org).Scan(&d.orgName, &d.tz); err != nil {
		return nil, err
	}
	d.today = domain.Today(s.now(), d.tz)
	rows, err := tx.Query(ctx, projectSelect+` AND p.tenant_id = $2 AND p.state = 'ACTIVE' AND ($3::uuid IS NULL OR p.department_id = $3)`,
		actor.UserID, org, f.DepartmentID)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		p, err := scanProject(rows, actor)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if p.Access >= authz.AccessRead {
			ids = append(ids, p.ID)
			d.access[p.ID] = p.Access
			d.deptOf[p.ID] = p.DepartmentName
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var from *time.Time
	if f.Period != "all" {
		n, _ := strconv.Atoi(f.Period)
		x := d.today.AddDate(0, 0, -(n - 1))
		from = &x
	}
	trows, err := tx.Query(ctx, taskSelect+` AND t.tenant_id = $1 AND t.project_id = ANY($2)
		AND ($3::date IS NULL OR t.due_on BETWEEN $3 AND $4)
		ORDER BY t.due_on NULLS LAST, t.number`, org, ids, from, d.today)
	if err != nil {
		return nil, err
	}
	d.tasks, err = scanTasks(trows)
	return d, err
}

func (s *Server) reportAccess(w http.ResponseWriter, r *http.Request, p authz.Permission) (*authz.Actor, uuid.UUID, store.Scope, bool) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return nil, org, store.Scope{}, false
	}
	scope, err := orgScope(actor, org)
	if err != nil {
		s.fail(w, r, err)
		return nil, org, scope, false
	}
	if !actor.Can(org, p) {
		msg := "Your role does not include reporting for this organization."
		if p == authz.ReportExport {
			msg = "Your role does not include report exports for this organization."
		}
		s.fail(w, r, errForbidden(msg))
		return nil, org, scope, false
	}
	return actor, org, scope, true
}

func (s *Server) reportTasks(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.reportAccess(w, r, authz.ReportRead)
	if !ok {
		return
	}
	f, err := parseReportFilter(r.URL.Query())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := map[string]any{"filters": f, "definitions": metricDefinitions, "can_export": actor.Can(org, authz.ReportExport)}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		d, err := s.loadReport(ctx, tx, actor, org, f)
		if err != nil {
			return err
		}
		facts := make([]domain.TaskFacts, 0, len(d.tasks))
		counts := map[string]int{}
		var risk []*taskRow
		for _, t := range d.tasks {
			facts = append(facts, t.facts())
			counts[t.Status]++
			if t.Status == domain.StatusBlocked || t.facts().Overdue(d.today) {
				risk = append(risk, t)
			}
		}
		shown := d.tasks
		if len(shown) > reportItemLimit {
			shown = shown[:reportItemLimit]
		}
		items, err := s.renderTasks(ctx, tx, actor, shown, d.access)
		if err != nil {
			return err
		}
		riskItems, err := s.renderTasks(ctx, tx, actor, risk, d.access)
		if err != nil {
			return err
		}
		resp["organization"] = d.orgName
		resp["as_of"] = d.today.Format(time.DateOnly)
		resp["metrics"] = domain.Compute(facts, d.today)
		resp["status_counts"] = counts
		resp["trend"] = buildTrend(d.tasks, d.today, d.tz)
		resp["total"] = len(d.tasks)
		resp["items"] = items
		resp["risk"] = riskItems
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, resp)
}

// csvSafe neutralizes spreadsheet formula injection.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) exportTasks(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.reportAccess(w, r, authz.ReportExport)
	if !ok {
		return
	}
	f, err := parseReportFilter(r.URL.Query())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var buf bytes.Buffer
	var name string
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		d, err := s.loadReport(ctx, tx, actor, org, f)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(d.tasks))
		for _, t := range d.tasks {
			ids = append(ids, t.OwnerID)
		}
		users, err := loadUsers(ctx, tx, ids)
		if err != nil {
			return err
		}
		buf.WriteString("\ufeff")
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{"ID", "Title", "Project", "Department", "Owner", "Status", "Priority", "Start", "Due", "Estimate hours", "Overdue"})
		for _, t := range d.tasks {
			_ = cw.Write([]string{
				"TF-" + strconv.FormatInt(t.Number, 10), csvSafe(t.Title), csvSafe(t.ProjectName), csvSafe(d.deptOf[t.ProjectID]),
				csvSafe(users[t.OwnerID].DisplayName), t.Status, t.Priority, deref(dateStr(t.StartOn)), deref(dateStr(t.DueOn)),
				strconv.FormatFloat(float64(t.EstimateMinutes)/60, 'f', 1, 64), strconv.FormatBool(t.facts().Overdue(d.today)),
			})
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
		name = "TaskForce_report_" + d.today.Format(time.DateOnly) + ".csv"
		raw, _ := json.Marshal(f)
		var exportID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO report_exports (tenant_id, actor_id, name, row_count, filters) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			org, actor.UserID, name, len(d.tasks), raw).Scan(&exportID); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "report.exported", TargetType: "report_export", TargetID: &exportID,
			Summary: "exported " + strconv.Itoa(len(d.tasks)) + " task records", Metadata: map[string]any{"period": f.Period, "department_id": f.DepartmentID}})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) listReportExports(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.reportAccess(w, r, authz.ReportRead)
	if !ok {
		return
	}
	type exportJSON struct {
		ID        uuid.UUID       `json:"id"`
		Name      string          `json:"name"`
		RowCount  int             `json:"row_count"`
		Filters   json.RawMessage `json:"filters"`
		CreatedAt time.Time       `json:"created_at"`
	}
	out := []exportJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, name, row_count, filters, created_at FROM report_exports
			WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 50`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e exportJSON
			if err := rows.Scan(&e.ID, &e.Name, &e.RowCount, &e.Filters, &e.CreatedAt); err != nil {
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

type reportViewJSON struct {
	ID        uuid.UUID    `json:"id"`
	Name      string       `json:"name"`
	Filters   reportFilter `json:"filters"`
	CreatedAt time.Time    `json:"created_at"`
}

func (s *Server) listReportViews(w http.ResponseWriter, r *http.Request) {
	_, org, scope, ok := s.reportAccess(w, r, authz.ReportRead)
	if !ok {
		return
	}
	out := []reportViewJSON{}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, name, filters, created_at FROM report_views WHERE tenant_id = $1 ORDER BY name`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v reportViewJSON
			if err := rows.Scan(&v.ID, &v.Name, &v.Filters, &v.CreatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

func (s *Server) createReportView(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.reportAccess(w, r, authz.ReportRead)
	if !ok {
		return
	}
	var in struct {
		Name         string     `json:"name"`
		Period       string     `json:"period"`
		DepartmentID *uuid.UUID `json:"department_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	q := url.Values{"period": {in.Period}}
	if in.DepartmentID != nil {
		q.Set("department_id", in.DepartmentID.String())
	}
	f, err := parseReportFilter(q)
	if err == nil && (in.Name == "" || len(in.Name) > 80) {
		err = errValidation(map[string]string{"name": "Name this view (up to 80 characters)."})
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var v reportViewJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		if f.DepartmentID != nil {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM departments WHERE id = $1 AND tenant_id = $2)`, f.DepartmentID, org).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return errValidation(map[string]string{"department_id": "Choose a department in this organization."})
			}
		}
		raw, _ := json.Marshal(f)
		v.Name, v.Filters = in.Name, f
		return tx.QueryRow(ctx, `INSERT INTO report_views (tenant_id, owner_id, name, filters) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
			org, actor.UserID, in.Name, raw).Scan(&v.ID, &v.CreatedAt)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, v)
}

func (s *Server) deleteReportView(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "viewId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM report_views WHERE id = $1`, id)
		if err == nil && tag.RowsAffected() == 0 {
			return errNotFound()
		}
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
