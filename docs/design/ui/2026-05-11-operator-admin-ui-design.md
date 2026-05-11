# Operator Admin UI Design

Date: 2026-05-11
Status: Implemented in Storybook and production Web entry

## Goal

Refine the Web operator UI from a compact dashboard into a more polished Ant
Design Vue management-console experience. The UI should have larger readable
typography, a real admin shell, clearer page hierarchy, and Storybook coverage
for the main operator surfaces.

## Scope

- Add a management-console shell with sidebar navigation and top status context.
- Keep useful navigation entries only: `Dashboard` and `Runs`.
- Add Storybook pages for `Dashboard`, `Runs`, and `Run Detail`.
- Update the production page using the same shared components as Storybook.
- Keep the UI read-only.
- Keep data consumption limited to the existing backend API shape:
  `state`, `runs`, `run detail`, and `run events`.
- Use the existing `GET /api/v1/runs/{run_id}/events` endpoint for the run
  timeline.

## Out of Scope

- No Settings page or placeholder Settings function.
- No new backend API.
- No raw jsonl file endpoint in the frontend.
- No fake timeline stream that invents backend truth outside the existing event
  projection contract.
- No production mock-data fallback. Storybook may keep hardcoded fixtures for
  visual review, but the real app must show real API data, API errors, or empty
  states only.
- No backend contract changes and no provider truth defined from Storybook mocks.
- No auth, mutating controls, pause/resume/retry/cancel, or remote dashboard
  behavior.

## Visual Direction

Use a light Ant Design Pro style:

- Sidebar navigation on the left.
- Light gray application background.
- White content panels with subtle borders and restrained shadows.
- Larger page titles around 28-32px.
- Section headings around 18-20px.
- Body, table, and form text around 14-15px.
- Metric numbers around 28-34px.
- Blue primary accents, green success, gold retry/warning, red failure.

The UI should feel like a practical management backend, not a marketing page,
decorative dashboard, or dark ops wallboard.

## Page Structure

### Dashboard

The dashboard is the overview entry point. It shows:

- lifecycle and readiness summary
- running, retrying, failed, token, runtime, and rate-limit metrics
- recent runs
- a selected run summary when a run is selected

### Runs

The runs page is the primary list view. It shows:

- status and issue filters
- a full run table
- clear columns for issue, status, attempt, runtime, tokens, started time, and
  latest signal
- row selection that can lead to run detail

### Run Detail

The run detail view uses only existing run-detail data. It shows:

- run status, issue, attempt, runtime, tokens
- session and workspace metadata
- latest event summary
- failure and retry information when present
- redacted event timeline from `GET /api/v1/runs/{run_id}/events`
- expandable redacted payload JSON per event

The timeline is a backend-projected event view backed by durable `agent_events`.
It is not a direct raw jsonl file viewer.

## Component Plan

- `OperatorAdminShell`: shared admin shell with sidebar, topbar, source badge,
  readiness state, and content slot.
- `AdminMetricCard`: reusable metric card for dashboard and detail summary.
- `AdminDashboardView`: overview page for state metrics and recent runs.
- `AdminRunsView`: full list page using current `RunRow` data.
- `AdminRunDetailView`: detail page using current `RunDetail` data.
- `AdminRunDetailView`: includes the timeline section using current
  `TimelineEventRow` data, with category/severity badges and expandable
  redacted payload.

Storybook and the production page share these components. Storybook freezes the
visual states; production wiring consumes the same component set through
`AdminApp`.

## Data Flow

- Continue using `useDashboardData` for production data loading.
- Continue using `mockOperatorApiClient` and existing fixtures in Storybook.
- Keep view state in the frontend: `dashboard`, `runs`, and `run-detail`.
- Run selection continues through the existing `selectRun(runID)` behavior and
  opens the frontend `run-detail` view.
- Run detail timeline loads through the existing `getRunEvents(runID)` frontend
  API client method, which maps to `/api/v1/runs/{run_id}/events`.

## Error And Empty States

- If the API is unavailable, show the API error instead of substituting mock
  data.
- If runs are empty, show a management-console empty state in the page body.
- If run detail fails to load, show the error inside the detail page.
- If run events are empty, show a timeline empty state rather than a fake event
  stream.
- If run events fail to load, show a timeline-local error state.
- Do not show disabled Settings navigation or unused placeholder functions.

## Verification

Use these checks before handoff:

- `npm run typecheck`
- `npm test`
- `npm run build`
- `npm run build-storybook`
- `antd lint ./src --format json`
- Browser visual check of Storybook `Dashboard`, `Runs`, and `Run Detail`
- Browser visual check of the production Vite page, including run-row selection
  and timeline rendering
