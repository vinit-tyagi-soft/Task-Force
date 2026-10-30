# Verification notes

## Automated checks

- JavaScript syntax checked for all five source files.
- HTML-referenced local assets verified.
- Domain tests cover organization/private scope, context switching, cancelled-task denominators, deduplicated attention, shared-record updates, read-only task mutation guard, date windows, project health/progress, backup structure, and HTML escaping.

Run `npm run check` and `npm test` from the project root.

## Browser smoke verification

All 15 top-level module pages were opened and checked against their expected headings. Project creation, task creation, task comments and status completion were verified in the browser; completion updated the project metric from 0% to 100%.

Additional browser checks verified task editing and nested checklist dialogs; project Overview/Board/Timeline/Files/Activity tabs; persistence after reload; light/dark themes; organization-scoped executive views; viewer role access denial and owner restoration; settings sections; manual rule runs; AI source links; email-to-task review; global search; demo API client creation; archive/restore recovery; the single-file edition; and a real CSV download.

Browser testing is a focused smoke test, not an exhaustive enterprise QA or accessibility certification. Mobile CSS breakpoints were implemented but not separately emulated in the browser session.

## Manual review checklist for your team

1. Create company and private projects; confirm exports respect the selected scope.
2. Move a task across Kanban lanes, edit it through the drawer, and confirm the calendar and reports agree.
3. Test very long names, empty results, zero-task projects, blocked reasons, and invalid date ranges.
4. Try company switching, role previews, HR scope, and viewer task controls.
5. Test small attachments, full localStorage, backup round-trip and reset/Undo.
6. Try responsive layouts in your target desktop and mobile browsers.
7. Before production, replace every locally simulated enterprise control with server-side enforcement and run the full blueprint's security test catalog.

## Known implementation boundaries

Date-based analytics use an editable demo reference date. Historical activity uses the actual local clock. No synchronized calendar events exist. Candidate feedback and skill data are fictional. File storage depends on browser quotas and file-mode persistence varies between browsers. Integration and rule controls are simulations; they make no network requests.
