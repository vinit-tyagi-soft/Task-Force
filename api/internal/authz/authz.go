// Package authz is the central server-side policy. Roles never bypass tenant
// isolation: every decision starts from an active membership in the resource's tenant.
package authz

import (
	"github.com/google/uuid"
)

type Role string

const (
	TenantOwner    Role = "TENANT_OWNER"
	OrgAdmin       Role = "ORG_ADMIN"
	Executive      Role = "EXECUTIVE"
	DepartmentHead Role = "DEPARTMENT_HEAD"
	ProjectManager Role = "PROJECT_MANAGER"
	TeamLead       Role = "TEAM_LEAD"
	Member         Role = "MEMBER"
	Viewer         Role = "VIEWER"
	HRSpecialist   Role = "HR_SPECIALIST"
)

type Permission string

const (
	OrgMembersInvite Permission = "org.members.invite"
	OrgPolicyManage  Permission = "org.policy.manage"
	ProjectCreate    Permission = "project.create"
	ProjectReadAll   Permission = "project.read_all"
	ProjectManageAll Permission = "project.manage_all"
	ReportRead       Permission = "report.read"
	ReportExport     Permission = "report.export"
	AuditRead        Permission = "audit.read"
	HiringManage     Permission = "hiring.manage"
	AIQuery          Permission = "ai.query"
	AIManagePolicy   Permission = "ai.manage_policy"
	TeamManage       Permission = "team.manage"
	WorkloadRead     Permission = "workload.read"
)

// Org-scope permissions per system role (section 5.2 of the blueprint).
var rolePermissions = map[Role][]Permission{
	TenantOwner:    {OrgMembersInvite, OrgPolicyManage, ProjectCreate, ProjectReadAll, ProjectManageAll, ReportRead, ReportExport, AuditRead, AIQuery, AIManagePolicy, TeamManage, WorkloadRead},
	OrgAdmin:       {OrgMembersInvite, OrgPolicyManage, ProjectCreate, ProjectReadAll, ProjectManageAll, ReportRead, ReportExport, AuditRead, AIManagePolicy, TeamManage, WorkloadRead},
	Executive:      {ProjectReadAll, ReportRead, ReportExport, AIQuery, WorkloadRead},
	DepartmentHead: {ProjectCreate, TeamManage, WorkloadRead},
	ProjectManager: {ProjectCreate, WorkloadRead},
	TeamLead:       {},
	Member:         {},
	Viewer:         {ProjectReadAll, ReportRead, AuditRead},
	HRSpecialist:   {HiringManage},
}

func ValidRole(r string) bool { _, ok := rolePermissions[Role(r)]; return ok }

// Access is the effective level a user has on a project and its tasks.
type Access int

const (
	AccessNone Access = iota
	AccessRead
	AccessContribute
	AccessManage
)

func (a Access) String() string {
	return [...]string{"NONE", "READ", "CONTRIBUTE", "MANAGE"}[a]
}

// Actor is the authenticated user plus their active org-scoped role bindings.
type Actor struct {
	UserID uuid.UUID
	Roles  map[uuid.UUID][]Role // tenant -> org-scope roles (active memberships only)
}

func (a *Actor) IsMember(tenant uuid.UUID) bool {
	_, ok := a.Roles[tenant]
	return ok
}

func (a *Actor) Can(tenant uuid.UUID, p Permission) bool {
	for _, r := range a.Roles[tenant] {
		for _, granted := range rolePermissions[r] {
			if granted == p {
				return true
			}
		}
	}
	return false
}

// ProjectRef is the minimum needed to decide project access.
type ProjectRef struct {
	TenantID       *uuid.UUID // nil for personal workspaces
	PersonalOwner  *uuid.UUID // owner of the personal workspace
	OwnerID        uuid.UUID
	MemberAccess   string // caller's project_memberships.access_level, "" if none
}

func (a *Actor) ProjectAccess(p ProjectRef) Access {
	if p.TenantID == nil {
		if p.PersonalOwner != nil && *p.PersonalOwner == a.UserID {
			return AccessManage
		}
		return AccessNone
	}
	t := *p.TenantID
	if !a.IsMember(t) {
		return AccessNone
	}
	if a.Can(t, ProjectManageAll) || p.OwnerID == a.UserID || p.MemberAccess == "MANAGER" {
		return AccessManage
	}
	if p.MemberAccess == "CONTRIBUTOR" {
		return AccessContribute
	}
	if p.MemberAccess == "VIEWER" || a.Can(t, ProjectReadAll) {
		return AccessRead
	}
	return AccessNone
}

// CanEditTask: managers edit any task; contributors edit tasks they own.
func CanEditTask(access Access, taskOwner, actor uuid.UUID) bool {
	return access == AccessManage || (access == AccessContribute && taskOwner == actor)
}
