package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

type taskRow struct {
	ID              uuid.UUID
	Number          int64
	WorkspaceID     uuid.UUID
	TenantID        *uuid.UUID
	ProjectID       uuid.UUID
	ProjectName     string
	ProjectColor    string
	Kind            string
	Title           string
	Description     string
	Status          string
	Priority        string
	OwnerID         uuid.UUID
	StartOn         *time.Time
	DueOn           *time.Time
	EstimateMinutes int
	BlockedReason   string
	CompletedAt     *time.Time
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CommentCount    int
}

const taskSelect = `
	SELECT t.id, t.number, t.workspace_id, t.tenant_id, t.project_id, p.name, p.color, w.kind, t.title, t.description,
	       t.status, t.priority, t.owner_id, t.start_on, t.due_on, t.estimate_minutes, t.blocked_reason, t.completed_at,
	       t.version, t.created_at, t.updated_at,
	       (SELECT count(*) FROM task_comments c WHERE c.task_id = t.id)
	FROM tasks t
	JOIN projects p ON p.id = t.project_id
	JOIN workspaces w ON w.id = t.workspace_id
	WHERE t.deleted_at IS NULL AND p.deleted_at IS NULL`

func scanTask(row pgx.Row) (*taskRow, error) {
	var t taskRow
	err := row.Scan(&t.ID, &t.Number, &t.WorkspaceID, &t.TenantID, &t.ProjectID, &t.ProjectName, &t.ProjectColor, &t.Kind,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.OwnerID, &t.StartOn, &t.DueOn, &t.EstimateMinutes,
		&t.BlockedReason, &t.CompletedAt, &t.Version, &t.CreatedAt, &t.UpdatedAt, &t.CommentCount)
	return &t, err
}

func scanTasks(rows pgx.Rows) ([]*taskRow, error) {
	defer rows.Close()
	var out []*taskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (t *taskRow) facts() domain.TaskFacts {
	return domain.TaskFacts{Status: t.Status, DueOn: t.DueOn, EstimateMinutes: t.EstimateMinutes}
}

// Task permissions derive from project access; the accountable owner may always
// view and progress their own assignment.
func taskPermissions(actor *authz.Actor, projectAccess authz.Access, t *taskRow) (read, edit bool) {
	own := t.OwnerID == actor.UserID && (t.TenantID == nil || actor.IsMember(*t.TenantID))
	read = projectAccess >= authz.AccessRead || own
	edit = authz.CanEditTask(projectAccess, t.OwnerID, actor.UserID) || own
	return read, edit
}

type taskJSON struct {
	ID            uuid.UUID  `json:"id"`
	Number        int64      `json:"number"`
	Key           string     `json:"key"`
	Project       taskProj   `json:"project"`
	WorkspaceKind string     `json:"workspace_kind"`
	TenantID      *uuid.UUID `json:"tenant_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	Owner         *userJSON  `json:"owner"`
	StartOn       *string    `json:"start_on"`
	DueOn         *string    `json:"due_on"`
	EstimateHours float64    `json:"estimate_hours"`
	BlockedReason string     `json:"blocked_reason"`
	CompletedAt   *time.Time `json:"completed_at"`
	Version       int        `json:"version"`
	Overdue       bool       `json:"overdue"`
	CommentCount  int        `json:"comment_count"`
	CanEdit       bool       `json:"can_edit"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type taskProj struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
}

func dateStr(d *time.Time) *string {
	if d == nil {
		return nil
	}
	s := d.Format(time.DateOnly)
	return &s
}

func (t *taskRow) toJSON(users map[uuid.UUID]userJSON, today time.Time, canEdit bool) taskJSON {
	out := taskJSON{
		ID: t.ID, Number: t.Number, Key: "TF-" + strconv.FormatInt(t.Number, 10),
		Project:       taskProj{ID: t.ProjectID, Name: t.ProjectName, Color: t.ProjectColor},
		WorkspaceKind: t.Kind, TenantID: t.TenantID, Title: t.Title, Description: t.Description,
		Status: t.Status, Priority: t.Priority, StartOn: dateStr(t.StartOn), DueOn: dateStr(t.DueOn),
		EstimateHours: math.Round(float64(t.EstimateMinutes)/60*10) / 10, BlockedReason: t.BlockedReason,
		CompletedAt: t.CompletedAt, Version: t.Version, Overdue: t.facts().Overdue(today),
		CommentCount: t.CommentCount, CanEdit: canEdit, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	if u, ok := users[t.OwnerID]; ok {
		out.Owner = &u
	}
	return out
}

// renderTasks attaches owners and per-caller edit capability.
func (s *Server) renderTasks(ctx context.Context, tx pgx.Tx, actor *authz.Actor, ts []*taskRow, access map[uuid.UUID]authz.Access) ([]taskJSON, error) {
	z, err := loadZones(ctx, tx, actor.UserID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(ts))
	for _, t := range ts {
		ids = append(ids, t.OwnerID)
	}
	users, err := loadUsers(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]taskJSON, 0, len(ts))
	for _, t := range ts {
		_, edit := taskPermissions(actor, access[t.ProjectID], t)
		out = append(out, t.toJSON(users, z.today(s.now(), t.TenantID), edit))
	}
	return out, nil
}

func (s *Server) loadTask(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (*taskRow, *projectRow, bool, error) {
	t, err := scanTask(tx.QueryRow(ctx, taskSelect+` AND t.id = $1`, id))
	if err != nil {
		if store.IsNoRows(err) {
			return nil, nil, false, errNotFound()
		}
		return nil, nil, false, err
	}
	p, err := scanProject(tx.QueryRow(ctx, projectSelect+` AND p.id = $2`, actor.UserID, t.ProjectID), actor)
	if err != nil {
		if store.IsNoRows(err) {
			return nil, nil, false, errNotFound()
		}
		return nil, nil, false, err
	}
	read, edit := taskPermissions(actor, p.Access, t)
	if !read {
		return nil, nil, false, errNotFound()
	}
	return t, p, edit, nil
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var out []taskJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		t, p, _, err := s.loadTask(r.Context(), tx, actor, id)
		if err != nil {
			return err
		}
		out, err = s.renderTasks(r.Context(), tx, actor, []*taskRow{t}, map[uuid.UUID]authz.Access{p.ID: p.Access})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	writeJSON(w, r, 200, out[0])
}

func (s *Server) listProjectTasks(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var out []taskJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, taskSelect+` AND t.project_id = $1 ORDER BY t.due_on NULLS LAST, t.number`, id)
		if err != nil {
			return err
		}
		ts, err := scanTasks(rows)
		if err != nil {
			return err
		}
		out, err = s.renderTasks(ctx, tx, actor, ts, map[uuid.UUID]authz.Access{p.ID: p.Access})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

func (s *Server) projectBoard(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type lane struct {
		Status    string     `json:"status"`
		WIPLimit  int        `json:"wip_limit"`
		OverLimit bool       `json:"over_limit"`
		Tasks     []taskJSON `json:"tasks"`
	}
	lanes := []lane{}
	var projectOut []projectJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if projectOut, err = s.renderProjects(ctx, tx, actor, []*projectRow{p}); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, taskSelect+` AND t.project_id = $1 AND t.status <> 'CANCELLED'
			ORDER BY CASE t.priority WHEN 'URGENT' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 ELSE 3 END, t.due_on NULLS LAST`, id)
		if err != nil {
			return err
		}
		ts, err := scanTasks(rows)
		if err != nil {
			return err
		}
		rendered, err := s.renderTasks(ctx, tx, actor, ts, map[uuid.UUID]authz.Access{p.ID: p.Access})
		if err != nil {
			return err
		}
		for _, st := range domain.Statuses[:5] {
			l := lane{Status: st, WIPLimit: 4, Tasks: []taskJSON{}}
			for _, t := range rendered {
				if t.Status == st {
					l.Tasks = append(l.Tasks, t)
				}
			}
			l.OverLimit = st != domain.StatusDone && len(l.Tasks) > l.WIPLimit
			lanes = append(lanes, l)
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"project": projectOut[0], "lanes": lanes})
}

// myWorkItems is the union of the caller's private tasks and their assignments
// in the selected organization. Private items never enter tenant queries.
func (s *Server) myWorkItems(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	scope := store.Scope{UserID: actor.UserID}
	var org *uuid.UUID
	if v := r.URL.Query().Get("org"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil || !actor.IsMember(id) {
			s.fail(w, r, errNotFound())
			return
		}
		org = &id
		scope.TenantID = org
	}
	var out []taskJSON
	var metrics domain.Metrics
	var today time.Time
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, taskSelect+`
			AND t.owner_id = $1 AND p.state = 'ACTIVE'
			AND (w.kind = 'PERSONAL' OR ($2::uuid IS NOT NULL AND t.tenant_id = $2))
			ORDER BY t.due_on NULLS LAST, t.number`, actor.UserID, org)
		if err != nil {
			return err
		}
		ts, err := scanTasks(rows)
		if err != nil {
			return err
		}
		access := map[uuid.UUID]authz.Access{}
		facts := make([]domain.TaskFacts, 0, len(ts))
		for _, t := range ts {
			facts = append(facts, t.facts())
		}
		z, err := loadZones(ctx, tx, actor.UserID)
		if err != nil {
			return err
		}
		today = domain.Today(s.now(), z.user)
		metrics = domain.Compute(facts, today)
		out, err = s.renderTasks(ctx, tx, actor, ts, access)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out, "metrics": metrics, "today": today.Format(time.DateOnly), "org_id": org})
}

type taskInput struct {
	Title         *string    `json:"title"`
	Description   *string    `json:"description"`
	Status        *string    `json:"status"`
	Priority      *string    `json:"priority"`
	OwnerID       *uuid.UUID `json:"owner_id"`
	StartOn       *string    `json:"start_on"`
	DueOn         *string    `json:"due_on"`
	EstimateHours *float64   `json:"estimate_hours"`
	BlockedReason *string    `json:"blocked_reason"`
}

// taskValues is the validated state of a task after applying input.
type taskValues struct {
	title, description, status, priority, blockedReason string
	owner                                               uuid.UUID
	start, due                                          *time.Time
	estimateMinutes                                     int
}

func applyTaskInput(v *taskValues, in taskInput) map[string]string {
	errs := map[string]string{}
	if in.Title != nil {
		v.title = strings.TrimSpace(*in.Title)
	}
	if v.title == "" || len(v.title) > 180 {
		errs["title"] = "Give this task a title (up to 180 characters)."
	}
	if in.Description != nil {
		v.description = strings.TrimSpace(*in.Description)
	}
	if len(v.description) > 3000 {
		errs["description"] = "Keep the description under 3000 characters."
	}
	if in.Status != nil {
		v.status = *in.Status
	}
	if !domain.ValidStatus(v.status) {
		errs["status"] = "Unknown status."
	}
	if in.Priority != nil {
		v.priority = *in.Priority
	}
	if !domain.ValidPriority(v.priority) {
		errs["priority"] = "Unknown priority."
	}
	if in.OwnerID != nil {
		v.owner = *in.OwnerID
	}
	if in.StartOn != nil {
		if *in.StartOn == "" {
			v.start = nil
		} else {
			d := parseDate("start_on", *in.StartOn, errs)
			v.start = &d
		}
	}
	if in.DueOn != nil {
		if *in.DueOn == "" {
			v.due = nil
		} else {
			d := parseDate("due_on", *in.DueOn, errs)
			v.due = &d
		}
	}
	if v.start != nil && v.due != nil && v.due.Before(*v.start) {
		errs["due_on"] = "The due date must be on or after the start date."
	}
	if in.EstimateHours != nil {
		h := *in.EstimateHours
		if h < 0 || h > 500 || math.IsNaN(h) {
			errs["estimate_hours"] = "Estimate must be between 0 and 500 hours."
		} else {
			v.estimateMinutes = int(math.Round(h * 60))
		}
	}
	if in.BlockedReason != nil {
		v.blockedReason = strings.TrimSpace(*in.BlockedReason)
	}
	if len(v.blockedReason) > 1000 {
		errs["blocked_reason"] = "Keep the blocked reason under 1000 characters."
	}
	if v.status == domain.StatusBlocked && v.blockedReason == "" {
		errs["blocked_reason"] = "Add a short dependency or blocked reason."
	}
	return errs
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	projectID, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.fail(w, r, &httpError{413, "PAYLOAD_TOO_LARGE", "The request body is too large.", nil})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var in taskInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	sum := sha256.Sum256(append([]byte(projectID.String()+"\n"), raw...))
	reqHash := hex.EncodeToString(sum[:])

	status := http.StatusCreated
	var out []taskJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, projectID)
		if err != nil {
			return err
		}
		if idemKey != "" {
			var existingHash string
			var existingID uuid.UUID
			err := tx.QueryRow(ctx, `SELECT request_hash, resource_id FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
				actor.UserID, idemKey).Scan(&existingHash, &existingID)
			if err == nil {
				if existingHash != reqHash {
					return errUnprocessable("IDEMPOTENCY_KEY_REUSED", "This Idempotency-Key was already used for a different request.")
				}
				t, tp, _, err := s.loadTask(ctx, tx, actor, existingID)
				if err != nil {
					return err
				}
				status = http.StatusOK
				out, err = s.renderTasks(ctx, tx, actor, []*taskRow{t}, map[uuid.UUID]authz.Access{tp.ID: tp.Access})
				return err
			} else if !store.IsNoRows(err) {
				return err
			}
		}
		if p.Access < authz.AccessContribute {
			return errForbidden("You can view this project but not add tasks to it.")
		}
		if p.State != "ACTIVE" {
			return errUnprocessable("PROJECT_ARCHIVED", "Restore the project before adding tasks.")
		}
		v := taskValues{status: domain.StatusTodo, priority: "MEDIUM", owner: actor.UserID}
		errs := applyTaskInput(&v, in)
		if v.status == domain.StatusCancelled {
			errs["status"] = "New tasks cannot start as cancelled."
		}
		if len(errs) > 0 {
			return errValidation(errs)
		}
		if v.owner != actor.UserID {
			if err := checkAssignable(ctx, tx, p, v.owner); err != nil {
				return err
			}
		}
		var completed *time.Time
		if v.status == domain.StatusDone {
			now := s.now().UTC()
			completed = &now
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO tasks (workspace_id, project_id, title, description, status, priority, owner_id, creator_id,
			                   start_on, due_on, estimate_minutes, blocked_reason, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
			p.WorkspaceID, p.ID, v.title, v.description, v.status, v.priority, v.owner, actor.UserID,
			v.start, v.due, v.estimateMinutes, v.blockedReason, completed).Scan(&id); err != nil {
			return err
		}
		if idemKey != "" {
			if len(idemKey) < 8 || len(idemKey) > 128 {
				return errValidation(map[string]string{"Idempotency-Key": "Use 8 to 128 characters."})
			}
			tag, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (actor_id, key, request_hash, resource_type, resource_id)
				VALUES ($1, $2, $3, 'task', $4) ON CONFLICT DO NOTHING`, actor.UserID, idemKey, reqHash, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return &httpError{409, "IDEMPOTENCY_IN_PROGRESS", "A request with this Idempotency-Key is already being processed.", nil}
			}
		}
		if err := audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: "task.created",
			TargetType: "task", TargetID: &id, Summary: "created task " + v.title,
			Metadata: map[string]any{"project_id": p.ID, "owner_id": v.owner, "status": v.status}}); err != nil {
			return err
		}
		t, _, _, err := s.loadTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err = s.renderTasks(ctx, tx, actor, []*taskRow{t}, map[uuid.UUID]authz.Access{p.ID: p.Access})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	w.Header().Set("Location", "/api/v1/tasks/"+out[0].ID.String())
	writeJSON(w, r, status, out[0])
}

func checkAssignable(ctx context.Context, tx pgx.Tx, p *projectRow, owner uuid.UUID) error {
	if p.Access != authz.AccessManage {
		return errForbidden("Only project managers can assign work to someone else.")
	}
	levels, err := assignableUsers(ctx, tx, p)
	if err != nil {
		return err
	}
	if _, ok := levels[owner]; !ok {
		return errValidation(map[string]string{"owner_id": "The owner must be a contributing member of this project."})
	}
	return nil
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in taskInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	var out []taskJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, p, canEdit, err := s.loadTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !canEdit {
			return errForbidden("You can view this task but not change it.")
		}
		if t.Version != version {
			return errVersionConflict()
		}
		v := taskValues{title: t.Title, description: t.Description, status: t.Status, priority: t.Priority,
			blockedReason: t.BlockedReason, owner: t.OwnerID, start: t.StartOn, due: t.DueOn, estimateMinutes: t.EstimateMinutes}
		errs := applyTaskInput(&v, in)
		if len(errs) > 0 {
			return errValidation(errs)
		}
		if !domain.CanTransition(t.Status, v.status) {
			return errUnprocessable("INVALID_TRANSITION", "A task cannot move from "+t.Status+" to "+v.status+".")
		}
		if v.owner != t.OwnerID {
			if err := checkAssignable(ctx, tx, p, v.owner); err != nil {
				return err
			}
		}
		completed := t.CompletedAt
		if v.status == domain.StatusDone && t.Status != domain.StatusDone {
			now := s.now().UTC()
			completed = &now
		} else if v.status != domain.StatusDone {
			completed = nil
		}
		tag, err := tx.Exec(ctx, `
			UPDATE tasks SET title = $1, description = $2, status = $3, priority = $4, owner_id = $5, start_on = $6, due_on = $7,
			       estimate_minutes = $8, blocked_reason = $9, completed_at = $10, version = version + 1, updated_at = now()
			WHERE id = $11 AND version = $12`,
			v.title, v.description, v.status, v.priority, v.owner, v.start, v.due, v.estimateMinutes, v.blockedReason, completed, id, version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errVersionConflict()
		}
		base := auditEntry{Tenant: t.TenantID, Workspace: &t.WorkspaceID, Actor: actor.UserID, TargetType: "task", TargetID: &id}
		var events []auditEntry
		if v.status != t.Status {
			e := base
			e.Action, e.Summary = "task.status_changed", "moved "+v.title+" to "+v.status
			if domain.IsReopen(t.Status, v.status) {
				e.Action, e.Summary = "task.reopened", "reopened "+v.title
			}
			e.Metadata = map[string]any{"from": t.Status, "to": v.status}
			events = append(events, e)
		}
		if v.owner != t.OwnerID {
			e := base
			e.Action, e.Summary = "task.assigned", "reassigned "+v.title
			e.Metadata = map[string]any{"from": t.OwnerID, "to": v.owner}
			events = append(events, e)
		}
		if len(events) == 0 || in.Title != nil || in.Description != nil || in.Priority != nil || in.DueOn != nil || in.StartOn != nil || in.EstimateHours != nil {
			e := base
			e.Action, e.Summary = "task.updated", "updated "+v.title
			events = append(events, e)
		}
		for _, e := range events {
			if err := audit(ctx, tx, e); err != nil {
				return err
			}
		}
		t, p, _, err = s.loadTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err = s.renderTasks(ctx, tx, actor, []*taskRow{t}, map[uuid.UUID]authz.Access{p.ID: p.Access})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	writeJSON(w, r, 200, out[0])
}

type commentJSON struct {
	ID        uuid.UUID `json:"id"`
	Author    *userJSON `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []commentJSON{}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, _, _, err := s.loadTask(ctx, tx, actor, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, author_id, body, created_at FROM task_comments WHERE task_id = $1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		var authors []uuid.UUID
		for rows.Next() {
			var c commentJSON
			var a uuid.UUID
			if err := rows.Scan(&c.ID, &a, &c.Body, &c.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out = append(out, c)
			authors = append(authors, a)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		users, err := loadUsers(ctx, tx, authors)
		if err != nil {
			return err
		}
		for i := range out {
			if u, ok := users[authors[i]]; ok {
				out[i].Author = &u
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

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || len(body) > 1500 {
		s.fail(w, r, errValidation(map[string]string{"body": "Write an update of up to 1500 characters."}))
		return
	}
	var out commentJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, p, canEdit, err := s.loadTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !canEdit && p.Access < authz.AccessContribute {
			return errForbidden("You can view this task but not comment on it.")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO task_comments (task_id, workspace_id, author_id, body) VALUES ($1, $2, $3, $4)
			RETURNING id, created_at`, id, t.WorkspaceID, actor.UserID, body).Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		out.Body = body
		users, err := loadUsers(ctx, tx, []uuid.UUID{actor.UserID})
		if err != nil {
			return err
		}
		u := users[actor.UserID]
		out.Author = &u
		return audit(ctx, tx, auditEntry{Tenant: t.TenantID, Workspace: &t.WorkspaceID, Actor: actor.UserID, Action: "comment.created",
			TargetType: "task", TargetID: &id, Summary: "commented on " + t.Title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

func sortMembers[T any](items []T, key func(T) string) {
	sort.Slice(items, func(i, j int) bool { return key(items[i]) < key(items[j]) })
}
