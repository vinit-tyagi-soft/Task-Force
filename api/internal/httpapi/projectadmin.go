package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

// manageProject loads a project the caller can see and requires manage access.
func manageProject(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (*projectRow, error) {
	p, err := loadProject(ctx, tx, actor, id)
	if err != nil {
		return nil, err
	}
	if p.Access != authz.AccessManage {
		return nil, errForbidden("Only project owners and managers can change this project.")
	}
	return p, nil
}

type milestoneOut struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	TargetOn string    `json:"target_on"`
	State    string    `json:"state"`
}

func scanMilestone(row pgx.Row) (milestoneOut, error) {
	var m milestoneOut
	var target time.Time
	err := row.Scan(&m.ID, &m.Title, &target, &m.State)
	m.TargetOn = target.Format(time.DateOnly)
	return m, err
}

type milestoneInput struct {
	Title    *string `json:"title"`
	TargetOn *string `json:"target_on"`
	State    *string `json:"state"`
}

func (in *milestoneInput) validate(create bool) error {
	errs := map[string]string{}
	if in.Title != nil {
		*in.Title = strings.TrimSpace(*in.Title)
	}
	if create && in.Title == nil || in.Title != nil && (*in.Title == "" || len(*in.Title) > 100) {
		errs["title"] = "Name the milestone (up to 100 characters)."
	}
	if create && in.TargetOn == nil {
		errs["target_on"] = "Use a YYYY-MM-DD date."
	} else if in.TargetOn != nil {
		parseDate("target_on", *in.TargetOn, errs)
	}
	if in.State != nil && *in.State != "OPEN" && *in.State != "DONE" {
		errs["state"] = "Use OPEN or DONE."
	}
	if len(errs) > 0 {
		return errValidation(errs)
	}
	return nil
}

func (s *Server) createMilestone(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in milestoneInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(true); err != nil {
		s.fail(w, r, err)
		return
	}
	var out milestoneOut
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := manageProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err = scanMilestone(tx.QueryRow(ctx, `INSERT INTO milestones (project_id, workspace_id, title, target_on)
			VALUES ($1, $2, $3, $4::date) RETURNING id, title, target_on, state`, p.ID, p.WorkspaceID, *in.Title, *in.TargetOn))
		if err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: "milestone.created",
			TargetType: "milestone", TargetID: &out.ID, Summary: "added milestone " + out.Title + " to " + p.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

// milestoneProject resolves the milestone's project with manage rights.
func milestoneProject(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (*projectRow, string, error) {
	var pid uuid.UUID
	var title string
	err := tx.QueryRow(ctx, `SELECT project_id, title FROM milestones WHERE id = $1`, id).Scan(&pid, &title)
	if store.IsNoRows(err) {
		return nil, "", errNotFound()
	}
	if err != nil {
		return nil, "", err
	}
	p, err := manageProject(ctx, tx, actor, pid)
	return p, title, err
}

func (s *Server) updateMilestone(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "milestoneId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in milestoneInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(false); err != nil {
		s.fail(w, r, err)
		return
	}
	var out milestoneOut
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, _, err := milestoneProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		out, err = scanMilestone(tx.QueryRow(ctx, `UPDATE milestones SET title = coalesce($1, title), target_on = coalesce($2::date, target_on),
			state = coalesce($3, state) WHERE id = $4 RETURNING id, title, target_on, state`, in.Title, in.TargetOn, in.State, id))
		if err != nil {
			return err
		}
		action, summary := "milestone.updated", "updated milestone "+out.Title
		if in.State != nil && *in.State == "DONE" {
			action, summary = "milestone.completed", "completed milestone "+out.Title
		}
		return audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: action,
			TargetType: "milestone", TargetID: &id, Summary: summary})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, out)
}

func (s *Server) deleteMilestone(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "milestoneId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, title, err := milestoneProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM milestones WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: "milestone.deleted",
			TargetType: "milestone", TargetID: &id, Summary: "removed milestone " + title + " from " + p.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectAccessList returns every explicit project membership, including viewers.
func (s *Server) projectAccessList(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type entry struct {
		User        userJSON `json:"user"`
		AccessLevel string   `json:"access_level"`
	}
	out := []entry{}
	canManage := false
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := loadProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		canManage = p.Access == authz.AccessManage && p.Kind == "ORG"
		rows, err := tx.Query(ctx, `SELECT user_id, access_level FROM project_memberships WHERE project_id = $1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		levels := map[uuid.UUID]string{}
		var ids []uuid.UUID
		for rows.Next() {
			var uid uuid.UUID
			var lvl string
			if err := rows.Scan(&uid, &lvl); err != nil {
				rows.Close()
				return err
			}
			levels[uid] = lvl
			ids = append(ids, uid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		users, err := loadUsers(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, uid := range ids {
			if u, ok := users[uid]; ok {
				out = append(out, entry{User: u, AccessLevel: levels[uid]})
			}
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out, "can_manage": canManage})
}

func (s *Server) setProjectAccess(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	target, err := pathUUID(r, "userId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		AccessLevel string `json:"access_level"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.AccessLevel != "MANAGER" && in.AccessLevel != "CONTRIBUTOR" && in.AccessLevel != "VIEWER" {
		s.fail(w, r, errValidation(map[string]string{"access_level": "Use MANAGER, CONTRIBUTOR or VIEWER."}))
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := manageProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if p.Kind != "ORG" {
			return errValidation(map[string]string{"user_id": "Personal projects are private and cannot be shared."})
		}
		if target == actor.UserID {
			return errForbidden("Ask another project manager to change your own access.")
		}
		if target == p.OwnerID {
			return errValidation(map[string]string{"user_id": "The project owner always has full access."})
		}
		var name string
		err = tx.QueryRow(ctx, `SELECT u.display_name FROM org_memberships m JOIN users u ON u.id = m.user_id
			WHERE m.org_id = $1 AND m.user_id = $2 AND m.state = 'ACTIVE'`, p.TenantID, target).Scan(&name)
		if store.IsNoRows(err) {
			return errValidation(map[string]string{"user_id": "Only active members of this organization can join the project."})
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO project_memberships (project_id, workspace_id, user_id, access_level) VALUES ($1, $2, $3, $4)
			ON CONFLICT (project_id, user_id) DO UPDATE SET access_level = EXCLUDED.access_level`, p.ID, p.WorkspaceID, target, in.AccessLevel); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: "project.access_changed",
			TargetType: "project", TargetID: &p.ID, Summary: "gave " + name + " " + strings.ToLower(in.AccessLevel) + " access to " + p.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"user_id": target, "access_level": in.AccessLevel})
}

func (s *Server) removeProjectAccess(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "projectId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	target, err := pathUUID(r, "userId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		p, err := manageProject(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if target == actor.UserID {
			return errForbidden("Ask another project manager to remove you.")
		}
		tag, err := tx.Exec(ctx, `DELETE FROM project_memberships WHERE project_id = $1 AND user_id = $2`, p.ID, target)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound()
		}
		return audit(ctx, tx, auditEntry{Tenant: p.TenantID, Workspace: &p.WorkspaceID, Actor: actor.UserID, Action: "project.access_removed",
			TargetType: "project", TargetID: &p.ID, Summary: "removed a member from " + p.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
