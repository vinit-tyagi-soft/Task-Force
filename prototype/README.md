# TaskForce Studio — Enterprise Dashboard UI

**Version 3.1 · Second design edition · Frontend demonstration**

A complete, dependency-free dashboard project based on the TaskForce V3.0 Enterprise Implementation Blueprint. Includes 15 top-level modules, detailed project workspaces, two fictional enterprise tenants, a private workspace, and consistent local task state.

## Start in 10 seconds

1. Extract the ZIP completely.
2. Open **index.html** in a modern browser.
   Alternatively, open **TaskForce_Standalone.html** for the entire UI in one file.
3. Explore the sidebar. Use **Create task**, **New project**, or **⌘/Ctrl K** to get started.

Windows users can also double-click `START-WINDOWS.bat`. On macOS, open `index.html` directly; `START-MAC.command` is an optional convenience launcher.

No installation, API keys, internet connection, account, build step, or paid dependency is needed for the downloaded UI.

### Recommended developer mode

With Node.js 18 or later:

```bash
npm start
```

Open **http://127.0.0.1:3000**. No `npm install` is required. A stable localhost origin makes browser persistence more predictable than `file://` mode. The server binds to the local machine only.

Optional alternative with Python:

```bash
python -m http.server 3000 --bind 127.0.0.1
```

### Build and checks

```bash
npm run check
npm test
npm run build
```

`dist/` contains the same self-contained static application. Deploy that directory to any static host. There is no server runtime required by the UI.

## What to explore first

- **Personal Dashboard:** switch Today / Upcoming / All work / Completed. Cards and charts derive from the underlying task records.
- **Projects & Tasks:** open a project and explore Overview, Tasks, Board, Timeline, Files, and Activity.
- **Task drawer:** change status, add checklist items and comments, upload small supporting files, edit the task, or mark it complete. Project metrics update immediately.
- **Kanban Boards:** filter by project and drag cards between lanes. Keyboard users can open a task and change its Status control.
- **Teams & Workload:** inspect team membership and seven-day estimated effort against configurable weekly capacity.
- **Executive Dashboard:** inspect company-only project health and a deduplicated blocked/overdue intervention queue.
- **Workspace switcher:** switch Acme Corporation / Vertex Studio. Your personal projects remain private in the intended product model.
- **Profile menu:** explore role previews and associated access-denied screens; restore Tenant owner to see everything. Hiring requires HR scope, enabled explicitly for the owner demo.
- **AI Intelligence Center:** ask about blocked work, priorities or portfolio summaries. Answers are deterministic demo templates with clickable source records.
- **Communication Hub:** review a sample thread and approve an email-to-task draft. Reply drafts stay local; nothing is sent.
- **Automations:** run a local simulation and review alert and run history. Duplicate alerts for the same rule/task/reference day are suppressed.
- **Settings → Preferences & data:** try dark mode, compact density, backup/restore, or reset the sample workspace.

## All 15 modules

| # | Module | Included behavior |
|---|---|---|
| 1 | Personal Dashboard | Derived metrics, priority tabs, source-based trends, project pulse, activity |
| 2 | My Work | Personal/company indicators, search, sorting, status/priority filters, team view, bulk completion |
| 3 | My Calendar | Month/week/day, previous/next/today, scoped deadlines, task drill-down |
| 4 | Personal Projects | Private project creation/editing and full project workspaces |
| 5 | Projects & Tasks | Project grid/list, health/search filters, create/edit/archive/restore, six project detail tabs |
| 6 | Kanban Boards | Five lanes, project filter, drag/drop, WIP guidance, task drawers |
| 7 | Teams & Workload | Team creation/editing, member management, member directory, capacity planning |
| 8 | Development & Hiring | Hiring requests, stage updates, candidate feedback, onboarding, learning steps |
| 9 | Executive Dashboard | Company-only KPIs, project health, departments, intervention queue |
| 10 | Reports & Insights | Period/department filters, saved views, definitions, CSV, export history |
| 11 | AI Intelligence Center | Local template answers, cited tasks, acknowledged findings, route/governance previews |
| 12 | Communication Hub | Sample inbox, star/read controls, reviewed task conversion, local reply drafts |
| 13 | Automations & Alerts | Editable rules, toggles, manual simulation, alert inbox, run history |
| 14 | API & Integrations | Catalog, simulated connection state, scoped demo clients, local webhook tests |
| 15 | Settings & Administration | Profile/org editing, invitations, role matrix, SSO preview, AI policy, audit, theme, backup |

## Project structure

```text
TaskForce_Dashboard_UI/
  index.html                  # Direct-open entry point
  TaskForce_Standalone.html    # All CSS and JS embedded for easy sharing
  assets/
    favicon.svg
    css/styles.css            # Responsive design tokens, light/dark themes
    js/data.js                # Fictional demo fixtures
    js/core.js                # State, selectors, metrics, local persistence
    js/components.js          # Reusable HTML/SVG components
    js/pages.js               # All module and project route views
    js/app.js                 # Router, forms, drawers, keyboard and drag/drop
  scripts/
    serve.cjs                 # Zero-dependency local development server
    build.cjs                 # Copies deployable static assets into dist/
    check.cjs                 # JavaScript syntax and asset-reference checks
  tests/domain.test.cjs        # Meaningful scope, metric and state tests
  docs/
    FEATURE_MAP.md
    BACKEND_HANDOFF.md
    DESIGN_SYSTEM.md
    QA_NOTES.md
    TaskForce_V3_Enterprise_Implementation_Blueprint.md
  dist/                       # Ready-to-host static build
  START-WINDOWS.bat
  START-MAC.command
  package.json
```

## Demo data and privacy

- All company names, mail threads, candidates and work records are fictional. Vinit is the demo workspace owner.
- A fixed, editable **demo reference date of 30 October 2026** keeps due-date examples reproducible. See Settings → Preferences & data. Activity events use the actual local clock.
- State is saved under localStorage key `taskforce-studio-v31` in this browser. There is no network synchronization. The older demo's storage key is separate.
- Do not enter real secrets or confidential enterprise/personnel information. The frontend is a product prototype, not a secure multiuser system.
- Full JSON backups contain all local workspaces, including private demo work. Company reports/CSV exports exclude personal tasks; individual My Work exports can include the user's personal tasks.
- File attachments are kept in localStorage as data URLs and limited to 1 MB each. Browser storage quotas apply; use small demo files and JSON backups.
- If browser storage is blocked/full, an in-app notice prompts you to export a backup. File-mode persistence can vary by browser.

## What requires a production backend

SSO/MFA, real authentication and tenant/resource authorization, database persistence, audit integrity, cloud files, OAuth mail access, outgoing email, background schedules, API credentials, webhook delivery, model serving, validated predictions, billing and real-time collaboration are **not implemented**. The UI explicitly labels these previews. See `docs/BACKEND_HANDOFF.md` for a practical integration guide.

Role and workspace previews demonstrate product behavior; they are not security controls. Production code must enforce all access policies server-side on every request.

## Keyboard and accessibility

- **⌘/Ctrl K:** open workspace search.
- **N:** create a task when not typing in a field.
- **Escape:** close the topmost dialog/drawer.
- **Tab / Shift Tab:** focus containment in dialogs.
- Skip-to-content link, labeled forms, SVG text alternatives, focus indicators, reduced-motion support and print styles are included.
- Drag/drop has a status-select alternative. This is not a formal WCAG certification; validate with your enterprise accessibility process before release.

## Handoff and reuse

The project uses ordinary HTML, CSS, JavaScript and inline SVG. There are no external fonts, images, icon packages, analytics scripts, CDN links or runtime dependencies. Your team can adapt the component functions or port the views into React/Next.js while reusing the design tokens and domain definitions.
