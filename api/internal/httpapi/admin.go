package httpapi

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

var (
	orgRoles = []authz.Role{authz.TenantOwner, authz.OrgAdmin, authz.Executive, authz.DepartmentHead, authz.ProjectManager,
		authz.TeamLead, authz.Member, authz.Viewer, authz.HRSpecialist}
	slugPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		if r := []rune(w); len(out) < 2 && unicode.IsLetter(r[0]) {
			out = append(out, unicode.ToUpper(r[0]))
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func hasRole(actor *authz.Actor, org uuid.UUID, role authz.Role) bool {
	return slices.Contains(actor.Roles[org], role)
}

func validTimezone(tz string) bool {
	_, err := time.LoadLocation(tz)
	return err == nil && tz != "" && tz != "Local"
}

func (s *Server) createOrganization(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	if actor.TokenScoped {
		s.fail(w, r, errForbidden("API tokens cannot create organizations."))
		return
	}
	var in struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Timezone string `json:"reporting_timezone"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Name, in.Slug = strings.TrimSpace(in.Name), strings.ToLower(strings.TrimSpace(in.Slug))
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	errs := map[string]string{}
	if in.Name == "" || len(in.Name) > 120 {
		errs["name"] = "Name the organization (up to 120 characters)."
	}
	if !slugPattern.MatchString(in.Slug) {
		errs["slug"] = "Use 2–63 lowercase letters, numbers or hyphens."
	}
	if !validTimezone(in.Timezone) {
		errs["reporting_timezone"] = "Use an IANA timezone such as Asia/Kolkata."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	var id uuid.UUID
	err := s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `SELECT tf_create_org($1, $2, $3, $4)`, in.Slug, in.Name, initials(in.Name), in.Timezone).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &id, Actor: actor.UserID, Action: "org.created", TargetType: "organization",
			TargetID: &id, Summary: "created organization " + in.Name})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, map[string]any{"id": id})
}

// adminScope resolves an org the caller belongs to and requires the given permission.
func (s *Server) adminScope(w http.ResponseWriter, r *http.Request, p authz.Permission) (*authz.Actor, uuid.UUID, store.Scope, bool) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err == nil {
		var scope store.Scope
		if scope, err = orgScope(actor, org); err == nil {
			if actor.Can(org, p) && !actor.TokenScoped {
				return actor, org, scope, true
			}
			err = errForbidden("Only organization owners and administrators can manage this organization.")
		}
	}
	s.fail(w, r, err)
	return nil, org, store.Scope{}, false
}

func (s *Server) updateOrganization(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.adminScope(w, r, authz.OrgPolicyManage)
	if !ok {
		return
	}
	var in struct {
		Name     *string `json:"name"`
		Timezone *string `json:"reporting_timezone"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if *in.Name == "" || len(*in.Name) > 120 {
			errs["name"] = "Name the organization (up to 120 characters)."
		}
	}
	if in.Timezone != nil && !validTimezone(*in.Timezone) {
		errs["reporting_timezone"] = "Use an IANA timezone such as Asia/Kolkata."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, err := tx.Exec(ctx, `UPDATE organizations SET name = coalesce($1, name), reporting_timezone = coalesce($2, reporting_timezone)
			WHERE id = $3`, in.Name, in.Timezone, org); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "org.updated", TargetType: "organization",
			TargetID: &org, Summary: "updated organization settings"})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.getOrganization(w, r)
}

func (s *Server) inviteMember(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.adminScope(w, r, authz.OrgMembersInvite)
	if !ok {
		return
	}
	var in struct {
		Email       string     `json:"email"`
		DisplayName string     `json:"display_name"`
		Role        authz.Role `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Email, in.DisplayName = strings.ToLower(strings.TrimSpace(in.Email)), strings.TrimSpace(in.DisplayName)
	errs := map[string]string{}
	if !emailPattern.MatchString(in.Email) || len(in.Email) > 320 {
		errs["email"] = "Enter a valid email address."
	}
	if in.DisplayName == "" || len(in.DisplayName) > 120 {
		errs["display_name"] = "Enter the person's name (up to 120 characters)."
	}
	if !slices.Contains(orgRoles, in.Role) {
		errs["role"] = "Choose a valid role."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	if in.Role == authz.TenantOwner && !hasRole(actor, org, authz.TenantOwner) {
		s.fail(w, r, errForbidden("Only tenant owners can invite another owner."))
		return
	}
	var id uuid.UUID
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `SELECT tf_invite_member($1, $2, $3, $4, $5)`, org, in.Email, in.DisplayName, initials(in.DisplayName), in.Role).
			Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "member.invited", TargetType: "user",
			TargetID: &id, Summary: "invited " + in.Email + " as " + string(in.Role)})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, map[string]any{"user_id": id, "state": "INVITED"})
}

func (s *Server) myInvitations(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	type invitation struct {
		OrgID     uuid.UUID `json:"org_id"`
		Name      string    `json:"name"`
		InvitedAt time.Time `json:"invited_at"`
	}
	out := []invitation{}
	if !actor.TokenScoped {
		err := s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
			rows, err := tx.Query(r.Context(), `SELECT org_id, name, invited_at FROM tf_my_invitations()`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var i invitation
				if err := rows.Scan(&i.OrgID, &i.Name, &i.InvitedAt); err != nil {
					return err
				}
				out = append(out, i)
			}
			return rows.Err()
		})
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err == nil && actor.TokenScoped {
		err = errNotFound()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT tf_accept_invitation($1)`, org).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errNotFound()
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "member.joined", TargetType: "user",
			TargetID: &actor.UserID, Summary: "accepted the invitation"})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"org_id": org, "state": "ACTIVE"})
}

// memberTarget loads the target's membership and enforces the owner-protection rules shared by role and state changes.
func memberTarget(r *http.Request, tx pgx.Tx, actor *authz.Actor, org uuid.UUID) (uuid.UUID, string, bool, error) {
	ctx := r.Context()
	target, err := pathUUID(r, "userId")
	if err != nil {
		return target, "", false, err
	}
	var state string
	var owner bool
	err = tx.QueryRow(ctx, `SELECT m.state, EXISTS (SELECT 1 FROM role_bindings WHERE tenant_id = m.org_id AND user_id = m.user_id
		AND role_key = 'TENANT_OWNER') FROM org_memberships m WHERE m.org_id = $1 AND m.user_id = $2 AND m.state <> 'REMOVED'`, org, target).
		Scan(&state, &owner)
	if store.IsNoRows(err) {
		return target, "", false, errNotFound()
	}
	if err != nil {
		return target, "", false, err
	}
	if owner && !hasRole(actor, org, authz.TenantOwner) {
		return target, "", false, errForbidden("Only tenant owners can change another owner's access.")
	}
	return target, state, owner, nil
}

func otherActiveOwner(r *http.Request, tx pgx.Tx, org, target uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM role_bindings b JOIN org_memberships m ON m.org_id = b.tenant_id AND m.user_id = b.user_id
		WHERE b.tenant_id = $1 AND b.user_id <> $2 AND b.role_key = 'TENANT_OWNER' AND m.state = 'ACTIVE')`, org, target).Scan(&ok)
	return ok, err
}

func (s *Server) setMemberRoles(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.adminScope(w, r, authz.OrgMembersInvite)
	if !ok {
		return
	}
	var in struct {
		Roles []authz.Role `json:"roles"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	slices.Sort(in.Roles)
	in.Roles = slices.Compact(in.Roles)
	valid := len(in.Roles) > 0 && len(in.Roles) <= 4
	for _, role := range in.Roles {
		valid = valid && slices.Contains(orgRoles, role)
	}
	if !valid {
		s.fail(w, r, errValidation(map[string]string{"roles": "Choose between one and four valid roles."}))
		return
	}
	makesOwner := slices.Contains(in.Roles, authz.TenantOwner)
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		target, _, wasOwner, err := memberTarget(r, tx, actor, org)
		if err != nil {
			return err
		}
		if makesOwner && !hasRole(actor, org, authz.TenantOwner) {
			return errForbidden("Only tenant owners can grant the owner role.")
		}
		if wasOwner && !makesOwner {
			if others, err := otherActiveOwner(r, tx, org, target); err != nil || !others {
				if err == nil {
					err = errValidation(map[string]string{"roles": "An organization must keep at least one active owner."})
				}
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM role_bindings WHERE tenant_id = $1 AND user_id = $2 AND scope_type = 'ORG'`, org, target); err != nil {
			return err
		}
		for _, role := range in.Roles {
			if _, err := tx.Exec(ctx, `INSERT INTO role_bindings (tenant_id, user_id, role_key, created_by) VALUES ($1, $2, $3, $4)`,
				org, target, role, actor.UserID); err != nil {
				return err
			}
		}
		names := make([]string, len(in.Roles))
		for i, role := range in.Roles {
			names[i] = string(role)
		}
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "member.roles_changed", TargetType: "user",
			TargetID: &target, Summary: "set roles to " + strings.Join(names, ", ")})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"roles": in.Roles})
}

func (s *Server) setMemberState(w http.ResponseWriter, r *http.Request) {
	actor, org, scope, ok := s.adminScope(w, r, authz.OrgMembersInvite)
	if !ok {
		return
	}
	var in struct {
		State string `json:"state"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	allowed := map[string][]string{"ACTIVE": {"SUSPENDED"}, "SUSPENDED": {"ACTIVE"}, "REMOVED": {"ACTIVE", "SUSPENDED", "INVITED"}}
	if _, ok := allowed[in.State]; !ok {
		s.fail(w, r, errValidation(map[string]string{"state": "Use ACTIVE, SUSPENDED or REMOVED."}))
		return
	}
	err := s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		target, current, owner, err := memberTarget(r, tx, actor, org)
		if err != nil {
			return err
		}
		if target == actor.UserID {
			return errForbidden("You cannot change your own membership.")
		}
		if !slices.Contains(allowed[in.State], current) {
			return errValidation(map[string]string{"state": "A " + strings.ToLower(current) + " membership cannot become " + strings.ToLower(in.State) + "."})
		}
		if owner && in.State != "ACTIVE" {
			if others, err := otherActiveOwner(r, tx, org, target); err != nil || !others {
				if err == nil {
					err = errValidation(map[string]string{"state": "An organization must keep at least one active owner."})
				}
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE org_memberships SET state = $1 WHERE org_id = $2 AND user_id = $3`, in.State, org, target); err != nil {
			return err
		}
		action := map[string]string{"ACTIVE": "member.reactivated", "SUSPENDED": "member.suspended", "REMOVED": "member.removed"}[in.State]
		return audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: action, TargetType: "user",
			TargetID: &target, Summary: strings.TrimPrefix(action, "member.") + " a member"})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"state": in.State})
}
