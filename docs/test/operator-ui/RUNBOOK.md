# Operator UI Static Serving Runbook

## Purpose

Validate that the Go operator server can serve the production Web GUI build
from the same loopback HTTP surface as the `/api/v1` operator API.

This runbook covers:

- Vite production build output in `web/dist`
- Go static asset serving at `/`
- `/api/v1/*` API route precedence
- frontend history fallback for browser dashboard routes
- missing asset 404 behavior

## Safety And Side Effects

- The operator HTTP server binds loopback by default: `127.0.0.1`.
- `npm run build` writes `web/dist`, which is ignored and should not be
  committed.
- The smoke uses local HTTP requests only.
- Do not expose the listener on a non-loopback interface without an external
  access-control layer.

## Preconditions

- Node dependencies are installed under `web/node_modules`.
- A readable workflow file exists for `symphony run`.
- For manual endpoint smoke, dispatch dependencies must be configured so the
  `symphony run` process remains alive while curl requests are sent.
- Without live dispatch dependencies, use
  `go test ./internal/server -run TestOperatorServerDashboardSmoke -count=1`
  for the local HTTP server smoke.

## Commands

Frontend build:

```bash
cd web
npm run build
cd ..
```

Focused Go tests:

```bash
go test ./internal/server ./cmd/symphony
```

Full regression:

```bash
go test ./...
```

Harness gates:

```bash
make harness-check
make harness-review-gate PLAN=.agents/plans/TOO-144.md
```

Manual daemon smoke with configured dispatch dependencies:

```bash
symphony run --workflow WORKFLOW.md --port 4002 --instance dev
```

Expected startup output includes:

```text
operator HTTP server listening on http://127.0.0.1:4002
```

## Endpoint Smoke

Replace `$BASE` with the printed loopback URL.

```bash
curl -fsS "$BASE/" | head
curl -fsS "$BASE/api/v1/state"
curl -fsSI "$BASE/assets/<built-asset-name>"
curl -fsS -H 'Accept: text/html' "$BASE/runs/<run_id>" | head
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE/assets/missing.js"
```

Expected:

- `/` returns the Vite `index.html`.
- `/api/v1/state` returns operator API JSON, not dashboard HTML.
- An existing `/assets/*` file returns `200` with a JavaScript or CSS content
  type.
- A browser refresh on `/runs/<run_id>` returns the dashboard HTML when the
  request accepts `text/html`.
- A missing `/assets/*` file returns `404`.

## Current Verification Result

Executed in the `TOO-144` workspace:

```bash
go test ./internal/server ./cmd/symphony
cd web && npm run build
go test ./...
go test ./internal/server -run TestOperatorServerDashboardSmoke -count=1
make harness-check
make harness-review-gate PLAN=.agents/plans/TOO-144.md
```

Result:

- focused Go tests: passed
- frontend production build: passed
- full Go test suite: passed
- local operator server smoke: passed
- harness check: passed
- harness review gate: passed with `blocking_findings=none`
