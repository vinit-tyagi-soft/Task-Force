package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

var (
	requisitionStages = []string{"PENDING_APPROVAL", "APPROVED", "SCREENING", "INTERVIEWING", "OFFER", "CLOSED"}
	candidateStages   = []string{"APPLIED", "SCREEN", "INTERVIEW", "OFFER", "HIRED", "REJECTED"}
)

type candidateJSON struct {
	ID              uuid.UUID  `json:"id"`
	RequisitionID   uuid.UUID  `json:"requisition_id"`
	FullName        string     `json:"full_name"`
	Email           string     `json:"email"`
	Stage           string     `json:"stage"`
	NextInterviewAt *time.Time `json:"next_interview_at"`
	Notes           string     `json:"notes"`
	Version         int        `json:"version"`
}

type requisitionJSON struct {
	ID             uuid.UUID       `json:"id"`
	Title          string          `json:"title"`
	DepartmentID   *uuid.UUID      `json:"department_id"`
	DepartmentName string          `json:"department_name"`
	HiringManager  userJSON        `json:"hiring_manager"`
	Openings       int             `json:"openings"`
	Stage          string          `json:"stage"`
	Version        int             `json:"version"`
	Candidates     []candidateJSON `json:"candidates"`
}

const candidateCols = `id, requisition_id, full_name, email, stage, next_interview_at, notes, version`

func scanCandidate(row pgx.Row) (candidateJSON, error) {
	var c candidateJSON
	err := row.Scan(&c.ID, &c.RequisitionID, &c.FullName, &c.Email, &c.Stage, &c.NextInterviewAt, &c.Notes, &c.Version)
	return c, err
}

// listHiring returns requisitions (with candidates) the caller may see: everything for
// hiring admins, only their own requisitions for hiring managers. RLS enforces the same.
func (s *Server) listHiring(w http.ResponseWriter, r *http.Request) {
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
	items := []requisitionJSON{}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, `
			SELECT q.id, q.title, q.department_id, coalesce(d.name, ''), u.id, u.display_name, u.initials, u.title, u.avatar_color,
			       q.openings, q.stage, q.version
			FROM job_requisitions q
			JOIN users u ON u.id = q.hiring_manager_id
			LEFT JOIN departments d ON d.id = q.department_id
			WHERE q.tenant_id = $1 ORDER BY q.stage = 'CLOSED', q.created_at`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		index := map[uuid.UUID]int{}
		for rows.Next() {
			var q requisitionJSON
			m := &q.HiringManager
			if err := rows.Scan(&q.ID, &q.Title, &q.DepartmentID, &q.DepartmentName, &m.ID, &m.DisplayName, &m.Initials, &m.Title,
				&m.AvatarColor, &q.Openings, &q.Stage, &q.Version); err != nil {
				return err
			}
			q.Candidates = []candidateJSON{}
			index[q.ID] = len(items)
			items = append(items, q)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT `+candidateCols+` FROM candidates WHERE tenant_id = $1
			ORDER BY next_interview_at NULLS LAST, created_at`, org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCandidate(rows)
			if err != nil {
				return err
			}
			if i, ok := index[c.RequisitionID]; ok {
				items[i].Candidates = append(items[i].Candidates, c)
			}
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": items, "can_manage": actor.Can(org, authz.HiringManage),
		"stages": requisitionStages, "candidate_stages": candidateStages})
}

func hiringForbidden() error {
	return errForbidden("Only HR specialists and organization administrators can change hiring records.")
}

type requisitionInput struct {
	Title           string     `json:"title"`
	DepartmentID    *uuid.UUID `json:"department_id"`
	HiringManagerID uuid.UUID  `json:"hiring_manager_id"`
	Openings        int        `json:"openings"`
}

func (s *Server) createRequisition(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	scope, err := orgScope(actor, org)
	if err == nil && !actor.Can(org, authz.HiringManage) {
		err = hiringForbidden()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in requisitionInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 120 {
		errs["title"] = "Name the role (up to 120 characters)."
	}
	if in.Openings == 0 {
		in.Openings = 1
	}
	if in.Openings < 1 || in.Openings > 50 {
		errs["openings"] = "Openings must be between 1 and 50."
	}
	if in.HiringManagerID == uuid.Nil {
		errs["hiring_manager_id"] = "Choose a hiring manager."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	var id uuid.UUID
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_memberships WHERE org_id = $1 AND user_id = $2 AND state = 'ACTIVE')`,
			org, in.HiringManagerID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return errValidation(map[string]string{"hiring_manager_id": "The hiring manager must be an active member."})
		}
		if in.DepartmentID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM departments WHERE id = $1 AND tenant_id = $2)`,
				*in.DepartmentID, org).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errValidation(map[string]string{"department_id": "Choose a department in this organization."})
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO job_requisitions (tenant_id, title, department_id, hiring_manager_id, openings, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, org, in.Title, in.DepartmentID, in.HiringManagerID, in.Openings, actor.UserID).
			Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "requisition.created", TargetType: "job_requisition",
			TargetID: &id, Summary: "opened a hiring request for " + in.Title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, 1)
	writeJSON(w, r, 201, map[string]any{"id": id, "version": 1})
}

type stagePatch struct {
	Stage    *string `json:"stage"`
	Openings *int    `json:"openings"`
}

func (s *Server) updateRequisition(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "reqId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in stagePatch
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if (in.Stage != nil && !slices.Contains(requisitionStages, *in.Stage)) || (in.Openings != nil && (*in.Openings < 1 || *in.Openings > 50)) {
		s.fail(w, r, errValidation(map[string]string{"stage": "Choose a valid stage; openings must be 1–50."}))
		return
	}
	var next int
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, title, err := requisitionForWrite(ctx, tx, actor, id, version)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE job_requisitions SET stage = coalesce($1, stage), openings = coalesce($2, openings),
			version = version + 1, updated_at = now() WHERE id = $3 AND version = $4 RETURNING version`,
			in.Stage, in.Openings, id, version).Scan(&next); err != nil {
			if store.IsNoRows(err) {
				return errVersionConflict()
			}
			return err
		}
		summary := "updated the hiring request for " + title
		if in.Stage != nil {
			summary = "moved the hiring request for " + title + " to " + *in.Stage
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "requisition.updated", TargetType: "job_requisition",
			TargetID: &id, Summary: summary})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, next)
	writeJSON(w, r, 200, map[string]any{"id": id, "version": next})
}

// requisitionForWrite loads a visible requisition and enforces manage rights and, when version > 0, the version.
func requisitionForWrite(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID, version int) (uuid.UUID, string, error) {
	var org uuid.UUID
	var title string
	var current int
	err := tx.QueryRow(ctx, `SELECT tenant_id, title, version FROM job_requisitions WHERE id = $1`, id).Scan(&org, &title, &current)
	switch {
	case store.IsNoRows(err):
		return org, title, errNotFound()
	case err != nil:
		return org, title, err
	case !actor.IsMember(org):
		return org, title, errNotFound()
	case !actor.Can(org, authz.HiringManage):
		return org, title, hiringForbidden()
	case version > 0 && current != version:
		return org, title, errVersionConflict()
	}
	return org, title, nil
}

type candidateInput struct {
	FullName        *string `json:"full_name"`
	Email           *string `json:"email"`
	Stage           *string `json:"stage"`
	NextInterviewAt *string `json:"next_interview_at"`
	Notes           *string `json:"notes"`
}

// validate normalizes the input; interview is nil when unchanged and points at a nil time to clear it.
func (in *candidateInput) validate(create bool) (**time.Time, error) {
	errs := map[string]string{}
	if in.FullName != nil {
		*in.FullName = strings.TrimSpace(*in.FullName)
	}
	if create && (in.FullName == nil || *in.FullName == "") || in.FullName != nil && (*in.FullName == "" || len(*in.FullName) > 120) {
		errs["full_name"] = "Enter the candidate's name (up to 120 characters)."
	}
	if in.Email != nil {
		*in.Email = strings.TrimSpace(*in.Email)
		if len(*in.Email) > 320 || *in.Email != "" && !strings.Contains(*in.Email, "@") {
			errs["email"] = "Enter a valid email address or leave it blank."
		}
	}
	if in.Stage != nil && !slices.Contains(candidateStages, *in.Stage) {
		errs["stage"] = "Choose " + strings.Join(candidateStages, ", ") + "."
	}
	if in.Notes != nil && len(*in.Notes) > 2000 {
		errs["notes"] = "Notes can be up to 2000 characters."
	}
	var interview **time.Time
	if in.NextInterviewAt != nil {
		var t *time.Time
		if *in.NextInterviewAt != "" {
			parsed, err := time.Parse(time.RFC3339, *in.NextInterviewAt)
			if err != nil {
				errs["next_interview_at"] = "Use an RFC 3339 date-time."
			}
			t = &parsed
		}
		interview = &t
	}
	if len(errs) > 0 {
		return nil, errValidation(errs)
	}
	return interview, nil
}

func (s *Server) createCandidate(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	reqID, err := pathUUID(r, "reqId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in candidateInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	interview, err := in.validate(true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var at *time.Time
	if interview != nil {
		at = *interview
	}
	var out candidateJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, title, err := requisitionForWrite(ctx, tx, actor, reqID, 0)
		if err != nil {
			return err
		}
		stage := "APPLIED"
		if in.Stage != nil {
			stage = *in.Stage
		}
		out, err = scanCandidate(tx.QueryRow(ctx, `INSERT INTO candidates (tenant_id, requisition_id, full_name, email, stage, next_interview_at, notes)
			VALUES ($1, $2, $3, coalesce($4, ''), $5, $6, coalesce($7, '')) RETURNING `+candidateCols,
			org, reqID, *in.FullName, in.Email, stage, at, in.Notes))
		if err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "candidate.created", TargetType: "candidate",
			TargetID: &out.ID, Summary: "added a candidate for " + title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out.Version)
	writeJSON(w, r, 201, out)
}

func (s *Server) updateCandidate(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "candidateId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	version, err := ifMatchVersion(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in candidateInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	interview, err := in.validate(false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var out candidateJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		var reqID uuid.UUID
		var current int
		err := tx.QueryRow(ctx, `SELECT requisition_id, version FROM candidates WHERE id = $1`, id).Scan(&reqID, &current)
		if store.IsNoRows(err) {
			return errNotFound()
		}
		if err != nil {
			return err
		}
		org, title, err := requisitionForWrite(ctx, tx, actor, reqID, 0)
		if err != nil {
			return err
		}
		if current != version {
			return errVersionConflict()
		}
		var at *time.Time
		if interview != nil {
			at = *interview
		}
		out, err = scanCandidate(tx.QueryRow(ctx, `UPDATE candidates SET full_name = coalesce($1, full_name), email = coalesce($2, email),
			stage = coalesce($3, stage), next_interview_at = CASE WHEN $4 THEN $5 ELSE next_interview_at END, notes = coalesce($6, notes),
			version = version + 1, updated_at = now() WHERE id = $7 AND version = $8 RETURNING `+candidateCols,
			in.FullName, in.Email, in.Stage, interview != nil, at, in.Notes, id, version))
		if store.IsNoRows(err) {
			return errVersionConflict()
		}
		if err != nil {
			return err
		}
		summary := "updated a candidate for " + title
		if in.Stage != nil {
			summary = "moved a candidate for " + title + " to " + *in.Stage
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "candidate.updated", TargetType: "candidate",
			TargetID: &id, Summary: summary})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setETag(w, out.Version)
	writeJSON(w, r, 200, out)
}

type goalJSON struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	TargetOn *string   `json:"target_on"`
	Progress int       `json:"progress"`
}

const goalCols = `id, title, to_char(target_on, 'YYYY-MM-DD'), progress`

func scanGoal(row pgx.Row) (goalJSON, error) {
	var g goalJSON
	err := row.Scan(&g.ID, &g.Title, &g.TargetOn, &g.Progress)
	return g, err
}

// Development goals are private to their owner; nobody else (including HR) can read them.
func (s *Server) listGoals(w http.ResponseWriter, r *http.Request) {
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
	out := []goalJSON{}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT `+goalCols+` FROM development_goals WHERE tenant_id = $1 AND user_id = $2
			ORDER BY progress = 100, target_on NULLS LAST, created_at`, org, actor.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGoal(rows)
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

type goalInput struct {
	Title    *string `json:"title"`
	TargetOn *string `json:"target_on"`
	Progress *int    `json:"progress"`
}

func (in *goalInput) validate(create bool) error {
	errs := map[string]string{}
	if in.Title != nil {
		*in.Title = strings.TrimSpace(*in.Title)
	}
	if create && in.Title == nil || in.Title != nil && (*in.Title == "" || len(*in.Title) > 160) {
		errs["title"] = "Describe the goal (up to 160 characters)."
	}
	if in.TargetOn != nil && *in.TargetOn != "" {
		parseDate("target_on", *in.TargetOn, errs)
	}
	if in.Progress != nil && (*in.Progress < 0 || *in.Progress > 100) {
		errs["progress"] = "Progress must be between 0 and 100."
	}
	if len(errs) > 0 {
		return errValidation(errs)
	}
	return nil
}

func (s *Server) createGoal(w http.ResponseWriter, r *http.Request) {
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
	var in goalInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(true); err != nil {
		s.fail(w, r, err)
		return
	}
	var out goalJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		var err error
		out, err = scanGoal(tx.QueryRow(r.Context(), `INSERT INTO development_goals (tenant_id, user_id, title, target_on, progress)
			VALUES ($1, $2, $3, nullif($4::text, '')::date, coalesce($5::int, 0)) RETURNING `+goalCols, org, actor.UserID, *in.Title, in.TargetOn, in.Progress))
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

func (s *Server) updateGoal(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "goalId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in goalInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := in.validate(false); err != nil {
		s.fail(w, r, err)
		return
	}
	var out goalJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		var err error
		out, err = scanGoal(tx.QueryRow(r.Context(), `UPDATE development_goals SET title = coalesce($1, title),
			target_on = CASE WHEN $2::text IS NULL THEN target_on ELSE nullif($2, '')::date END, progress = coalesce($3, progress),
			updated_at = now() WHERE id = $4 AND user_id = $5 RETURNING `+goalCols, in.Title, in.TargetOn, in.Progress, id, actor.UserID))
		if store.IsNoRows(err) {
			return errNotFound()
		}
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, out)
}

func (s *Server) deleteGoal(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "goalId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM development_goals WHERE id = $1 AND user_id = $2`, id, actor.UserID)
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
