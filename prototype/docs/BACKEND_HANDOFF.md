# Engineering handoff

This package is a working UI simulation, not the production SaaS implementation. Use the original blueprint alongside this guide.

## Recommended evolution

1. Keep `core.js` selectors as metric specifications; replace local mutations with authenticated API calls.
2. Implement organization, membership, project, task, attachment and role/resource policies in the Go API and PostgreSQL.
3. Enforce personal-workspace privacy and tenant boundaries on every query, export, search result, background job and AI retrieval. Client-side filters do not establish permission.
4. Introduce OIDC/enterprise SSO, session lifecycle and revocation. Add SAML/SCIM based on buyer requirements.
5. Move attachments to tenant-scoped object storage with malware scanning, content controls, signed URLs and retention policies.
6. Back reminders, exports and integrations with a durable job queue and audited workers.
7. Connect the hybrid AI gateway after scoped retrieval, logging, evaluation and human-review controls exist. The current local templates are not trained models or predictions.
8. Add concurrency/version checks, pagination, validation, rate limits, monitoring, backups and restore drills.

## Proposed frontend API adapter

| UI intent | Proposed API contract | Notes |
|---|---|---|
| Load workspace | GET /v1/workspaces/:id | Return membership and effective UI capabilities |
| Project directory | GET /v1/projects | Tenant and resource filters enforced server-side |
| Create/edit project | POST /v1/projects; PATCH /v1/projects/:id | Version check on edit; membership validation |
| Task register | GET /v1/tasks | Filters and pagination from server |
| Task lifecycle | POST /v1/tasks; PATCH /v1/tasks/:id | Allowed transitions, audit, completion timestamp |
| Task comments | POST /v1/tasks/:id/comments | Project authorization and safe content handling |
| Upload/download | POST /v1/attachments; GET /v1/attachments/:id | Scan, verify access, issue bounded signed URL |
| Portfolio metrics | GET /v1/reports/portfolio | Authorized snapshot, filter definitions, no private work |
| Email conversion | POST /v1/email-task-drafts; POST /v1/tasks | Reviewed fields and explicit target classification |
| Intelligence | POST /v1/ai/queries | Scoped retrieval, citations, policy route, no autonomous write |
| Rules | POST /v1/automation-rules | Durable evaluation, idempotency, escalation policy |
| Webhooks | POST /v1/webhooks | Signing secrets, replay defense, delivery logs, retries |

These routes are proposals, not existing HTTP endpoints. Align naming with your API standards and the full blueprint.

## State-to-domain mapping

- `orgs` → tenants/workspaces
- `users` + team/project membership → identity/membership tables; split global identity from tenant role bindings
- `projects` / `tasks` → project/task records
- nested `milestones`, `checklist`, `comments`, `files` → normalized child records with resource-scoped authorization
- `rules`, `notifications`, `runs` → rule definitions, event/outbox records, durable jobs
- `activity` → immutable audit/event service; the local demo log is editable and not evidence
- `clients`, `webhooks`, `integrations` → metadata and vault references, never browser-held production secrets
- hiring/candidate data → HR-specific storage and policies, excluded from generic AI retrieval

## Metric contract

- Exclude Cancelled from the completion denominator.
- Overdue means a nonterminal task due before the reference date in this date-only UI. Production must define timestamps and reporting timezone precisely.
- Needs attention is a set union of Blocked and Overdue.
- Project completion is eligible task-count completion, unweighted.
- Health is At risk if a task is blocked, otherwise Needs attention if any task is overdue, otherwise On track.
- Workload is open estimated hours with a due date in the next seven days divided by weekly capacity. It is a planning approximation, not a productivity score.
- Dashboard trends use task deadlines and recorded completion dates. No synthetic trend series or validated forecasts are implied.

## Important UI limitations to resolve in production

Role simulation is intentionally simplified and includes no confidential-project exceptions. All project members and candidate data are fictional. There is no cross-device or multiuser consistency. Local Undo restores the previous complete demo snapshot; replace it with resource-scoped undo/soft-delete backed by version checks in production. Local rule notifications are deduplicated per rule/task/reference day and do not represent delivery guarantees.
