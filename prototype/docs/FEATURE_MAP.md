# Functional boundary map

| Area | Working locally | Production service still required |
|---|---|---|
| Work | Create/edit/complete/cancel tasks, tags, descriptions, checklist, comments, dependency links, bulk completion | Concurrency, real permissions, shared persistence, notifications |
| Projects | Create/edit/archive, grid/list/filter, overview/tasks/board/timeline/files/activity | Database, membership enforcement, governance |
| Calendar | Month/week/day, period navigation, scope and task drill-down | External calendar sync, timezone-aware meetings |
| Workload | Team roster, assignment effort, capacity editing | Availability calendars, robust scheduling |
| Hiring | Requests, stages, fictional candidates/feedback, onboarding and lessons | Real HR policies and ATS connections |
| Executive | Derived metrics, risk explanations and source drill-down | Authorized snapshots, history, organizational reporting scope |
| Reports | Filters, saved filter views, CSV, export log | Scheduled exports, secure distribution |
| AI | Local rule summaries with task sources, finding acknowledgment | Managed/private model gateway, evaluation, validated forecasts |
| Email | Sample threads, star/read, reviewed task draft, local reply draft | OAuth mailboxes, sync, send and retention controls |
| Rules | Create/edit/toggle, manual simulation, local alert/run history | Scheduler, durable workers, channels and escalation |
| Integrations | Demo connection state, API client metadata, sample webhook configuration/test logs | OAuth, real API tokens, signed network deliveries |
| Identity | Workspace switching, role navigation preview, invitations stored locally | SSO, tenant/resource authorization, MFA, session management |
| Files | Small browser-local attachments, download/remove | Cloud storage, scanning, access enforcement |
| Data | Browser-local persistence, JSON backup/restore, reset and Undo | PostgreSQL, backups, recovery and audit integrity |

No external requests are performed by application scripts. A hosted copy may have its hosting provider's separate access gate; the downloaded static UI has no login dependency.
