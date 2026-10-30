# TaskForce Studio design system

## Direction

A calm, editorial enterprise workspace. Restrained violet identifies action and intelligence; warm neutrals and fine borders keep dense work readable. A custom vector progress illustration gives the dashboard a distinct opening without external assets.

## Tokens

| Token | Light value | Purpose |
|---|---|---|
| Primary | #7461df | Main actions, selected navigation, active controls |
| Canvas | #f7f8fc | Application background |
| Surface | #ffffff | Cards, dialogs, navigation |
| Text | #242639 | Main content |
| Secondary | #626a80 | Supporting content |
| Line | #eceef5 | Card boundaries and dividers |
| Success | #3a9e7d | Complete / on track |
| Attention | #c49447 | Due / needs attention |
| Risk | #ca7384 | Blocked / urgent |
| Radius | 7–14px | Compact controls through major surfaces |

Dark-theme overrides are defined with `[data-theme=dark]`. Do not hardcode a second theme in page renderers.

## Layout

- Desktop: fixed 246px sidebar, 75px application header, 33px content gutters.
- Wide desktop: larger gutters and 262px navigation.
- Dashboard: primary work column plus contextual right rail.
- Tablet: primary surfaces stack; right rail becomes a grid.
- Mobile: off-canvas navigation, two-column metric cards, stacked forms, scrollable tables and boards.

## Components

`components.js` contains buttons, icon buttons, status chips, priority indicators, task tables, project cards, charts, empty states, tabs, notices, forms, timeline rows and Kanban cards. Inline SVG icons use stroke inheritance and no external icon font.

## Interaction conventions

- Use drawers for quick task context; dialogs for focused creation/review.
- Source links take the user back to the record behind a report or AI demo answer.
- Inline status changes update every view sharing that record.
- Toast Undo restores the immediately preceding local snapshot.
- High-impact production features are described as previews, with no implied backend action.

The typography and focus styles are designed for usability, but this package has not undergone a formal accessibility audit.
