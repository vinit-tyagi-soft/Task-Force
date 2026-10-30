# TaskForce V3.0

Multi-tenant work management for personal and organization work. TaskForce has a Go API, PostgreSQL with row-level security (RLS) on every table, and a Next.js web app ported from the prototype UI in `prototype/`.

Private (personal) work never leaves its owner. It is excluded from company calendars, reports, CSV exports, workload, alerts, webhooks, API tokens and AI answers. Automated tests enforce this.

## Repository layout

| Path | What it is |
| --- | --- |
| `api/cmd/api` | HTTP API server (Go 1.26, `net/http`, pgx v5) and the background webhook worker |
| `api/cmd/tfadmin` | Admin CLI: `migrate`, `seed`, `reset` |
| `api/migrations` | Ordered SQL migrations (schema, RLS policies, security-definer helpers) |
| `api/internal/authz` | Central role → permission map and project access rules |
| `api/internal/httpapi` | Handlers, auth, audit, webhooks, and integration tests |
| `web/` | Next.js 16 app; proxies `/api/*` to the API so the session cookie stays first-party |
| `prototype/` | Original static UI prototype (reference only) |

## Prerequisites

- Go 1.26+
- Node.js 20+ and npm
- PostgreSQL 17 (macOS: `brew install postgresql@17 && brew services start postgresql@17`)

## Database setup (once)

```bash
createdb taskforce
createdb taskforce_test

# Migrations run as the database owner; they create the non-privileged role taskforce_app.
cd api
export TF_MIGRATE_DATABASE_URL="postgres://$USER@localhost:5432/taskforce?sslmode=disable"
go run ./cmd/tfadmin migrate

# Let the app role log in (roles are cluster-wide, so this also covers taskforce_test).
psql taskforce -c "ALTER ROLE taskforce_app LOGIN PASSWORD 'taskforce_dev'"

go run ./cmd/tfadmin seed     # loads demo data if the database is empty
```

`tfadmin reset` truncates all data and reseeds. Run it after pulling new modules to get their demo records, such as message threads, automation rules, hiring requisitions and goals. `migrate` alone only adds the new, empty tables.

## Running locally

API (from `api/`):

```bash
TF_DATABASE_URL="postgres://taskforce_app:taskforce_dev@localhost:5432/taskforce?sslmode=disable" \
TF_SESSION_SECRET="dev-only-secret-change-me-0123456789abcdef" \
TF_DEV_AUTH=true \
go run ./cmd/api
```

Web (from `web/`):

```bash
npm install
npm run dev        # http://localhost:3000
```

### Environment variables

| Variable | Used by | Purpose |
| --- | --- | --- |
| `TF_DATABASE_URL` | API | Connection as `taskforce_app` (required) |
| `TF_SESSION_SECRET` | API | HMAC key for session cookies, at least 32 characters (required) |
| `TF_ADDR` | API | Listen address, default `127.0.0.1:8080` |
| `TF_DEV_AUTH` | API | `true` enables the passwordless demo login. Never enable it in production. |
| `TF_SECURE_COOKIES` | API | `true` marks cookies `Secure` (use behind HTTPS) |
| `TF_MIGRATE_DATABASE_URL` | tfadmin | Owner connection used for migrations and seeding |
| `TASKFORCE_API_ORIGIN` | web | API origin for the `/api` proxy, default `http://127.0.0.1:8080` |
| `TF_TEST_MIGRATE_DATABASE_URL`, `TF_TEST_DATABASE_URL` | tests | Override the `taskforce_test` connections |

## Demo accounts

With `TF_DEV_AUTH=true`, the sign-in page lists these seeded people. Acme Corporation is the main demo organization; Vertex Studio is a second tenant used to prove isolation.

| Person | Email | Acme role |
| --- | --- | --- |
| Vinit Tyagi | vinit@example.com | Tenant owner (also owns Vertex Studio) |
| Arjun Mehta | arjun.mehta@example.com | Project manager, hiring manager for "Senior frontend engineer" |
| Meera Iyer | meera.iyer@example.com | Executive |
| Ananya Sen | ananya.sen@example.com | HR specialist |
| Priya Sharma, Neha Kapoor, Sara Khan, Rohan Shah, Ishaan Rao | `first.last@example.com` | Members |

## Tests

```bash
cd api && go test ./... -count=1             # integration tests against taskforce_test
cd web && npx tsc --noEmit && npm run lint
```

The integration tests reset and reseed `taskforce_test`. The app-side checks run as `taskforce_app`, so RLS is exercised for real.

## Architecture and security model

- **Tenancy and RLS.** Every request runs in a transaction that sets `app.user_id` (and `app.tenant_id` where relevant). Every table uses `FORCE ROW LEVEL SECURITY`. The app role cannot bypass RLS, and small `SECURITY DEFINER` helpers (`tf_tenant_visible`, `tf_is_member`, …) answer membership questions. Composite foreign keys keep child rows inside their tenant.
- **Authorization.** `internal/authz` maps roles to permissions and project access. Reads the caller cannot see return 404; writes they may not make return 403.
- **Writes.** Updates use optimistic concurrency (`If-Match` / `ETag`, 412 on stale versions). Each change writes its audit event in the same transaction.
- **Webhooks.** Events are queued in an outbox in the same transaction as the audited change. A worker delivers them with HMAC-SHA256 signatures (`X-TaskForce-Signature` over `timestamp.body`) and retries with exponential backoff. URLs must be HTTPS. Private and loopback addresses are blocked at validation and again at dial time, and redirects are not followed.
- **API tokens.** Tokens are `tf_pat_…` bearer tokens, stored only as SHA-256 hashes. Each token is limited to:
  - one organization;
  - read or write scope;
  - an allowlist of task, project and report routes.

  Tokens never reach personal workspaces, conversations, notifications or admin endpoints.

## Modules

| Module | Web route | Who can use it | Highlights |
| --- | --- | --- | --- |
| Dashboards, My Work, Projects, Kanban | `/personal`, `/org`, `/my-work`, `/projects`, `/board` | Everyone (by project access) | Personal and organization workspaces, task drawer, comments, audit |
| My Calendar | `/calendar` | Everyone | Month/week/day views; mine, company and personal scopes; milestones |
| Teams & Workload | `/teams` | Everyone reads teams; `team.manage` edits; `workload.read` sees all workload | Capacity status (available, balanced, over capacity); team leads see their own team |
| Reports & Insights | `/reports` | `report.read` (owner, admin, executive) | Metrics, trend, risk list, saved views (private); CSV export needs `report.export` and is audited and formula-safe |
| Communication Hub | `/messages` | Thread participants only | Inbox, unread and starred filters, compose, optional project link, "Review & create task" |
| Automations & Alerts | `/automations` | `automation.manage` for rules; everyone gets their alerts | Whitelisted triggers and actions, manual "Run rules now", deduplicated alerts that reach only people who can already read the task |
| API & Integrations | `/integrations` | `integration.manage` (owner, admin) | Scoped personal access tokens, signed webhooks, delivery log, test ping |
| Development & Hiring | `/people` | Hiring: HR, admins, owners and each requisition's hiring manager (read-only). Goals: each person | Requisition stages, candidates, upcoming interviews; candidate names are kept out of audit summaries |
| AI Intelligence Center | `/ai` | `ai.query` (owner, executive) | Rules-based answers (blocked, overdue, my priorities, workload, summary) with cited source tasks; question history private to the asker |

## Not yet implemented

- Production identity (OIDC/SAML); the demo uses `TF_DEV_AUTH`.
- OAuth app connectors (Slack, Google, Jira, …). Their catalog cards show "Needs OAuth app setup".
- Scheduled automation runs. Rules currently run on demand.
- Model-based AI. The assistant is deliberately rules-based and does not call any external LLM.
- File attachments and email ingestion.
