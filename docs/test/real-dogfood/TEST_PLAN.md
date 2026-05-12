# Symphony Go Self-Dogfood Test Plan

## Purpose

This plan defines how to prove that the Go `symphony-go` binary can dogfood
itself. It must not be confused with deterministic fake E2E coverage, the
current real integration preflight, or a run performed by the external Elixir
Symphony runner.

The self-dogfood goal is to prove that `go run ./cmd/symphony run` or the built
Go binary can use real Linear credentials, a real isolated workspace root, a
real state database, real lifecycle hooks, and the local `codex app-server`
command to handle one low-risk `symphony-go` issue end to end.

This document is both the reusable test plan and the committed, sanitized
summary format for real Go self-dogfood runs.

## Current Boundary

The repository currently has four distinct validation layers:

| Layer | Command / Surface | What it proves | What it does not prove |
| --- | --- | --- | --- |
| Fake E2E | `make test-fake-e2e` | Real workflow/config loading, fake Linear, fake Codex JSONL app-server, workspace/hooks/retry/status behavior without external services | Real credentials, real Linear, real Codex auth, real operator cutover |
| Real preflight | `make test-real-integration` with `SYMPHONY_REAL_INTEGRATION=1` | Real `WORKFLOW.md`, real Linear candidate read, target issue visibility, `codex --version` | Real dispatch, workspace mutation, Codex turn execution, Linear workpad/state writeback |
| External runner smoke | `docs/symphony/WORKFLOW.md` with the local external Elixir Symphony runner | The existing external Symphony runner can drive a `symphony-go` issue lifecycle | Go binary self-dogfood |
| Go self-dogfood | This plan after Go runtime wiring exists | One isolated real issue is dispatched and observed through the Go binary runtime path | Production replacement or fleet operation |

The cutover decision remains `NO-GO` until explicit real dogfood and target
environment cutover smoke evidence pass.

Current status for Go self-dogfood:

- The Go CLI now starts the orchestrator with real side-effecting dependencies:
  Linear tracker, workspace manager, hook runner, Codex runner, and optional
  SQLite state store.
- A real Go binary run on port `4002` dispatched `TOO-138`, created/reused the
  retained workspace, invoked Codex, updated the Linear workpad, and moved the
  issue to `Human Review`.
- Operator endpoint checks passed from the outer operator shell. The Codex
  worker sandbox could observe process/state evidence but could not connect to
  loopback endpoints directly.
- A run performed by the external Elixir runner remains separate evidence and
  cannot satisfy this plan's pass criteria.

## Local Secret File

Use the ignored local file:

```text
docs/test/real-dogfood/secrets.env
```

Load it from the repository root before running explicit real preflight or
self-dogfood commands:

```bash
export SYMPHONY_REAL_DOGFOOD_ISSUE=TOO-xxx
set -a
source docs/test/real-dogfood/secrets.env
set +a
```

Do not commit this file or copy real values into committed docs, Linear
comments, PR descriptions, screenshots, logs, or run summaries.

Required variables:

| Variable | Purpose |
| --- | --- |
| `SYMPHONY_REAL_INTEGRATION` | Must be `1` for explicit real preflight. |
| `LINEAR_API_KEY` | Scoped Linear token for the target project. |
| `SYMPHONY_WORKSPACE_ROOT` | Dedicated absolute workspace root for this dogfood run. |
| `SOURCE_REPO_URL` | Execution repository URL used by `WORKFLOW.md` clone hooks. |
| `SYMPHONY_REAL_DOGFOOD_ISSUE` | New isolated Linear issue identifier, for example `TOO-xxx`; must be set explicitly before sourcing `secrets.env`. |
| `SYMPHONY_STATE_DB` | Dedicated SQLite database path for this Go instance. |

Optional variables:

| Variable | Purpose |
| --- | --- |
| `SYMPHONY_OPERATOR_ENDPOINT` | Loopback operator endpoint used by CLI HTTP subcommands. |
| `SYMPHONY_DOGFOOD_INSTANCE` | Human-readable instance name, for example `dogfood-local`. |

## Target Issue Requirements

Create or select exactly one dogfood issue with these properties:

1. It belongs to the `symphony-go` Linear project.
2. It is low risk and intentionally scoped for dogfood.
3. It is in an active dispatch state: `Todo`, `In Progress`, or `Rework`.
4. It is not in `Merging`.
5. It does not block or depend on active production work.
6. It has a clear acceptance criterion that can be verified from the repo, the
   Linear workpad, the operator endpoints, and the state database.
7. If repo-label routing is enabled, it has exactly one execution repo label,
   such as `repo:symphony-go`, and any dogfood-only label required by the local
   workflow filter.

For the first self-dogfood run, prefer a safe no-code or docs-only issue whose
expected result is small and reviewable. Do not use a production merge issue as
the first dogfood target.

## Environment Preparation

Run these checks from the repository root before any self-dogfood attempt:

```bash
git status --short
go version
codex --version
make test-fake-e2e
make test-real-integration
```

Expected:

- `git status --short` is understood before starting. A dirty tree is allowed
  only when the dogfood target explicitly owns those files.
- `codex --version` succeeds for the same OS user that will run Symphony.
- `make test-fake-e2e` passes.
- `make test-real-integration` passes with an explicit SKIP when
  `SYMPHONY_REAL_INTEGRATION` is not set.

Before the full self-dogfood phase, also confirm the Go runtime wiring exists:

```bash
go run ./cmd/symphony run --workflow WORKFLOW.md --port 0 --instance wiring-smoke
```

Expected:

- The process must not print `dispatch dependencies are not configured`.
- The operator server must be able to report ready state once runtime
  dependencies are configured.
- If the command exits after startup validation because dependencies are
  missing, stop and create a wiring follow-up instead of running dogfood.

Prepare isolated local paths:

```bash
mkdir -p "$SYMPHONY_WORKSPACE_ROOT"
mkdir -p "$(dirname "$SYMPHONY_STATE_DB")"
```

Expected:

- `SYMPHONY_WORKSPACE_ROOT` is not the repository checkout.
- `SYMPHONY_WORKSPACE_ROOT` is dedicated to this dogfood run.
- `SYMPHONY_STATE_DB` points to a dedicated SQLite file for this instance.

## Explicit Real Preflight

After loading `docs/test/real-dogfood/secrets.env`, run:

```bash
SYMPHONY_REAL_INTEGRATION=1 \
LINEAR_API_KEY="$LINEAR_API_KEY" \
SYMPHONY_WORKSPACE_ROOT="$SYMPHONY_WORKSPACE_ROOT" \
SYMPHONY_STATE_DB="$SYMPHONY_STATE_DB" \
SOURCE_REPO_URL="$SOURCE_REPO_URL" \
SYMPHONY_REAL_DOGFOOD_ISSUE="$SYMPHONY_REAL_DOGFOOD_ISSUE" \
make test-real-integration
```

Pass criteria:

- `WORKFLOW.md` loads successfully.
- `agent.max_concurrent_agents` is `1`.
- `Merging` is not part of default active dispatch.
- The target dogfood issue is returned by the real Linear candidate fetch.
- `codex --version` succeeds.
- Missing required variables or real API failures fail the command.

This is still a preflight. It does not count as full self-dogfood evidence by
itself.

## Full Self-Dogfood Execution

Before this phase, confirm the Go runtime entrypoint has real side-effecting
dependencies wired: Linear tracker, workspace manager, hook runner, Codex
runner, and optional SQLite state store. If the startup output says dispatch
dependencies are not configured, stop here and treat full self-dogfood as
blocked.

Recommended operator posture:

1. Stop or keep stopped the external Elixir bootstrap runner for the target
   Linear project.
2. Confirm no external-runner agent session is mutating the dogfood issue.
3. Confirm only the dogfood issue matches the Go instance dispatch filter.
4. Start the Go binary with loopback operator endpoints enabled.

Command shape:

```bash
go run ./cmd/symphony run \
  --workflow WORKFLOW.md \
  --port 0 \
  --instance "${SYMPHONY_DOGFOOD_INSTANCE:-dogfood-local}"
```

Pass criteria for startup:

- The process prints a loopback operator URL.
- `/healthz` returns `200`.
- `/readyz` returns `200` after dispatch dependencies are configured.
- `/status`, `/runs`, and `/metrics` are reachable.
- The process does not report missing dispatch dependencies.
- No external Elixir Symphony process is responsible for the dispatch.

Endpoint smoke:

```bash
BASE="$SYMPHONY_OPERATOR_ENDPOINT"
curl -fsS "$BASE/healthz"
curl -fsS "$BASE/readyz"
curl -fsS "$BASE/status"
curl -fsS "$BASE/runs"
curl -fsS "$BASE/metrics"
```

Pass criteria for one issue dispatch:

- Exactly one issue is admitted for dispatch.
- A workspace is created under `SYMPHONY_WORKSPACE_ROOT`.
- `after_create`, `before_run`, and `after_run` hooks behave as expected.
- `codex app-server` starts through the configured Codex runner.
- The Codex turn receives the target issue prompt and exits with a clear result.
- Linear has exactly one active `## Codex Workpad` or configured workpad comment
  for the issue.
- Any state transition or PR link writeback matches the issue workflow.
- The state database records the run, session, suppression or retry, and event
  rows needed for restart recovery.
- Logs redact secrets and do not expose raw token values.

## Recovery And Restart Smoke

Run this only after one controlled dispatch has started or completed.

1. Pause or drain the Go binary instance through the operator endpoint.
2. Stop the Go process.
3. Restart the Go instance with the same `WORKFLOW.md`, workspace root, and
   `SYMPHONY_STATE_DB`.
4. Query `/status` and `/runs`.

Pass criteria:

- Completed work is not redispatched as a new first attempt.
- Interrupted work is recovered as retry work instead of being marked complete.
- Retry rows are issue-scoped and do not duplicate the same issue.
- Operator status matches the state database and Linear issue activity.

## Rollback Smoke

Rollback must be known before dogfood begins.

1. Pause or stop the Go instance.
2. Capture a sanitized `/status`, `/runs`, and state database summary.
3. Leave a concise Linear workpad note if the Go instance mutated the issue.
4. Restore or resume the external bootstrap runner only after the Go instance
   is stopped and evidence is captured.
5. Confirm no Go process is dispatching.
6. Move the dogfood issue to the appropriate review or rework state.
7. Keep the workspace root and state database until the run is reviewed.

Pass criteria:

- The external runner can resume, if needed, without double-dispatching the same
  issue.
- The dogfood issue has a human-readable rollback point.
- No committed file contains secrets, raw host-specific paths, or raw external
  responses.

## Evidence To Record

Committed evidence may include:

- Commands that were run.
- Pass/fail summary by phase.
- Sanitized issue identifier.
- Sanitized operator endpoint checks.
- Sanitized state database counts.
- Sanitized workspace existence result.
- Links to PRs or Linear workpad entries when safe.

Do not commit:

- `docs/test/real-dogfood/secrets.env`.
- Raw Linear API responses.
- Token values or auth files.
- Full local workspace paths.
- Raw Codex app-server transcripts with private context.
- SQLite database files.
- Temporary cloned workspaces.

## Current Validation Result

Last updated for `TOO-138` on 2026-05-06:

```text
go test ./... -count=1
PASS

make harness-check
PASS

SYMPHONY_REAL_INTEGRATION=1 ... make test-real-integration
PASS: project candidate count was 1 and target was TOO-138
```

Go binary dogfood result:

- The local Go binary command using
  `docs/symphony/WORKFLOW.go-self-dogfood.md`, port `4002`, and instance
  `go-self-dogfood` started the operator server.
- `/healthz`, `/readyz`, `/status`, `/runs`, and `/metrics` responded from the
  outer operator shell.
- The first dispatch exposed a real hook bug: the workspace metadata file made
  `git clone "$SOURCE_REPO_URL" .` unsafe for a new workspace, and retries then
  skipped `after_create`.
- The hook was made idempotent by cloning into a temporary directory and copying
  contents into the workspace while preserving Symphony metadata.
- The rerun applied the current operator worktree diff to the retained dogfood
  workspace, dispatched `TOO-138`, invoked Codex through the Go runtime path,
  updated the existing `## Codex Workpad`, and moved the issue to `Human Review`.
- The Go runtime stopped the active run as `inactive_state` after Linear moved
  out of active dispatch states.

Residual risks from this run:

- The Codex worker sandbox could not directly connect to `127.0.0.1:4002`; HTTP
  endpoint response evidence came from the outer operator shell.
- The run did not perform restart recovery after an interrupted Codex turn; it
  only covered retry after hook failure and inactive-state stop after workpad
  writeback.
- The local workflow used an ignored current-worktree overlay so the dogfood
  workspace could test uncommitted Go runtime wiring. A final replacement proof
  should rerun after the same changes are committed or otherwise available to
  the clone source.

## Pass / Fail Decision

Full self-dogfood passes only when all are true:

1. Fake E2E passes.
2. Real preflight passes with explicit enablement.
3. The Go runtime dispatches exactly one isolated issue with real dependencies.
4. The workspace, hooks, Codex runner, Linear workpad/writeback, operator
   endpoints, and state database all show coherent evidence for the same issue.
5. Restart or rollback behavior has been verified.
6. The sanitized result can be written back without exposing secrets.
7. The external Elixir runner was not the process that dispatched the issue.

Full self-dogfood fails or remains blocked when any are true:

1. The run only reaches default SKIP.
2. Explicit real preflight cannot see the dogfood issue.
3. The CLI/runtime reports missing dispatch dependencies.
4. More than one issue is eligible for dispatch.
5. Workspace paths escape `SYMPHONY_WORKSPACE_ROOT`.
6. `Merging` issues are eligible in the first dogfood run.
7. Secrets appear in logs, docs, comments, or PR text.
8. The external bootstrap runner cannot be paused or restored.
9. The issue was dispatched by the external Elixir Symphony runner instead of
   the Go binary.

## Cleanup

After the review window:

1. Remove temporary workspaces under `SYMPHONY_WORKSPACE_ROOT` only after the
   evidence has been reviewed.
2. Keep or archive `SYMPHONY_STATE_DB` according to the incident/review need.
3. Clear token values from the local shell history if they were entered
   directly.
4. Keep `docs/test/real-dogfood/secrets.env` ignored and local.
5. Write only a sanitized result summary into committed docs if needed.
