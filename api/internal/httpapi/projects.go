package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/domain"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

type projectRow struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	Kind           string
	TenantID       *uuid.UUID
	PersonalOwner  *uuid.UUID
	Name           string
	Description    string
	DepartmentID   *uuid.UUID
	DepartmentName string
	OwnerID        uuid.UUID
	State          string
	StartOn        time.Time
	DueOn          time.Time
	Color          string
	Icon           string
	Version        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	MemberAccess   string
	Access         authz.Access
}

const projectSelect = `
	SELECT p.id, p.workspace_id, w.kind, p.tenant_id, w.owner_user_id, p.name, p.description,
	       p.department_id, coalesce(d.name, ''), p.owner_id, p.state, p.start_on, p.due_on, p.color, p.icon,
	       p.version, p.created_at, p.updated_at,
	       coalesce((SELECT pm.access_level FROM project_memberships pm WHERE pm.project_id = p.id AND pm.user_id = $1), '')
	FROM projects p
	JOIN workspaces w ON w.id = p.workspace_id
	LEFT JOIN departments d ON d.id = p.department_id
	WHERE p.deleted_at IS NULL`

func scanProject(row pgx.Row, actor *authz.Actor) (*projectRow, error) {
	var p projectRow
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Kind, &p.TenantID, &p.PersonalOwner, &p.Name, &p.Description,
		&p.DepartmentID, &p.DepartmentName, &p.OwnerID, &p.State, &p.StartOn, &p.DueOn, &p.Color, &p.Icon,
		&p.Version, &p.CreatedAt, &p.UpdatedAt, &p.MemberAccess); err != nil {
		return nil, err
	}
	p.Access = actor.ProjectAccess(authz.ProjectRef{TenantID: p.TenantID, PersonalOwner: p.PersonalOwner, OwnerID: p.OwnerID, MemberAccess: p.MemberAccess})
	return &p, nil
}

// loadProject resolves ownership and evaluates permission; a guessed ID is never enough.
func loadProject(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (*projectRow, error) {
	p, err := scanProject(tx.QueryRow(ctx, projectSelect+` AND p.id = $2`, actor.UserID, id), actor)
	if err != nil {
		if store.IsNoRows(err) {
			return nil, errNotFound()
		}
		return nil, err
	}
	if p.Access == authz.AccessNone {
		return nil, errNotFound()
	}
	return p, nil
}

// zones resolves "today" per scope: tenant reporting timezone for org work,
// the user's own timezone for personal work.
type zones struct {
	user string
	orgs map[uuid.UUID]string
}

func loadZones(ctx context.Context, tx pgx.Tx, me uuid.UUID) (*zones, error) {
	z := &zones{user: "UTC", orgs: map[uuid.UUID]string{}}
	if err := tx.QueryRow(ctx, `SELECT timezone FROM users WHERE id = $1`, me).Scan(&z.user); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, reporting_timezone FROM organizations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var tz string
		if err := rows.Scan(&id, &tz); err != nil {
			return nil, err
		}
		z.orgs[id] = tz
	}
	return z, rows.Err()
}

func (z *zones) tz(tenant *uuid.UUID) string {
	if tenant != nil {
		if tz, ok := z.orgs[*tenant]; ok {
			return tz
		}
	}
	return z.user
}

func (z *zones) today(now time.Time, tenant *uuid.UUID) time.Time {
	return domain.Today(now, z.tz(tenant))
}

func loadFacts(ctx context.Context, tx pgx.Tx, projectIDs []uuid.UUID, tzOf func(uuid.UUID) string) (map[uuid.UUID][]domain.TaskFacts, error) {
	out := map[uuid.UUID][]domain.TaskFacts{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT project_id, status, due_on, completed_at, estimate_minutes FROM tasks
		WHERE project_id = ANY($1) AND deleted_at IS NULL`, projectIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid uuid.UUID
		var f domain.TaskFacts
		var completed *time.Time
		if err := rows.Scan(&pid, &f.Status, &f.DueOn, &completed, &f.EstimateMinutes); err != nil {
			return nil, err
		}
		if completed != nil {
			d := domain.LocalDate(*completed, tzOf(pid))
			f.CompletedOn = &d
		}
		out[pid] = append(out[pid], f)
	}
	return out, rows.Err()
}

type projectJSON struct {
	ID            uuid.UUID      `json:"id"`
	WorkspaceID   uuid.UUID      `json:"workspace_id"`
	WorkspaceKind string         `json:"workspace_kind"`
	TenantID      *uuid.UUID     `json:"tenant_id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	DepartmentID  *uuid.UUID     `json:"department_id"`
	Department    string         `json:"department"`
	Owner         *userJSON      `json:"owner"`
	State         string         `json:"state"`
	StartOn       string         `json:"start_on"`
	DueOn         string         `json:"due_on"`
	Color         string         `json:"color"`
	Icon          string         `json:"icon"`
	Version       int            `json:"version"`
	Access        string         `json:"access"`
	Metrics       domain.Metrics `json:"metrics"`
	Health        string         `json:"health"`
	MemberIDs     []uuid.UUID    `json:"member_ids"`
	Members       []userJSON     `json:"members"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func (p *projectRow) toJSON(users map[uuid.UUID]userJSON, facts []domain.TaskFacts, today time.Time, members []uuid.UUID) projectJSON {
	m := domain.Compute(facts, today)
	out := projectJSON{
		ID: p.ID, WorkspaceID: p.WorkspaceID, WorkspaceKind: p.Kind, TenantID: p.TenantID, Name: p.Name,
		Description: p.Description, DepartmentID: p.DepartmentID, Department: p.DepartmentName, State: p.State,
		StartOn: p.StartOn.Format(time.DateOnly), DueOn: p.DueOn.Format(time.DateOnly), Color: p.Color, Icon: p.Icon,
		Version: p.Version, Access: p.Access.String(), Metrics: m, Health: domain.Health(m),
		MemberIDs: members, Members: []userJSON{}, UpdatedAt: p.UpdatedAt,
	}
	if p.Kind == "PERSONAL" {
		out.Department = "Personal"
	}
	if u, ok := users[p.OwnerID]; ok {
		out.Owner = &u
	}
	for _, id := range members {
		if u, ok := users[id]; ok {
			out.Members = append(out.Members, u)
		}
	}
	return out
}

func projectMemberIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT project_id, user_id FROM project_memberships WHERE project_id = ANY($1) ORDER BY created_at`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, uid uuid.UUID
		if err := rows.Scan(&pid, &uid); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], uid)
	}
	return out, rows.Err()
}

// renderProjects attaches owners, members and server-computed metrics.
func (s *Server) renderProjects(ctx context.Context, tx pgx.Tx, actor *authz.Actor, ps []*projectRow) ([]projectJSON, error) {
	z, err := loadZones(ctx, tx, actor.UserID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(ps))
	tenantOf := map[uuid.UUID]*uuid.UUID{}
	for i, p := range ps {
		ids[i] = p.ID
		tenantOf[p.ID] = p.TenantID
	}
	facts, err := loadFacts(ctx, tx, ids, func(pid uuid.UUID) string { return z.tz(tenantOf[pid]) })
	if err != nil {
		return nil, err
	}
	members, err := projectMemberIDs(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	userIDs := []uuid.UUID{}
	for _, p := range ps {
		userIDs = append(userIDs, p.OwnerID)
		if p.Kind == "PERSONAL" && len(members[p.ID]) == 0 {
			members[p.ID] = []uuid.UUID{p.OwnerID}
		}
		userIDs = append(userIDs, members[p.ID]...)
	}
	users, err := loadUsers(ctx, tx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make([]projectJSON, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.toJSON(users, facts[p.ID], z.today(s.now(), p.TenantID), members[p.ID]))
	}
	return out, nil
}

type workspaceRow struct {
	ID       uuid.UUID
	Kind     string
	TenantID *uuid.UUID
	Owner    *uuid.UUID
}

func (s *Server) workspaceScope(r *http.Request, actor *authz.Actor) (*workspaceRow, store.Scope, error) {
	wsID, err := pathUUID(r, "workspaceId")
	if err != nil {
		return nil, store.Scope{}, err
	}
	var ws workspaceRow
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT id, kind, tenant_id, owner_user_id FROM workspaces WHERE id = $1`, wsID).
			Scan(&ws.ID, &ws.Kind, &ws.TenantID, &ws.Owner)
	})
	if err != nil {
		if store.IsNoRows(err) {
			return nil, store.Scope{}, errNotFound()
		}
		return nil, store.Scope{}, err
	}
	if ws.Kind == "PERSONAL" {
		if ws.Owner == nil || *ws.Owner != actor.UserID {
			return nil, store.Scope{}, errNotFound()
		}
		return &ws, store.Scope{UserID: actor.UserID}, nil
	}
	scope, err := orgScope(actor, *ws.TenantID)
	return &ws, scope, err
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	ws, scope, err := s.workspaceScope(r, actor)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	state := "ACTIVE"
	if r.URL.Query().Get("state") == "ARCHIVED" {
		state = "ARCHIVED"
	}
	readAll := ws.Kind == "PERSONAL" || actor.Can(*ws.TenantID, authz.ProjectReadAll)
	var out []projectJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), projectSelect+`
			AND p.workspace_id = $2 AND p.state = $3
			AND ($4 OR p.owner_id = $1 OR EXISTS (SELECT 1 FROM project_memberships pm WHERE pm.project_id = p.id AND pm.user_id = $1))
			ORDER BY p.due_on, p.name`, actor.UserID, ws.ID, state, readAll)
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
			if p.Access != authz.AccessNone {
				ps = append(ps, p)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		out, err = s.renderProjects(r.Context(), tx, actor, ps)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	canCreate := ws.Kind == "PERSONAL" || actor.Can(*ws.TenantID, authz.ProjectCreate)
	writeJSON(w, r, 200, map[string]any{"items": out, "can_create": canCreate})
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type projectInput struct {
	Name         *string    `json:"name"`
	Description  *string    `json:"description"`
	DepartmentID *uuid.UUID `json:"department_id"`
	OwnerID      *uuid.UUID `json:"owner_id"`
	StartOn      *string    `json:"start_on"`
	DueOn        *string    `json:"due_on"`
	Color        *string    `json:"color"`
	State        *string    `json:"state"`
}

func parseDate(field, v string, errs map[string]string) time.Time {
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		errs[field] = "Use a YYYY-MM-DD date."
	}
	return d
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	ws, scope, err := s.workspaceScope(r, actor)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if ws.Kind == "ORG" && !actor.Can(*ws.TenantID, authz.ProjectCreate) {
		s.fail(w, r, errForbidden("Your role cannot create organization projects."))
		return
	}
	var in projectInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	name := strings.TrimSpace(deref(in.Name))
	if name == "" || len(name) > 100 {
		errs["name"] = "Give this project a name (up to 100 characters)."
	}
	desc := strings.TrimSpace(deref(in.Description))
	if len(desc) > 1000 {
		errs["description"] = "Keep the description under 1000 characters."
	}
	start := parseDate("start_on", deref(in.StartOn), errs)
	due := parseDate("due_on", deref(in.DueOn), errs)
	if len(errs) == 0 && due.Before(start) {
		errs["due_on"] = "Target delivery must be on or after the start date."
	}
	color := deref(in.Color)
	if color == "" {
		color = "#7964da"
	} else if !colorRe.MatchString(color) {
		errs["color"] = "Use a hex color such as #7964da."
	}
	owner := actor.UserID
	if in.OwnerID != nil {
		owner = *in.OwnerID
	}
	if ws.Kind == "PERSONAL" {
		if owner != actor.UserID {
			errs["owner_id"] = "Your private projects must be owned by you."
		}
		if in.DepartmentID != nil {
			errs["department_id"] = "Personal projects do not belong to a department."
		}
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	icon := "folder"
	if ws.Kind == "PERSONAL" {
		icon = "book"
	}
	var out []projectJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO projects (workspace_id, department_id, name, description, owner_id, start_on, due_on, color, icon, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			ws.ID, in.DepartmentID, name, desc, owner, start, due, color, icon, actor.UserID).Scan(&id); err != nil {
			return err
		}
		if ws.Kind == "ORG" {
			for _, uid := range uniqueIDs(owner, actor.UserID) {
				if _, err := tx.Exec(ctx, `INSERT INTO project_memberships (project_id, workspace_id, user_id, access_level)
					VALUES ($1, $2, $3, 'MANAGER')`, id, ws.ID, uid); err != nil {
					return err
				}
			}
		}
		if err := audit(ctx, tx, auditEntry{Tenant: ws.TenantID, Workspace: &ws.ID, Actor: actor.UserID, Action: "project.created",
			TargetType: "project", TargetID: &id, Summary: "created project " + name}); err != nil {
			return err
		}
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err = s.renderProjects(ctx, tx, actor, []*projectRow{p})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	w.Header().Set("Location", "/api/v1/projects/"+out[0].ID.String())
	writeJSON(w, r, 201, out[0])
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type milestoneJSON struct {
		ID       uuid.UUID `json:"id"`
		Title    string    `json:"title"`
		TargetOn string    `json:"target_on"`
		State    string    `json:"state"`
	}
	var resp struct {
		projectJSON
		Milestones []milestoneJSON `json:"milestones"`
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err := s.renderProjects(ctx, tx, actor, []*projectRow{p})
		if err != nil {
			return err
		}
		resp.projectJSON = out[0]
		resp.Milestones = []milestoneJSON{}
		rows, err := tx.Query(ctx, `SELECT id, title, target_on, state FROM milestones WHERE project_id = $1 ORDER BY target_on`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m milestoneJSON
			var target time.Time
			if err := rows.Scan(&m.ID, &m.Title, &target, &m.State); err != nil {
				return err
			}
			m.TargetOn = target.Format(time.DateOnly)
			resp.Milestones = append(resp.Milestones, m)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, resp.Version)
	writeJSON(w, r, 200, resp)
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in projectInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	var out []projectJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if p.Access != authz.AccessManage {
			return errForbidden("You can view this project but not change it.")
		}
		if p.Version != version {
			return errVersionConflict()
		}
		errs := map[string]string{}
		name, desc, color, state := p.Name, p.Description, p.Color, p.State
		start, due := p.StartOn, p.DueOn
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
			if name == "" || len(name) > 100 {
				errs["name"] = "Give this project a name (up to 100 characters)."
			}
		}
		if in.Description != nil {
			desc = strings.TrimSpace(*in.Description)
			if len(desc) > 1000 {
				errs["description"] = "Keep the description under 1000 characters."
			}
		}
		if in.Color != nil {
			color = *in.Color
			if !colorRe.MatchString(color) {
				errs["color"] = "Use a hex color such as #7964da."
			}
		}
		if in.State != nil {
			state = *in.State
			if state != "ACTIVE" && state != "ARCHIVED" {
				errs["state"] = "State must be ACTIVE or ARCHIVED."
			}
		}
		if in.StartOn != nil {
			start = parseDate("start_on", *in.StartOn, errs)
		}
		if in.DueOn != nil {
			due = parseDate("due_on", *in.DueOn, errs)
		}
		if len(errs) == 0 && due.Before(start) {
			errs["due_on"] = "Target delivery must be on or after the start date."
		}
		if in.OwnerID != nil || in.DepartmentID != nil {
			errs["owner_id"] = "Ownership and department changes are not supported in this release."
		}
		if len(errs) > 0 {
			return errValidation(errs)
		}
		tag, err := tx.Exec(ctx, `UPDATE projects SET name = $1, description = $2, color = $3, state = $4, start_on = $5, due_on = $6,
			version = version + 1, updated_at = now() WHERE id = $7 AND version = $8`,
			name, desc, color, state, start, due, id, version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errVersionConflict()
		}
		action := "project.updated"
		if state != p.State {
			action = map[string]string{"ARCHIVED": "project.archived", "ACTIVE": "project.restored"}[state]
		}
		if err := audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: action,
			TargetType: "project", TargetID: &id, Summary: action[len("project."):] + " project " + name}); err != nil {
			return err
		}
		p, err = loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err = s.renderProjects(ctx, tx, actor, []*projectRow{p})
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	writeJSON(w, r, 200, out[0])
}

// projectMembers lists people who can be assigned work in the project.
func (s *Server) projectMembers(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type memberJSON struct {
		userJSON
		AccessLevel string `json:"access_level"`
	}
	out := []memberJSON{}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		levels, err := assignableUsers(ctx, tx, p)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(levels))
		for uid := range levels {
			ids = append(ids, uid)
		}
		users, err := loadUsers(ctx, tx, ids)
		if err != nil {
			return err
		}
		for uid, lvl := range levels {
			if u, ok := users[uid]; ok {
				out = append(out, memberJSON{userJSON: u, AccessLevel: lvl})
			}
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sortMembers(out, func(m memberJSON) string { return m.DisplayName })
	writeJSON(w, r, 200, map[string]any{"items": out})
}

// assignableUsers: the project owner plus non-viewer project members (personal: the owner only).
func assignableUsers(ctx context.Context, tx pgx.Tx, p *projectRow) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{p.OwnerID: "MANAGER"}
	if p.Kind == "PERSONAL" {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT pm.user_id, pm.access_level FROM project_memberships pm
		JOIN org_memberships m ON m.org_id = $2 AND m.user_id = pm.user_id AND m.state = 'ACTIVE'
		WHERE pm.project_id = $1 AND pm.access_level <> 'VIEWER'`, p.ID, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid uuid.UUID
		var lvl string
		if err := rows.Scan(&uid, &lvl); err != nil {
			return nil, err
		}
		out[uid] = lvl
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uniqueIDs(ids ...uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
