# TaskForce

[![CI](https://github.com/vinit-tyagi-soft/Task-Force/actions/workflows/ci.yml/badge.svg)](https://github.com/vinit-tyagi-soft/Task-Force/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

TaskForce is an open-source, multi-tenant work management platform for both personal and company work. One account holds a private personal workspace plus any number of organizations. Each organization gets projects, Kanban boards, calendars, teams and workload, reports, messages, automations, hiring, an AI assistant and admin controls.

**Version 1.0.0** is the first public release. It covers the enterprise pilot MVP from the TaskForce V3.0 product spec except production single sign-on, backups and observability (see [Roadmap](#roadmap-and-pending-work)).

Privacy is the core design rule: **personal work never leaves its owner.** It is excluded from company calendars, reports, CSV exports, workload, alerts, webhooks, API tokens and AI answers. PostgreSQL row-level security enforces this, and automated tests check it.

## Contents

- [Features](#features)
- [Tech stack](#tech-stack)
- [Quick start](#quick-start)
- [Using TaskForce](#using-taskforce)
- [Configuration](#configuration)
- [Tests](#tests)
- [Architecture and security model](#architecture-and-security-model)
- [Repository layout](#repository-layout)
- [Roadmap and pending work](#roadmap-and-pending-work)
- [Contributing](#contributing)
- [License](#license)

## Features

| Module | Web route | Who can use it | Highlights |
| --- | --- | --- | --- |
| Dashboards, My Work, Projects, Kanban | `/personal`, `/org`, `/my-work`, `/projects`, `/board` | Everyone (by project access) | Personal and organization workspaces, task drawer, comments, audit |
| Project & task detail | `/projects/[id]`, task drawer | Project managers edit milestones and access; task editors manage checklists and links | Milestones, project member access (manager, contributor, viewer; personal projects can't be shared), task checklists, cycle-free dependencies within one workspace, attachments up to 5 MB stored in PostgreSQL and downloadable only by task readers |
| My Calendar | `/calendar` | Everyone | Month/week/day views; mine, company and personal scopes; milestones |
| Teams & Workload | `/teams` | Everyone reads teams; `team.manage` edits; `workload.read` sees all workload | Capacity status (available, balanced, over capacity); team leads see their own team |
| Reports & Insights | `/reports` | `report.read` (owner, admin, executive) | Metrics, trend, risk list, saved views (private); CSV export needs `report.export` and is audited and formula-safe |
| Communication Hub | `/messages` | Thread participants only | Inbox, unread and starred filters, compose, optional project link, "Review & create task" |
| Automations & Alerts | `/automations` | `automation.manage` for rules; everyone gets their alerts | Whitelisted triggers and actions, manual "Run rules now", deduplicated alerts that reach only people who can already read the task |
| API & Integrations | `/integrations` | `integration.manage` (owner, admin) | Scoped personal access tokens, signed webhooks, delivery log, test ping |
| Development & Hiring | `/people` | Hiring: HR, admins, owners and each requisition's hiring manager (read-only). Goals: each person | Requisition stages, candidates, upcoming interviews; candidate names are kept out of audit summaries |
| AI Intelligence Center | `/ai` | `ai.query` (owner, executive) | Rules-based answers (blocked, overdue, my priorities, workload, summary) with cited source tasks; question history private to the asker. Owners and admins set the AI policy: on/off, allowed question types, history retention (7–365 days) |
| Settings & Administration | `/settings` | Owners and admins | Create organizations, invite members (accepted in-app), change roles, suspend or remove members; owners are protected and every organization keeps at least one owner |

The AI assistant is deliberately rules-based. It reads only data the asker can already see and never calls an external model.

## Tech stack

- **API:** Go 1.26 (`net/http`, pgx v5), single binary with a background webhook worker.
- **Database:** PostgreSQL 17 with `FORCE ROW LEVEL SECURITY` on every table.
- **Web:** Next.js 16, React 19 and TypeScript. It proxies `/api/*` to the API so the session cookie stays first-party.
- **Admin CLI:** `tfadmin` for migrations, seeding and resets.

## Quick start

### 1. Install prerequisites

- [Go 1.26+](https://go.dev/dl/)
- [Node.js 20+](https://nodejs.org/) and npm
- PostgreSQL 17
  - macOS: `brew install postgresql@17 && brew services start postgresql@17`
  - Linux: install `postgresql-17` from your distribution or the [PostgreSQL apt/yum repos](https://www.postgresql.org/download/)
  - Docker: `docker run -d --name taskforce-pg -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:17` (then use `postgres:postgres@` as the owner in the URLs below)

### 2. Get the code

```bash
git clone https://github.com/vinit-tyagi-soft/Task-Force.git
cd Task-Force
```

### 3. Create and migrate the database

```bash
createdb taskforce
createdb taskforce_test   # only needed to run the tests

# Migrations run as the database owner; they create the non-privileged role taskforce_app.
cd api
export TF_MIGRATE_DATABASE_URL="postgres://$USER@localhost:5432/taskforce?sslmode=disable"
go run ./cmd/tfadmin migrate

# Let the app role log in (roles are cluster-wide, so this also covers taskforce_test).
psql taskforce -c "ALTER ROLE taskforce_app LOGIN PASSWORD 'taskforce_dev'"

go run ./cmd/tfadmin seed     # loads demo data if the database is empty
```

`tfadmin reset` truncates all data and reseeds. Run it after pulling new modules to get their demo records. `migrate` alone only adds the new, empty tables.

### 4. Start the API (from `api/`)

```bash
TF_DATABASE_URL="postgres://taskforce_app:taskforce_dev@localhost:5432/taskforce?sslmode=disable" \
TF_SESSION_SECRET="dev-only-secret-change-me-0123456789abcdef" \
TF_DEV_AUTH=true \
go run ./cmd/api
```

### 5. Start the web app (from `web/`)

```bash
npm install
npm run dev        # http://localhost:3000
```

For a production build, run `npm run build && npm start`.

## Using TaskForce

### Sign in

With `TF_DEV_AUTH=true`, the sign-in page lists the seeded demo people. Click a person to sign in as them. Acme Corporation is the main demo organization. Vertex Studio is a second tenant used to prove isolation.

| Person | Email | Acme role |
| --- | --- | --- |
| Vinit Tyagi | vinit@example.com | Tenant owner (also owns Vertex Studio) |
| Arjun Mehta | arjun.mehta@example.com | Project manager, hiring manager for "Senior frontend engineer" |
| Meera Iyer | meera.iyer@example.com | Executive |
| Ananya Sen | ananya.sen@example.com | HR specialist |
| Priya Sharma, Neha Kapoor, Sara Khan, Rohan Shah, Ishaan Rao | `first.last@example.com` | Members |

Sign in as different people to see how roles change what is visible. For example, Meera sees reports and AI, a member doesn't, and nobody sees another person's personal workspace.

Passwordless demo sign-in is for local use only. Production sign-in (OIDC) is on the roadmap.

### Everyday workflow

1. **Pick a workspace.** Your personal workspace is always in the sidebar. The organization switcher changes which company you're working in.
2. **Plan work.** Create a project in **Projects**, add milestones and give teammates manager, contributor or viewer access.
3. **Track tasks.** Add tasks and move them across the **Board**. Open a task to edit it, comment, tick off checklist items, link dependencies and attach files.
4. **Stay on schedule.** **My Work** and **My Calendar** show what is due. **Automations** raise alerts for overdue or blocked work.
5. **Collaborate.** Use the **Communication Hub** for threads and turn a message into a task after review.
6. **See the big picture.** Leaders use **Teams & Workload**, **Reports** (with CSV export) and the **AI Intelligence Center**.

### Set up your own organization

1. Open **Settings** and create an organization (slug, name and reporting timezone).
2. Invite people by email and pick their role. If they don't have an account yet, inviting them creates one. They accept the invitation in their own **Settings** page; invitation emails are not sent yet.
3. Change roles, suspend or remove members. Suspension applies on the person's next request.
4. Set the **AI policy**: turn the assistant on or off, choose allowed question types, and set history retention.
5. Under **API & Integrations**, create scoped API tokens or HTTPS webhooks to connect other tools.

### API

The web app talks to the JSON API at `/api/v1`. For scripts, create a personal access token in **API & Integrations** and send it as `Authorization: Bearer tf_pat_…`. Tokens are limited to one organization, read or write scope, and task, project and report routes. Routes are registered in `api/internal/httpapi/server.go`. An OpenAPI document is on the roadmap.

## Configuration

| Variable | Used by | Purpose |
| --- | --- | --- |
| `TF_DATABASE_URL` | API | Connection as `taskforce_app` (required) |
| `TF_SESSION_SECRET` | API | HMAC key for session cookies, at least 32 characters (required) |
| `TF_ADDR` | API | Listen address, default `127.0.0.1:8080` |
| `TF_DEV_AUTH` | API | `true` enables the passwordless demo login. Never enable it in production. The API refuses to start with it unless `TF_ADDR` is a loopback address and `TF_SECURE_COOKIES` is off. |
| `TF_SECURE_COOKIES` | API | `true` marks cookies `Secure` (use behind HTTPS) |
| `TF_MIGRATE_DATABASE_URL` | tfadmin | Owner connection used for migrations and seeding |
| `TASKFORCE_API_ORIGIN` | web | API origin for the `/api` proxy, default `http://127.0.0.1:8080` |
| `TF_TEST_MIGRATE_DATABASE_URL`, `TF_TEST_DATABASE_URL` | tests | Override the `taskforce_test` connections |

## Tests

```bash
cd api && go test ./... -count=1             # integration tests against taskforce_test
cd web && npx tsc --noEmit && npm run lint
```

The integration tests reset and reseed `taskforce_test`. The app-side checks run as `taskforce_app`, so RLS is exercised for real. GitHub Actions runs the same checks on every push and pull request.

## Architecture and security model

- **Tenancy and RLS.** Every request runs in a transaction that sets `app.user_id` (and `app.tenant_id` where relevant). Every table uses `FORCE ROW LEVEL SECURITY`. The app role cannot bypass RLS, and small `SECURITY DEFINER` helpers (`tf_tenant_visible`, `tf_is_member`, …) answer membership questions. Composite foreign keys keep child rows inside their tenant.
- **Authorization.** `internal/authz` maps roles to permissions and project access. Reads the caller cannot see return 404; writes they may not make return 403. The caller's roles are reloaded on every request, so role changes and suspensions apply immediately.
- **Writes.** Updates use optimistic concurrency (`If-Match` / `ETag`, 412 on stale versions). Each change writes its audit event in the same transaction.
- **Sessions.** Signed `HttpOnly` cookies. Non-GET cookie requests must send `X-TaskForce-Client: web` (CSRF protection). Request bodies are limited to 1 MB, or about 5 MB for attachment uploads.
- **Attachments.** Stored in PostgreSQL with a SHA-256 checksum and a sanitized filename. They are served as downloads with a sandboxing Content-Security-Policy.
- **Webhooks.** Events are queued in an outbox in the same transaction as the audited change. A worker delivers them with HMAC-SHA256 signatures (`X-TaskForce-Signature` over `timestamp.body`) and retries with exponential backoff. URLs must be HTTPS. Private and loopback addresses are blocked at validation and again at dial time, and redirects are not followed.
- **API tokens.** Tokens are `tf_pat_…` bearer tokens, stored only as SHA-256 hashes. They never reach personal workspaces, conversations, notifications or admin endpoints.

## Repository layout

| Path | What it is |
| --- | --- |
| `api/cmd/api` | HTTP API server and the background webhook worker |
| `api/cmd/tfadmin` | Admin CLI: `migrate`, `seed`, `reset` |
| `api/migrations` | Ordered SQL migrations (schema, RLS policies, security-definer helpers) |
| `api/internal/authz` | Central role → permission map and project access rules |
| `api/internal/httpapi` | Handlers, auth, audit, webhooks, and integration tests |
| `web/` | Next.js app (pages in `web/app`, shared UI in `web/components`) |
| `prototype/` | Original static UI prototype (reference only) |

## Roadmap and pending work

Priorities follow the TaskForce V3.0 product spec. Each pending item has a GitHub issue labeled `P0`, `P1` or `P2`; see [open issues](https://github.com/vinit-tyagi-soft/Task-Force/issues).

### P0: enterprise pilot MVP

Done in 1.0.0:

- Tenant and personal workspace isolation
- Admin-managed memberships and scoped roles
- Projects, tasks and boards, plus milestones, comments and attachments
- Departments and teams
- Calendars for due dates
- Executive metrics and CSV exports
- Notifications and rules-based alerts
- Audit trails and tenant settings
- Read-only AI summaries behind policy controls

Still pending:

- [ ] **Production sign-in (OIDC SSO).** Replace the demo login with OpenID Connect, including account linking and session revocation.
- [ ] **Backups and observability.** Structured logs, metrics, health and readiness endpoints, plus a documented backup and restore procedure.
- [ ] **Production deployment.** Dockerfiles or Compose, HTTPS guidance and a hardened configuration checklist.
- [ ] **Invitation and notification emails.** Invitations are currently accepted in-app only.

### P1: extended enterprise

- [ ] SAML and SCIM provisioning
- [ ] Workload estimates (hours or points) feeding team capacity
- [ ] Advanced templates, custom fields and approvals
- [ ] Interview and onboarding workflows with HR-specific permissions
- [ ] Connected Gmail and Microsoft 365 mail (read-only) with reviewed email-to-task conversion
- [ ] Documented developer API (OpenAPI spec)
- [ ] Configurable workflow rules with scheduled runs (rules currently run on demand)
- [ ] Scheduled report delivery

### P2: expansion

- [ ] Predictive delivery models backed by validated history
- [ ] AI agents that propose actions for human approval
- [ ] Bidirectional mail and calendar sync, and full email compose
- [ ] App marketplace and OAuth connectors (Slack, Google, Jira, …), which show "Needs OAuth app setup" today
- [ ] Industry packs, dedicated tenant infrastructure and private AI serving
- [ ] Extended workforce and skills features

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md), then pick an [open issue](https://github.com/vinit-tyagi-soft/Task-Force/issues). P0 issues are the most valuable right now. Bugs and ideas go through the issue templates.

## License

[MIT](LICENSE) © 2026 Vinit Tyagi
