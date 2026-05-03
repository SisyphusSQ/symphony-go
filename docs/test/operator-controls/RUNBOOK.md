# Operator Controls HTTP Runbook

## Purpose

Validate the single-instance operator control surface shipped by `TOO-133`.

This runbook covers:

- local HTTP liveness/readiness/status endpoints
- Prometheus-style metrics output
- run inspection endpoints
- unsafe control endpoints: pause, resume, drain, cancel, retry, cleanup
- CLI `--port` precedence over workflow `server.port`

## Safety And Side Effects

- The HTTP server binds loopback by default: `127.0.0.1`.
- No Authentication/RBAC is implemented in this slice; the assumption is local operator access only.
- `POST /orchestrator/pause` and `POST /orchestrator/drain` suppress new dispatch in the local process.
- `POST /runs/{id}/cancel` cancels an active in-memory run or removes a queued retry.
- `POST /runs/{id}/retry` only wakes an existing retry row; it does not create tracker truth for an arbitrary issue id.
- `POST /orchestrator/cleanup?terminal=true` runs terminal workspace cleanup using the configured tracker/workspace/hook dependencies.
- Do not expose the listener on a non-loopback interface without adding an external access-control layer.

## Preconditions

- Repository tests pass.
- A workflow file is readable and valid.
- For a real daemon smoke, dispatch dependencies must be configured: Linear credentials, workspace root, hooks, and Codex command.
- For local endpoint shape validation, the deterministic tests in `internal/server` are sufficient and do not require external services.

## Commands

Focused package tests:

```bash
go test ./internal/orchestrator ./internal/server ./cmd/symphony
```

Full regression:

```bash
go test ./...
```

Harness gates:

```bash
make harness-check
make harness-review-gate PLAN=.agent/plans/TOO-133-execution-plan.md
```

Manual startup smoke with an ephemeral local port:

```bash
symphony run --workflow WORKFLOW.md --port 0 --instance dev
```

Expected startup output includes:

```text
operator HTTP server listening on http://127.0.0.1:<port>
```

## Endpoint Smoke

Replace `$BASE` with the printed loopback URL.

```bash
curl -fsS "$BASE/healthz"
curl -fsS "$BASE/readyz"
curl -fsS "$BASE/status"
curl -fsS "$BASE/runs"
curl -fsS "$BASE/metrics"
curl -fsS "$BASE/doctor"
```

Expected:

- `/healthz` returns `200` while the process is alive.
- `/readyz` returns `200` only when lifecycle is `running` and dispatch dependencies are configured; otherwise it returns `503` with JSON error code `not_ready`.
- `/metrics` includes `symphony_runs_active`, `symphony_retry_count`, `symphony_ready`, and `symphony_lifecycle_state`.
- `/runs` returns `running` and `retrying` counts plus current rows.

Control smoke:

```bash
curl -fsS -X POST "$BASE/orchestrator/pause"
curl -fsS -X POST "$BASE/orchestrator/resume"
curl -fsS -X POST "$BASE/orchestrator/drain"
curl -fsS -X POST "$BASE/runs/TOO-123/cancel"
curl -fsS -X POST "$BASE/runs/TOO-123/retry"
```

Expected:

- pause/resume/drain return `202` with a `ControlResult`.
- cancel unknown target returns `404` with JSON error envelope.
- retry active target returns `409` with JSON error envelope.
- unsupported methods on defined routes return `405`.

## Current Verification Result

Executed in the `TOO-133` workspace:

```bash
go test ./internal/orchestrator ./internal/server ./cmd/symphony
go test ./...
```

Result:

- focused package tests: passed
- full Go test suite: passed

External real daemon smoke:

- not executed in this run; it requires live Linear/Codex credentials and a safe dogfood issue.
