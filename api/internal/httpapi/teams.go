package httpapi

import (
	"context"
	"math"
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

const maxTeamMembers = 100

type deptRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type teamJSON struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Department  *deptRef   `json:"department"`
	Lead        *userJSON  `json:"lead"`
	Color       string     `json:"color"`
	Members     []userJSON `json:"members"`
	Version     int        `json:"version"`
	CanManage   bool       `json:"can_manage"`
}

func loadTeams(ctx context.Context, tx pgx.Tx, actor *authz.Actor, org uuid.UUID, only *uuid.UUID) ([]teamJSON, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.name, t.description, t.department_id, coalesce(d.name, ''), t.lead_user_id, t.color, t.version,
		       coalesce(array_agg(m.user_id) FILTER (WHERE m.user_id IS NOT NULL), '{}')
		FROM teams t
		LEFT JOIN departments d ON d.id = t.department_id
		LEFT JOIN team_members m ON m.team_id = t.id
		WHERE t.tenant_id = $1 AND ($2::uuid IS NULL OR t.id = $2)
		GROUP BY t.id, d.name ORDER BY t.name`, org, only)
	if err != nil {
		return nil, err
	}
	type raw struct {
		teamJSON
		deptID  *uuid.UUID
		leadID  *uuid.UUID
		members []uuid.UUID
	}
	var list []raw
	var ids []uuid.UUID
	for rows.Next() {
		var t raw
		var deptName string
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.deptID, &deptName, &t.leadID, &t.Color, &t.Version, &t.members); err != nil {
			rows.Close()
			return nil, err
		}
		if t.deptID != nil {
			t.Department = &deptRef{*t.deptID, deptName}
		}
		ids = append(ids, t.members...)
		if t.leadID != nil {
			ids = append(ids, *t.leadID)
		}
		list = append(list, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	users, err := loadUsers(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	canManage := actor.Can(org, authz.TeamManage)
	out := make([]teamJSON, 0, len(list))
	for _, t := range list {
		t.Members = []userJSON{}
		for _, id := range t.members {
			if u, ok := users[id]; ok {
				t.Members = append(t.Members, u)
			}
		}
		sortMembers(t.Members, func(u userJSON) string { return u.DisplayName })
		if t.leadID != nil {
			if u, ok := users[*t.leadID]; ok {
				t.Lead = &u
			}
		}
		t.CanManage = canManage
		out = append(out, t.teamJSON)
	}
	return out, nil
}

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
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
	var out []teamJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		out, err = loadTeams(r.Context(), tx, actor, org, nil)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out, "can_manage": actor.Can(org, authz.TeamManage)})
}

// teamInput is a full replacement: omitted department or lead clears it.
type teamInput struct {
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	DepartmentID *uuid.UUID  `json:"department_id"`
	LeadUserID   *uuid.UUID  `json:"lead_user_id"`
	Color        string      `json:"color"`
	MemberIDs    []uuid.UUID `json:"member_ids"`
}

// validateTeam normalizes input and checks every reference belongs to the org.
func validateTeam(ctx context.Context, tx pgx.Tx, org uuid.UUID, self *uuid.UUID, in *teamInput) error {
	errs := map[string]string{}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || len(in.Name) > 80 {
		errs["name"] = "Give this team a name (up to 80 characters)."
	}
	if len(in.Description) > 500 {
		errs["description"] = "Keep the description under 500 characters."
	}
	if in.Color == "" {
		in.Color = "#7964da"
	}
	if !colorRe.MatchString(in.Color) {
		errs["color"] = "Use a hex color such as #7964da."
	}
	if in.LeadUserID != nil {
		in.MemberIDs = append(in.MemberIDs, *in.LeadUserID)
	}
	in.MemberIDs = uniqueIDs(in.MemberIDs...)
	if len(in.MemberIDs) > maxTeamMembers {
		errs["member_ids"] = "A team can have at most " + strconv.Itoa(maxTeamMembers) + " members."
	}
	if len(errs) > 0 {
		return errValidation(errs)
	}
	var dup bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM teams WHERE tenant_id = $1 AND lower(name) = lower($2) AND ($3::uuid IS NULL OR id <> $3))`,
		org, in.Name, self).Scan(&dup); err != nil {
		return err
	}
	if dup {
		errs["name"] = "Another team already uses this name."
	}
	if in.DepartmentID != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM departments WHERE id = $1 AND tenant_id = $2)`, in.DepartmentID, org).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			errs["department_id"] = "Choose a department in this organization."
		}
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_memberships WHERE org_id = $1 AND state = 'ACTIVE' AND user_id = ANY($2)`,
		org, in.MemberIDs).Scan(&active); err != nil {
		return err
	}
	if active != len(in.MemberIDs) {
		errs["member_ids"] = "Every member and the lead must be active members of this organization."
	}
	if len(errs) > 0 {
		return errValidation(errs)
	}
	return nil
}

func replaceTeamMembers(ctx context.Context, tx pgx.Tx, org, team uuid.UUID, members []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM team_members WHERE team_id = $1`, team); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO team_members (team_id, tenant_id, user_id) SELECT $1, $2, unnest($3::uuid[])`, team, org, members)
	return err
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
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
	if !actor.Can(org, authz.TeamManage) {
		s.fail(w, r, errForbidden("Only organization administrators and department heads can create teams."))
		return
	}
	var in teamInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	var out []teamJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := validateTeam(ctx, tx, org, nil, &in); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO teams (tenant_id, name, description, department_id, lead_user_id, color)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, org, in.Name, in.Description, in.DepartmentID, in.LeadUserID, in.Color).Scan(&id); err != nil {
			return err
		}
		if err := replaceTeamMembers(ctx, tx, org, id, in.MemberIDs); err != nil {
			return err
		}
		if err := audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "team.created", TargetType: "team", TargetID: &id,
			Summary: "created team " + in.Name, Metadata: map[string]any{"members": len(in.MemberIDs)}}); err != nil {
			return err
		}
		out, err = loadTeams(ctx, tx, actor, org, &id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	writeJSON(w, r, 201, out[0])
}

// teamForWrite resolves a team's tenant (404 when invisible) and enforces manage rights.
func (s *Server) teamForWrite(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID, version int) (uuid.UUID, string, error) {
	var org uuid.UUID
	var name string
	var current int
	if err := tx.QueryRow(ctx, `SELECT tenant_id, name, version FROM teams WHERE id = $1`, id).Scan(&org, &name, &current); err != nil {
		if store.IsNoRows(err) {
			return org, name, errNotFound()
		}
		return org, name, err
	}
	if !actor.IsMember(org) {
		return org, name, errNotFound()
	}
	if !actor.Can(org, authz.TeamManage) {
		return org, name, errForbidden("Only organization administrators and department heads can change teams.")
	}
	if current != version {
		return org, name, errVersionConflict()
	}
	return org, name, nil
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "teamId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in teamInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	var out []teamJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, _, err := s.teamForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		if err := validateTeam(ctx, tx, org, &id, &in); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE teams SET name = $1, description = $2, department_id = $3, lead_user_id = $4, color = $5,
			version = version + 1, updated_at = now() WHERE id = $6 AND version = $7`,
			in.Name, in.Description, in.DepartmentID, in.LeadUserID, in.Color, id, version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errVersionConflict()
		}
		if err := replaceTeamMembers(ctx, tx, org, id, in.MemberIDs); err != nil {
			return err
		}
		if err := audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "team.updated", TargetType: "team", TargetID: &id,
			Summary: "updated team " + in.Name, Metadata: map[string]any{"members": len(in.MemberIDs)}}); err != nil {
			return err
		}
		out, err = loadTeams(ctx, tx, actor, org, &id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out[0].Version)
	writeJSON(w, r, 200, out[0])
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "teamId")
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
		org, name, err := s.teamForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "team.deleted", TargetType: "team", TargetID: &id,
			Summary: "deleted team " + name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// workload compares open org effort due in the window against each person's
// weekly capacity scaled to the window. Private work is never counted.
func (s *Server) workload(w http.ResponseWriter, r *http.Request) {
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
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if days, err = strconv.Atoi(v); err != nil || days < 1 || days > 28 {
			s.fail(w, r, errValidation(map[string]string{"days": "Use a window of 1 to 28 days."}))
			return
		}
	}
	type row struct {
		User        userJSON `json:"user"`
		Teams       []string `json:"teams"`
		Tasks       int      `json:"assigned_tasks"`
		Hours       float64  `json:"planned_hours"`
		Capacity    float64  `json:"capacity_hours"`
		Utilization int      `json:"utilization_percent"`
		Overdue     int      `json:"overdue_tasks"`
		Status      string   `json:"status"`
	}
	rows := []row{}
	visibility := "organization"
	var from, to time.Time
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var tz string
		if err := tx.QueryRow(ctx, `SELECT reporting_timezone FROM organizations WHERE id = $1`, org).Scan(&tz); err != nil {
			return err
		}
		from = domain.Today(s.now(), tz)
		to = from.AddDate(0, 0, days-1)

		var only []uuid.UUID
		if !actor.Can(org, authz.WorkloadRead) {
			visibility = "self"
			if err := tx.QueryRow(ctx, `SELECT array_agg(DISTINCT m.user_id) FROM team_members m JOIN teams t ON t.id = m.team_id
				WHERE t.tenant_id = $1 AND t.lead_user_id = $2`, org, actor.UserID).Scan(&only); err != nil {
				return err
			}
			if len(only) > 0 {
				visibility = "team"
			}
			only = append(only, actor.UserID)
		}

		teamNames := map[uuid.UUID][]string{}
		tr, err := tx.Query(ctx, `SELECT m.user_id, t.name FROM team_members m JOIN teams t ON t.id = m.team_id WHERE t.tenant_id = $1 ORDER BY t.name`, org)
		if err != nil {
			return err
		}
		for tr.Next() {
			var u uuid.UUID
			var n string
			if err := tr.Scan(&u, &n); err != nil {
				tr.Close()
				return err
			}
			teamNames[u] = append(teamNames[u], n)
		}
		tr.Close()

		q, err := tx.Query(ctx, `
			SELECT u.id, u.display_name, u.initials, u.title, u.avatar_color, u.weekly_capacity_hours,
			       count(t.id) FILTER (WHERE t.due_on BETWEEN $2 AND $3),
			       coalesce(sum(t.estimate_minutes) FILTER (WHERE t.due_on BETWEEN $2 AND $3), 0),
			       count(t.id) FILTER (WHERE t.due_on < $2)
			FROM org_memberships m
			JOIN users u ON u.id = m.user_id
			LEFT JOIN (
				SELECT t.id, t.owner_id, t.due_on, t.estimate_minutes FROM tasks t JOIN projects p ON p.id = t.project_id
				WHERE t.tenant_id = $1 AND t.deleted_at IS NULL AND p.deleted_at IS NULL AND p.state = 'ACTIVE'
				  AND t.status NOT IN ('DONE', 'CANCELLED')
			) t ON t.owner_id = u.id
			WHERE m.org_id = $1 AND m.state = 'ACTIVE' AND u.state = 'ACTIVE' AND ($4::uuid[] IS NULL OR u.id = ANY($4))
			GROUP BY u.id ORDER BY u.display_name`, org, from, to, only)
		if err != nil {
			return err
		}
		defer q.Close()
		for q.Next() {
			var rw row
			var weekly, minutes int
			if err := q.Scan(&rw.User.ID, &rw.User.DisplayName, &rw.User.Initials, &rw.User.Title, &rw.User.AvatarColor, &weekly,
				&rw.Tasks, &minutes, &rw.Overdue); err != nil {
				return err
			}
			rw.Teams = teamNames[rw.User.ID]
			if rw.Teams == nil {
				rw.Teams = []string{}
			}
			rw.Hours = math.Round(float64(minutes)/60*10) / 10
			rw.Capacity = math.Round(float64(weekly)*float64(days)/7*10) / 10
			switch {
			case rw.Capacity > 0:
				rw.Utilization = int(math.Round(rw.Hours / rw.Capacity * 100))
			case rw.Hours > 0:
				rw.Utilization = 999
			}
			rw.Status = "AVAILABLE"
			if rw.Utilization > 100 {
				rw.Status = "OVER_CAPACITY"
			} else if rw.Utilization > 80 {
				rw.Status = "BALANCED"
			}
			rows = append(rows, rw)
		}
		return q.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{
		"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "days": days, "visibility": visibility, "items": rows,
		"definition": "Open organization tasks due in the window, estimated effort ÷ weekly capacity scaled to the window. Private work is excluded; this is not a measure of individual value.",
	})
}
