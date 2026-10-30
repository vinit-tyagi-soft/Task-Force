// Package seed loads the fictional demo organizations from the V0.2 prototype.
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type user struct{ key, name, initials, title, dept, color, email string }

var users = []user{
	{"u1", "Vinit Tyagi", "VT", "Chief of Staff", "Leadership", "#7862d5", "vinit@example.com"},
	{"u2", "Arjun Mehta", "AM", "Engineering Lead", "Engineering", "#e2a064", "arjun.mehta@example.com"},
	{"u3", "Priya Sharma", "PS", "Product Designer", "Design", "#d380a7", "priya.sharma@example.com"},
	{"u4", "Neha Kapoor", "NK", "Platform Engineer", "Engineering", "#7f9ac3", "neha.kapoor@example.com"},
	{"u5", "Sara Khan", "SK", "Security Lead", "Security", "#7faa97", "sara.khan@example.com"},
	{"u6", "Rohan Shah", "RS", "Marketing Manager", "Marketing", "#b68cbe", "rohan.shah@example.com"},
	{"u7", "Ishaan Rao", "IR", "Frontend Engineer", "Engineering", "#689bab", "ishaan.rao@example.com"},
	{"u8", "Ananya Sen", "AS", "People Partner", "People", "#c7a26d", "ananya.sen@example.com"},
	{"u9", "Meera Iyer", "MI", "VP Operations", "Leadership", "#8b7fc7", "meera.iyer@example.com"},
}

type org struct {
	key, slug, name, short, color string
	members                       map[string]string // user key -> org role
	departments                   []string
}

var orgs = []org{
	{"org1", "acme", "Acme Corporation", "AC", "#7660dc",
		map[string]string{"u1": "TENANT_OWNER", "u2": "PROJECT_MANAGER", "u3": "MEMBER", "u4": "MEMBER", "u5": "MEMBER",
			"u6": "MEMBER", "u7": "MEMBER", "u8": "HR_SPECIALIST", "u9": "EXECUTIVE"},
		[]string{"Product", "Engineering", "Security", "Design", "Marketing", "People"}},
	{"org2", "vertex", "Vertex Studio", "VS", "#4e95aa",
		map[string]string{"u1": "TENANT_OWNER", "u3": "MEMBER", "u6": "MEMBER", "u7": "MEMBER"},
		[]string{"Product", "Marketing"}},
}

type project struct {
	key, org, name, desc, dept, owner, due, color, icon string
	members                                            []string
}

var projects = []project{
	{"p1", "org1", "Enterprise workspace launch", "Design a better way for enterprise teams to work together.", "Product", "u2", "2026-11-20", "#7964da", "rocket", []string{"u1", "u2", "u3", "u4", "u7"}},
	{"p2", "org1", "Zero Trust rollout", "Build a safer enterprise with identity-first access.", "Security", "u5", "2026-11-12", "#5c98a4", "shield", []string{"u1", "u2", "u5", "u4"}},
	{"p3", "org1", "Customer experience refresh", "Make every customer interaction feel considered.", "Design", "u3", "2026-11-06", "#d09969", "pen", []string{"u1", "u3", "u7"}},
	{"p4", "org1", "Q4 go-to-market strategy", "Bring a clear enterprise story to our next chapter.", "Marketing", "u6", "2026-11-25", "#a487bd", "megaphone", []string{"u1", "u6", "u3"}},
	{"p5", "org1", "Cloud cost optimization", "Create sustainable infrastructure without slowing delivery.", "Engineering", "u4", "2026-11-10", "#769f8e", "cloud", []string{"u1", "u4", "u2"}},
	{"p6", "personal", "Leadership learning journal", "Space to learn, reflect, and become a better leader.", "", "u1", "2026-12-01", "#8d7ab9", "book", nil},
	{"p7", "personal", "Half marathon preparation", "Small, consistent steps toward the next finish line.", "", "u1", "2026-12-15", "#69a49b", "activity", nil},
	{"p8", "org2", "Partner onboarding portal", "A welcoming first experience for every partner.", "Product", "u1", "2026-11-18", "#6d92c3", "globe", []string{"u1", "u3", "u7"}},
	{"p9", "org2", "Brand & content sprint", "Tell a more useful story to our audience.", "Marketing", "u6", "2026-11-24", "#c387a3", "megaphone", []string{"u1", "u6"}},
}

type task struct {
	project, title, owner, status, priority, due string
	hours                                        int
}

var tasks = []task{
	{"p1", "Review enterprise authentication flow", "u1", "IN_PROGRESS", "HIGH", "2026-10-30", 6},
	{"p2", "Finalize Q4 security assessment", "u1", "IN_REVIEW", "HIGH", "2026-10-30", 4},
	{"p3", "Approve onboarding wireframes", "u1", "TODO", "MEDIUM", "2026-10-30", 3},
	{"p4", "Prepare leadership update", "u1", "TODO", "MEDIUM", "2026-11-02", 2},
	{"p2", "Resolve identity provider dependency", "u2", "BLOCKED", "URGENT", "2026-10-29", 8},
	{"p1", "Complete API integration tests", "u2", "IN_PROGRESS", "HIGH", "2026-11-03", 12},
	{"p3", "Publish design system guidelines", "u3", "DONE", "LOW", "2026-10-28", 5},
	{"p5", "Review infrastructure usage report", "u4", "IN_REVIEW", "MEDIUM", "2026-11-04", 6},
	{"p4", "Draft enterprise launch messaging", "u6", "IN_PROGRESS", "MEDIUM", "2026-11-05", 8},
	{"p2", "Validate access control test cases", "u5", "TODO", "HIGH", "2026-11-06", 10},
	{"p6", "Read a chapter on leadership", "u1", "TODO", "LOW", "2026-10-31", 1},
	{"p7", "Complete weekend long run", "u1", "TODO", "MEDIUM", "2026-11-01", 2},
	{"p1", "Document release readiness checklist", "u4", "DONE", "MEDIUM", "2026-10-27", 4},
	{"p5", "Archive unused cloud resources", "u2", "BLOCKED", "HIGH", "2026-11-02", 7},
	{"p1", "Implement notification preferences", "u7", "IN_PROGRESS", "MEDIUM", "2026-11-04", 10},
	{"p1", "Approve architecture decisions", "u1", "DONE", "HIGH", "2026-10-26", 3},
	{"p3", "Audit mobile navigation patterns", "u3", "IN_REVIEW", "MEDIUM", "2026-10-31", 6},
	{"p3", "Build accessible form components", "u7", "IN_PROGRESS", "HIGH", "2026-11-02", 12},
	{"p2", "Map privileged service accounts", "u5", "DONE", "HIGH", "2026-10-23", 8},
	{"p4", "Review enterprise pitch deck", "u1", "IN_REVIEW", "MEDIUM", "2026-11-03", 3},
	{"p4", "Prepare launch email copy", "u6", "TODO", "LOW", "2026-11-09", 5},
	{"p5", "Set workload resource budgets", "u4", "IN_PROGRESS", "MEDIUM", "2026-11-05", 8},
	{"p1", "Validate tenant isolation scenarios", "u5", "TODO", "HIGH", "2026-11-08", 12},
	{"p2", "Approve guest access policy", "u1", "TODO", "HIGH", "2026-11-04", 2},
	{"p3", "Interview five pilot users", "u3", "DONE", "MEDIUM", "2026-10-24", 5},
	{"p1", "Ship project detail navigation", "u7", "DONE", "MEDIUM", "2026-10-29", 6},
	{"p5", "Publish cloud spend baseline", "u4", "DONE", "MEDIUM", "2026-10-25", 4},
	{"p6", "Complete weekly reflection", "u1", "DONE", "LOW", "2026-10-25", 1},
	{"p7", "Schedule strength sessions", "u1", "IN_PROGRESS", "LOW", "2026-11-03", 1},
	{"p6", "Practice stakeholder storytelling", "u1", "IN_PROGRESS", "MEDIUM", "2026-11-06", 2},
	{"p8", "Review partner portal prototype", "u1", "IN_REVIEW", "HIGH", "2026-10-30", 4},
	{"p8", "Build partner invitation screen", "u7", "IN_PROGRESS", "MEDIUM", "2026-11-05", 8},
	{"p8", "Publish welcome content", "u3", "DONE", "LOW", "2026-10-28", 3},
	{"p9", "Approve brand content calendar", "u1", "TODO", "MEDIUM", "2026-11-03", 2},
	{"p9", "Write partner launch announcement", "u6", "IN_PROGRESS", "HIGH", "2026-11-06", 5},
	{"p1", "Review old billing prototype", "u2", "CANCELLED", "LOW", "2026-10-22", 2},
	{"p4", "Align sales enablement owners", "u6", "DONE", "MEDIUM", "2026-10-30", 4},
	{"p2", "Configure SSO staging tenant", "u4", "IN_PROGRESS", "HIGH", "2026-11-01", 10},
	{"p1", "Confirm API release handover", "u1", "TODO", "HIGH", "2026-11-09", 3},
	{"p3", "Design empty and error states", "u3", "TODO", "MEDIUM", "2026-11-04", 8},
}

const description = "Coordinate with the project team, validate the outcome, and share a concise handover. Keep the accountable owner informed of any delivery dependencies."

// Load inserts demo data. Without reset it does nothing if users already exist.
func Load(ctx context.Context, pool *pgxpool.Pool, reset bool) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if reset {
		if _, err := tx.Exec(ctx, `TRUNCATE users, organizations, idempotency_keys, audit_events CASCADE`); err != nil {
			return false, err
		}
	} else {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return false, nil
		}
	}
	if err := load(ctx, tx); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func load(ctx context.Context, tx pgx.Tx) error {
	userIDs := map[string]uuid.UUID{}
	personalWS := map[string]uuid.UUID{}
	for _, u := range users {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO users (email, display_name, initials, title, avatar_color, timezone)
			VALUES ($1, $2, $3, $4, $5, 'Asia/Kolkata') RETURNING id`, u.email, u.name, u.initials, u.title, u.color).Scan(&id); err != nil {
			return fmt.Errorf("user %s: %w", u.key, err)
		}
		userIDs[u.key] = id
		var ws uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces (kind, owner_user_id, name) VALUES ('PERSONAL', $1, 'Personal workspace') RETURNING id`, id).Scan(&ws); err != nil {
			return err
		}
		personalWS[u.key] = ws
	}

	orgWS := map[string]uuid.UUID{}
	orgIDs := map[string]uuid.UUID{}
	depts := map[string]uuid.UUID{} // org/dept -> id
	for _, o := range orgs {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO organizations (slug, name, short_name, region, reporting_timezone, accent_color)
			VALUES ($1, $2, $3, 'India', 'Asia/Kolkata', $4) RETURNING id`, o.slug, o.name, o.short, o.color).Scan(&id); err != nil {
			return err
		}
		orgIDs[o.key] = id
		var ws uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces (kind, tenant_id, name) VALUES ('ORG', $1, $2) RETURNING id`, id, o.name).Scan(&ws); err != nil {
			return err
		}
		orgWS[o.key] = ws
		for uk, role := range o.members {
			if _, err := tx.Exec(ctx, `INSERT INTO org_memberships (org_id, user_id, state, joined_at) VALUES ($1, $2, 'ACTIVE', now())`, id, userIDs[uk]); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO role_bindings (tenant_id, user_id, role_key) VALUES ($1, $2, $3)`, id, userIDs[uk], role); err != nil {
				return err
			}
		}
		for _, d := range o.departments {
			var did uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO departments (tenant_id, name) VALUES ($1, $2) RETURNING id`, id, d).Scan(&did); err != nil {
				return err
			}
			depts[o.key+"/"+d] = did
		}
	}

	projectIDs := map[string]uuid.UUID{}
	projectWS := map[string]uuid.UUID{}
	for _, p := range projects {
		ws := personalWS[p.owner]
		var dept *uuid.UUID
		if p.org != "personal" {
			ws = orgWS[p.org]
			d := depts[p.org+"/"+p.dept]
			dept = &d
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO projects (workspace_id, department_id, name, description, owner_id, start_on, due_on, color, icon, created_by)
			VALUES ($1, $2, $3, $4, $5, '2026-10-15', $6, $7, $8, $5) RETURNING id`,
			ws, dept, p.name, p.desc, userIDs[p.owner], p.due, p.color, p.icon).Scan(&id); err != nil {
			return fmt.Errorf("project %s: %w", p.key, err)
		}
		projectIDs[p.key], projectWS[p.key] = id, ws
		for _, m := range p.members {
			level := "CONTRIBUTOR"
			if m == p.owner {
				level = "MANAGER"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO project_memberships (project_id, workspace_id, user_id, access_level) VALUES ($1, $2, $3, $4)`,
				id, ws, userIDs[m], level); err != nil {
				return err
			}
		}
		for _, m := range []struct{ title, on, state string }{
			{"Discovery & alignment", "2026-10-23", "DONE"},
			{"Implementation review", "2026-11-05", "OPEN"},
			{"Delivery & handover", p.due, "OPEN"},
		} {
			if _, err := tx.Exec(ctx, `INSERT INTO milestones (project_id, workspace_id, title, target_on, state) VALUES ($1, $2, $3, $4, $5)`,
				id, ws, m.title, m.on, m.state); err != nil {
				return err
			}
		}
	}

	ist, _ := time.LoadLocation("Asia/Kolkata")
	for i, t := range tasks {
		var completed *time.Time
		if t.status == "DONE" {
			d, _ := time.ParseInLocation(time.DateOnly, t.due, ist)
			c := d.Add(17 * time.Hour)
			completed = &c
		}
		blocked := ""
		if t.status == "BLOCKED" {
			blocked = "Pending approval of reserved resource commitments."
			if t.project == "p2" {
				blocked = "Awaiting identity provider configuration from the infrastructure owner."
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO tasks (workspace_id, project_id, title, description, status, priority, owner_id, creator_id,
				start_on, due_on, estimate_minutes, blocked_reason, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7, '2026-10-20', $8, $9, $10, $11) RETURNING id`,
			projectWS[t.project], projectIDs[t.project], t.title, description, t.status, t.priority, userIDs[t.owner],
			t.due, t.hours*60, blocked, completed).Scan(&id); err != nil {
			return fmt.Errorf("task %q: %w", t.title, err)
		}
		if i < 8 && projects[indexOf(t.project)].org != "personal" {
			author := "u3"
			if i%2 == 1 {
				author = "u2"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO task_comments (task_id, workspace_id, author_id, body, created_at)
				VALUES ($1, $2, $3, 'The latest review notes are ready. Let''s align on the next steps.', '2026-10-29T09:30:00Z')`,
				id, projectWS[t.project], userIDs[author]); err != nil {
				return err
			}
		}
	}
	return nil
}

func indexOf(key string) int {
	for i, p := range projects {
		if p.key == key {
			return i
		}
	}
	return -1
}
