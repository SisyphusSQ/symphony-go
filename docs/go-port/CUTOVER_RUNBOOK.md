# Go Cutover Runbook

## Purpose

This runbook defines the production cutover gate for replacing the external
Elixir bootstrap runner with the Go Symphony implementation.

It is a decision runbook, not a replacement claim. The Go implementation may
replace the external bootstrap runner only when every required gate in this
document is green for the target environment.

## Current Decision

| Field | Value |
| --- | --- |
| Decision | `NO-GO` until the required real dogfood and production cutover smoke gates pass with explicit enablement. |
| Fallback | Keep the external Elixir bootstrap runner as the active production runner. |
| Scope | One Go instance, one Linear project, one execution repo, one workspace root, one SQLite state database. |
| Out of scope | Fleet manager, single-instance multi-repo operation, enterprise RBAC/approval queue/container hardening, additional tracker adapters. |

## Operating Shape

The only supported v1 cutover shape is:

```text
one Symphony Go instance
  -> one WORKFLOW.md
  -> one Linear project
  -> one execution repository
  -> one workspace root
  -> one state database
```

If one Linear project tracks multiple repositories, run one Symphony instance per
execution repository and use repo label routing. Do not cut over to a single
instance that mutates multiple repositories.

## Required Inputs

| Input | Requirement |
| --- | --- |
| Workflow | A committed `WORKFLOW.md` using safe production defaults. |
| Linear | A scoped `LINEAR_API_KEY` for the target Linear project. |
| Repo | A `SOURCE_REPO_URL` that points at the execution repository. |
| Workspace | A dedicated writable workspace root used only by the Go instance. |
| State DB | A dedicated SQLite path supplied through `state_store.path` / `SYMPHONY_STATE_DB`. |
| Codex | Local Codex auth and command availability for the target OS user. |
| External runner | Current external Elixir bootstrap runner can be paused, stopped, and restarted by the operator. |

Do not write real token values, local auth files, raw external responses, full
host paths, or temporary workspace details into committed docs.

## Pre-Cutover Gate Matrix

| Gate | Required | Command / Evidence | Pass Criteria | Current result |
| --- | --- | --- | --- | --- |
| Full Go regression | yes | `go test ./...` | All packages pass. | PASS in `TOO-136`. |
| Harness consistency | yes | `make harness-check` | Harness gate passes. | PASS in `TOO-136`. |
| Fake E2E | yes | `make test-fake-e2e` | Deterministic fake Linear + fake Codex profile passes. | PASS in `TOO-136`. |
| Real dogfood default safety | yes | `make test-real-integration` | Default profile succeeds with an explicit skip when not enabled. | PASS with explicit SKIP in `TOO-136`. |
| Real dogfood explicit run | yes | `SYMPHONY_REAL_INTEGRATION=1 ... make test-real-integration` | Real Linear/Codex preflight passes against an isolated active dogfood issue. | Blocked until explicit credentials, repo URL, workspace root, dogfood issue, and local Codex auth are supplied for the target environment. |
| Durable state | yes | `go test ./internal/state ./internal/orchestrator -run 'TestSQLiteStore|TestRuntimeRecoverState|TestRuntimeStartupRecovery' -count=1` | SQLite migration, retry/session persistence, restart recovery, and interrupted-run retry recovery pass. | PASS in `TOO-136`. |
| Operator controls | yes | `go test ./internal/orchestrator ./internal/server ./cmd/symphony -run 'TestRuntimePauseDrainAndResumeControlDispatch|TestRuntimeCancelRunStopsActiveAttemptWithoutRetry|TestRuntimeRetryRunWakesQueuedRetryAndRejectsRunningRun|TestHandlerServesStatusRunsReadinessAndMetrics|TestHandlerControlEndpointsAndErrorSemantics|TestRunPerformsStartupValidationWithoutStartingRuntime' -count=1` | pause/resume/drain/cancel/retry/status/ready/metrics and CLI startup behavior pass. | PASS in `TOO-136`. |
| Production safety | yes | `go test ./internal/safety ./internal/config ./internal/observability ./internal/agent ./internal/agent/codex ./internal/orchestrator ./internal/state -count=1` | safe sandbox defaults, secret redaction, audit events, and resource guardrails pass. | PASS in `TOO-136`. |
| Typed Linear write APIs | yes | `go test ./internal/tracker/linear ./internal/tools/lineargraphql -run 'TestCreateAndUpdateIssueComment|TestUpsertIssueWorkpad|TestTransitionIssueState|TestLinkIssueURL|TestWriteAPIErrors|TestExecute|TestAvailableOnlyForLinearTrackerWithAuth' -count=1` | comment/workpad/state/link APIs and raw GraphQL tool behavior pass. | PASS in `TOO-136`. |
| Production baseline CLI smoke | yes | Commands in the next section. | Validate/run/help surfaces exit successfully and do not require browser/manual workflows. | PASS in `TOO-136`. |

If any required gate is failed or blocked, the cutover decision is `NO-GO`.

## Production Baseline Smoke Commands

These commands are safe local smoke checks for the committed runtime surface:

```bash
go run ./cmd/symphony --help
go run ./cmd/symphony run --help
go run ./cmd/symphony validate WORKFLOW.md
go run ./cmd/symphony run --workflow WORKFLOW.md --instance cutover-smoke
go run ./cmd/symphony run --workflow WORKFLOW.md --port 0 --instance cutover-smoke
```

Expected results:

- help commands print command usage and exit successfully.
- `validate WORKFLOW.md` reports startup validation success.
- `run --workflow WORKFLOW.md --instance cutover-smoke` reports workflow startup
  validation. Because the committed workflow currently sets `server.port: 0`,
  it starts a loopback operator HTTP server from workflow config; if dispatch
  dependencies are not injected in the CLI slice, it exits successfully after
  reporting that dispatch is not configured.
- `run --workflow WORKFLOW.md --port 0 --instance cutover-smoke` starts a
  loopback operator HTTP server on an ephemeral port, reports the URL, and then
  exits successfully if dispatch dependencies are not configured.

Real daemon cutover must additionally run the explicit real dogfood profile and
operator endpoint checks against the target environment.

## Current Validation Result

Executed for `TOO-136`:

```text
go test ./...
make harness-check
make test-fake-e2e
make test-real-integration
go test ./internal/state ./internal/orchestrator -run 'TestSQLiteStore|TestRuntimeRecoverState|TestRuntimeStartupRecovery' -count=1
go test ./internal/orchestrator ./internal/server ./cmd/symphony -run 'TestRuntimePauseDrainAndResumeControlDispatch|TestRuntimeCancelRunStopsActiveAttemptWithoutRetry|TestRuntimeRetryRunWakesQueuedRetryAndRejectsRunningRun|TestHandlerServesStatusRunsReadinessAndMetrics|TestHandlerControlEndpointsAndErrorSemantics|TestRunPerformsStartupValidationWithoutStartingRuntime' -count=1
go test ./internal/safety ./internal/config ./internal/observability ./internal/agent ./internal/agent/codex ./internal/orchestrator ./internal/state -count=1
go test ./internal/tracker/linear ./internal/tools/lineargraphql -run 'TestCreateAndUpdateIssueComment|TestUpsertIssueWorkpad|TestTransitionIssueState|TestLinkIssueURL|TestWriteAPIErrors|TestExecute|TestAvailableOnlyForLinearTrackerWithAuth' -count=1
go run ./cmd/symphony --help
go run ./cmd/symphony run --help
go run ./cmd/symphony validate WORKFLOW.md
go run ./cmd/symphony run --workflow WORKFLOW.md --instance cutover-smoke
go run ./cmd/symphony run --workflow WORKFLOW.md --port 0 --instance cutover-smoke
```

Result:

- local deterministic gates: passed
- real dogfood default profile: passed with explicit skip because
  `SYMPHONY_REAL_INTEGRATION` was not set
- explicit real dogfood: blocked for the target environment
- cutover decision after this run: `NO-GO`

## Go / No-Go Criteria

### GO

Proceed only when all are true:

1. Every required pre-cutover gate is `pass`.
2. The explicit real dogfood profile has run with `SYMPHONY_REAL_INTEGRATION=1`
   against an isolated active issue and passed.
3. The target `WORKFLOW.md` uses one Linear project and one execution repo.
4. `agent.max_concurrent_agents` is set to `1` for the first production cutover.
5. `Merging` is not enabled as an active dispatch state for first cutover.
6. `state_store.path` points at a dedicated SQLite database.
7. The external bootstrap runner can be paused/stopped before Go starts.
8. Rollback owner and rollback command are known before the switch.

### NO-GO

Do not proceed when any are true:

1. Any required gate fails or is blocked.
2. Full real dogfood is only default-skipped.
3. Go and external bootstrap runner would both dispatch from the same Linear
   project at the same time.
4. The target workflow would operate on multiple repos from one instance.
5. Required credentials are broad personal tokens instead of scoped runtime
   credentials.
6. Operator cannot confirm health/readiness/metrics after startup.
7. Rollback command or external bootstrap runner recovery is unknown.

## Cutover Procedure

1. Announce the cutover window and freeze new production dispatch changes.
2. Back up or copy the current Go state database if it already exists.
3. Record current external bootstrap runner process/service identity.
4. Run the full pre-cutover gate matrix in this document.
5. Confirm the decision is `GO`.
6. Drain or pause the external bootstrap runner.
7. Confirm no active external-runner agent session is mutating the target repo.
8. Start the Go instance with the target `WORKFLOW.md`, workspace root, and state
   database.
9. Confirm `/healthz`, `/readyz`, `/metrics`, `/status`, and `/runs` on the
   loopback operator surface.
10. Allow one low-risk active issue to dispatch.
11. Confirm the Go runner writes the expected Linear workpad/state updates.
12. Continue with `max_concurrent_agents=1` until the monitoring window is clean.

## Post-Cutover Monitoring

Monitor for at least one full polling/dispatch/retry cycle:

| Signal | Healthy expectation | Breach action |
| --- | --- | --- |
| `/healthz` | HTTP 200 while process is alive. | Inspect process logs; rollback if process cannot stay up. |
| `/readyz` | HTTP 200 after dispatch dependencies are configured. | Keep external runner paused; fix config or rollback. |
| `/metrics` | `symphony_ready`, active run count, retry count, and lifecycle state are present. | Treat missing metrics as observability breach. |
| `/status` / `/runs` | Running and retry rows match Linear issue activity. | Pause Go runner and inspect state DB before retrying. |
| Linear workpad | Exactly one active `## Symphony Workpad` comment per issue. | Pause Go runner; use typed write API diagnostics. |
| Workspace root | One workspace per issue; no cross-repo mutation. | Pause Go runner; restore external runner if isolation is broken. |
| State DB | Running/retry/session rows persist and recover after controlled restart. | Roll back if restart recovery fails. |
| Logs/audit | Secrets are redacted; issue/session context is present. | Roll back if secrets are exposed or audit context is missing. |

## Rollback Plan

Rollback unit for this issue:

- Revert or restore cutover docs/config changes from the cutover branch.
- If external Elixir bootstrap runner instructions were changed, restore the
  previous instructions.

Production rollback procedure:

1. Pause or stop the Go instance.
2. Capture sanitized Go logs, `/status`, `/runs`, and latest state DB summary for
   incident review.
3. If Go mutated a Linear issue, leave a Linear workpad note with the rollback
   point and current issue state.
4. Restore the external Elixir bootstrap runner command/service using the
   pre-cutover workflow and environment.
5. Confirm the external runner is polling the same Linear project and no Go
   process is dispatching.
6. Move any partially handled issue to the appropriate human review or rework
   state with a concise rollback note.
7. Keep the Go state DB and workspace root intact until the failure has been
   reviewed; do not delete evidence during rollback.

## Final Production Notes

- The first cutover must stay single-instance and single-repo.
- Fleet manager remains a follow-up and must not be used to justify this cutover.
- Enterprise hardening remains a follow-up; the current gate relies on local
  loopback operator controls, scoped credentials, workspace isolation, redaction,
  audit events, and runtime guardrails.
- Additional tracker adapters remain out of scope for v1 cutover.
- A `GO` decision requires explicit real dogfood evidence. A default skipped real
  integration profile is useful validation of safe skip semantics, but it is not
  production replacement evidence.
