# Final Core Release Gate Runbook

## Purpose

This runbook is the single execution plan for the final core release gate before
`symphony-go` is treated as release-ready.

It composes the existing deterministic gates, production baseline checks,
operator UI smoke, real Go binary self-dogfood, cutover readiness, and release
artifact checks into one ordered decision flow.

This document is a gate plan. It does not claim the application is released.
Release is allowed only after every required gate below passes on the release
candidate revision and the result is written back with sanitized evidence.

## Current Validation Result

- Recorded date: 2026-05-11
- Recorded directory: `docs/test/release-gate/`
- Run type: release gate execution
- Current conclusion: `NO-GO`
- Automated entry points:
  - `make verify`
  - `make test-fake-e2e`
  - `make test-real-integration`
  - focused Go package tests listed below
  - `npm test` under `web/`
  - `npm run build` under `web/`
- Related runbooks:
  - `docs/test/real-dogfood/TEST_PLAN.md`
  - `docs/test/real-dogfood/RUNBOOK.md`
  - `docs/go-port/CUTOVER_RUNBOOK.md`
  - `docs/test/operator-ui/RUNBOOK.md`
  - `docs/test/production-safety/RUNBOOK.md`
- Result summary: deterministic repository gates, focused production gates,
  operator UI gates, explicit real preflight, endpoint smoke, restart smoke,
  rollback stop, and local artifact build checks ran on 2026-05-11. The final
  release decision is `NO-GO` because the live Go self-dogfood phase
  redispatched the same active dogfood issue after the first Codex turn
  completed. The operator was paused, the second run was canceled, the Go
  runtime was stopped, and the dogfood issue was moved to `Human Review` to
  prevent further automatic dispatch.

```text
release_candidate: 81af829 plus uncommitted release-gate/workflow/test evidence overlay
gate_started_at: 2026-05-11 18:10 CST
gate_finished_at: 2026-05-11 18:36 CST
decision: NO-GO
dogfood_issue: TOO-145
operator_endpoint: loopback, port redacted
workspace_root: dedicated path confirmed, full path not recorded
state_db: dedicated sqlite path confirmed, full path not recorded
external_runner: no matching external runner process observed; Go runtime stopped after rollback
changelog_action: updated Unreleased
changelog_version: Unreleased
verification_summary:
  - deterministic gates: pass
  - focused production gates: pass
  - operator UI gates: pass
  - explicit real preflight: pass, one candidate TOO-145
  - full Go dogfood: fail, same issue redispatched after first Codex turn
  - restart rollback: pass for stop/no-redispatch after Human Review; interrupted retry proof remains blocked by NO-GO
  - release artifact readiness: partial pass for classify/build/help; archive/tag publication not run
residual_risks:
  - release candidate freeze failed because evidence required an uncommitted workflow/test/runbook overlay
  - live dogfood must prevent same-issue redispatch after a completed turn before final GO
```

### 2026-05-12 Token Guardrail Alignment Verification

After aligning `agent.max_total_tokens` with the original Symphony default
posture, the local verification reran the deterministic and focused gates below.
This verification does not replace the final live dogfood gate because no
single active dogfood issue was selected at the time of rerun.

```text
decision: NO-GO for final release
fix_scope: default token guardrail disabled unless agent.max_total_tokens is a positive explicit value
verification_summary:
  - config and agent focused tests: pass
  - full Go regression: pass
  - harness check: pass
  - full local verify: pass
  - fake E2E: pass
  - default real integration: pass with explicit skip
  - focused production gates: pass
  - operator UI tests/build/static smoke: pass
  - release skill changelog gate: pass
  - explicit real preflight: blocked, configured dogfood issue was not in Todo/In Progress/Rework
residual_risks:
  - final live dogfood still requires selecting exactly one isolated active issue
  - release candidate freeze still requires committing or otherwise freezing the current overlay
```

### Step Status

| Gate | Result | Notes |
| --- | --- | --- |
| Release candidate freeze | fail | Worktree contained release-gate/workflow/test evidence overlay; final proof did not run from a clean committed revision. |
| Deterministic repository gates | pass | `go test ./...`, `make harness-check`, `make verify`, `make test-fake-e2e`, and default `make test-real-integration` passed. |
| Operator UI release surface | pass | `npm test`, `npm run build`, `go test ./internal/server ./cmd/symphony`, and dashboard static-serving smoke passed. |
| Production baseline focused gates | pass | Durable state, operator controls, production safety, and typed Linear write API focused tests passed. |
| Explicit real preflight | pass | After correcting the workflow project slug, real preflight saw exactly one active candidate, `TOO-145`, and `codex --version` succeeded. |
| Full Go binary self-dogfood | fail | Go runtime started, endpoint smoke passed, state store was configured, Codex was invoked, and Linear Workpad was written, but the same issue was redispatched after the first turn. |
| Restart and rollback smoke | partial | Rollback stop passed; restart with the same workspace/state DB showed zero running/retry work after moving `TOO-145` to `Human Review`. Full interrupted retry proof remains blocked by the dogfood NO-GO. |
| Release artifact readiness | partial | Changed-file classification returned `runtime-build`; `make clean`, `make build`, `bin/symphony --help`, and `bin/symphony run --help` passed. No release archive/tag/publish step ran. |

## Release Boundary

The final core release gate is narrower than a fleet rollout and broader than a
single dogfood run.

In scope:

- one release candidate revision
- one Go Symphony instance
- one committed `WORKFLOW.md`
- one Linear project
- one execution repository
- one dedicated workspace root
- one dedicated SQLite state database
- one isolated low-risk dogfood issue
- local operator HTTP surface on loopback
- Web operator UI production build and static serving
- release artifact readiness checks for the CLI binary and release notes

Out of scope:

- fleet manager
- single-instance multi-repo operation
- enterprise RBAC or approval queue
- containerized worker isolation
- secret-manager provider
- additional tracker adapters beyond Linear
- browser-based merge or release button workflows

Passing this gate means the release candidate can proceed to tag/artifact
publication and the documented v1 cutover shape. It does not approve expanding
the operating shape beyond the scope above.

## Safety And Side Effects

Expected local side effects:

- `bin/symphony` may be built.
- `web/dist` may be generated by `npm run build`.
- temporary Go test directories and SQLite files may be created by test helpers.
- a dedicated dogfood workspace root and state database are used for the live
  Go binary self-dogfood run.

Expected external side effects when explicit real dogfood is enabled:

- Linear is read by the real integration preflight.
- The Go binary may update one configured workpad comment or state transition on
  the isolated dogfood issue.
- The local Codex app-server is invoked by the Go runtime for the dogfood issue.

Do not commit or paste:

- token values
- auth files
- raw Linear responses
- raw Codex transcripts
- full local workspace paths
- SQLite database files
- temporary cloned workspaces
- raw logs containing private host details

If the scope of an external write is unclear, stop before starting the live
dogfood phase.

## Required Inputs

| Input | Requirement |
| --- | --- |
| Release candidate revision | The exact commit intended for release; no unreviewed worktree overlay. |
| Branch state | Current branch or detached revision is understood and recorded. |
| Workflow | Committed `WORKFLOW.md` using safe production defaults. |
| Linear credentials | Scoped `LINEAR_API_KEY` for the target project. |
| Dogfood issue | Exactly one isolated active issue in `Todo`, `In Progress`, or `Rework`; not `Merging`. |
| Repo URL | `SOURCE_REPO_URL` points to the execution repository for clone hooks. |
| Workspace root | Dedicated writable directory outside the repository checkout. |
| State DB | Dedicated SQLite path supplied by `state_store.path` or `SYMPHONY_STATE_DB`. |
| Codex | `codex` command and local auth are available to the same OS user running Symphony. |
| External runner | Existing external Elixir runner can be paused, stopped, and restarted. |
| Web dependencies | `web/node_modules` is installed before Web build and UI smoke. |

## Environment Initialization

Run from the repository root:

```bash
set -euo pipefail

git status --short --branch
git rev-parse --short HEAD
go version
codex --version
```

Load private dogfood inputs from an ignored local file or equivalent private
operator shell. `SYMPHONY_REAL_DOGFOOD_ISSUE` should be exported explicitly
before sourcing the file so the target is visible in shell history without
revealing token values.

```bash
export SYMPHONY_REAL_DOGFOOD_ISSUE=TOO-xxx
set -a
source docs/test/real-dogfood/secrets.env
set +a
```

Expected:

- repository status is understood before the gate begins
- no release candidate proof depends on an uncommitted worktree overlay
- `codex --version` succeeds
- secret values are not printed

## Gate Matrix

| Gate | Required | Command / Evidence | Pass Criteria |
| --- | --- | --- | --- |
| RC freeze | yes | `git status --short --branch`, `git rev-parse --short HEAD` | Exact release candidate revision is recorded; unrelated dirty files are absent. |
| Go regression | yes | `go test ./...` | All Go packages pass. |
| Harness consistency | yes | `make harness-check` | Harness contract passes. |
| Full local verify | yes | `make verify` | Formatting, tidy, Go tests, build, CLI help, and harness checks pass. |
| Fake E2E | yes | `make test-fake-e2e` | Deterministic fake Linear + fake Codex profile passes. |
| Real default safety | yes | `make test-real-integration` | Command succeeds with explicit SKIP when real integration is not enabled. |
| Durable state | yes | focused state/orchestrator test below | SQLite migration, retry/session persistence, restart recovery, and interrupted-run recovery pass. |
| Operator controls | yes | focused operator/server/CLI test below | pause/resume/drain/cancel/retry/status/ready/metrics and CLI startup behavior pass. |
| Production safety | yes | focused safety/config/observability/agent/state test below | sandbox defaults, redaction, audit events, and runtime/cost guardrails pass. |
| Linear write APIs | yes | focused Linear/tooling test below | comment/workpad/state/link APIs and raw GraphQL tool behavior pass. |
| Operator UI tests | yes | `cd web && npm test` | Vitest suite for the Web UI passes. |
| Operator UI build | yes | `cd web && npm run build` | Vite production build succeeds and writes only ignored `web/dist`. |
| Operator UI serving | yes | `go test ./internal/server -run TestOperatorServerDashboardSmoke -count=1` | `/`, `/api/v1/*`, assets, history fallback, and missing asset behavior are correct. |
| Production CLI smoke | yes | commands listed below | CLI help, validate, and run startup surfaces succeed. |
| Explicit real preflight | yes | `SYMPHONY_REAL_INTEGRATION=1 ... make test-real-integration` | Real Linear candidate fetch sees exactly the isolated dogfood issue and `codex --version` succeeds. |
| Full Go self-dogfood | yes | `go run ./cmd/symphony run ...` plus endpoint/state/Linear evidence | Go binary dispatches exactly one issue with real dependencies, Codex invocation, workpad/state writeback, and coherent state DB evidence. |
| Restart smoke | yes | stop/restart same workflow, workspace root, and state DB | Completed work is not redispatched; interrupted work becomes issue-scoped retry work. |
| Rollback smoke | yes | pause/stop Go, restore external runner, verify no Go dispatch | Operator can return to external runner without double-dispatch. |
| Release artifact readiness | yes | changelog archive dry-run/write, binary build, release notes review | Version boundary, artifacts, and notes are ready without leaking secrets. |

Any required gate with `fail`, `blocked`, or `not-run` means final release
decision is `NO-GO`.

## Deterministic Repository Gates

Run:

```bash
go test ./...
make harness-check
make verify
make test-fake-e2e
make test-real-integration
```

Expected:

- `go test ./...` passes.
- `make harness-check` passes.
- `make verify` passes without leaving unintended source changes.
- `make test-fake-e2e` passes.
- `make test-real-integration` succeeds with an explicit skip when
  `SYMPHONY_REAL_INTEGRATION` is not set.
- `git diff --exit-code -- go.mod go.sum cmd internal` passes after `make verify`,
  or any formatter/tidy changes are reviewed and the gate is restarted from the
  frozen release candidate.

If `make verify` changes formatted Go files or `go.mod` / `go.sum`, inspect the
diff and restart the gate after deciding whether those changes belong in the
release candidate.

## Focused Production Baseline Gates

Durable state:

```bash
go test ./internal/state ./internal/orchestrator \
  -run 'TestSQLiteStore|TestRuntimeRecoverState|TestRuntimeStartupRecovery' \
  -count=1
```

Operator controls:

```bash
go test ./internal/orchestrator ./internal/server ./cmd/symphony \
  -run 'TestRuntimePauseDrainAndResumeControlDispatch|TestRuntimeCancelRunStopsActiveAttemptWithoutRetry|TestRuntimeRetryRunWakesQueuedRetryAndRejectsRunningRun|TestHandlerServesStatusRunsReadinessAndMetrics|TestHandlerControlEndpointsAndErrorSemantics|TestRunPerformsStartupValidationWithoutStartingRuntime' \
  -count=1
```

Production safety:

```bash
go test ./internal/safety ./internal/config ./internal/observability ./internal/agent ./internal/agent/codex ./internal/orchestrator ./internal/state \
  -count=1
```

Typed Linear write APIs:

```bash
go test ./internal/tracker/linear ./internal/tools/lineargraphql \
  -run 'TestCreateAndUpdateIssueComment|TestUpsertIssueWorkpad|TestTransitionIssueState|TestLinkIssueURL|TestWriteAPIErrors|TestExecute|TestAvailableOnlyForLinearTrackerWithAuth' \
  -count=1
```

Expected:

- each focused command exits successfully
- failure output is summarized by package/test name only in committed evidence
- no raw secrets, host-specific paths, or external responses are written into
  committed docs

## Operator UI Release Surface

Run:

```bash
cd web
npm test
npm run build
cd ..

go test ./internal/server ./cmd/symphony
go test ./internal/server -run TestOperatorServerDashboardSmoke -count=1
```

Expected:

- Web unit tests pass.
- Web production build succeeds.
- `web/dist` remains ignored and is not committed.
- Go server tests pass.
- dashboard static serving smoke proves `/`, `/api/v1/*`, built assets, browser
  history fallback, and missing asset behavior.

If a manual daemon UI smoke is run, use the loopback URL printed by
`symphony run` and record only sanitized endpoint results:

```bash
BASE="$SYMPHONY_OPERATOR_ENDPOINT"
curl -fsS "$BASE/" >/dev/null
curl -fsS "$BASE/api/v1/state" >/dev/null
curl -fsS "$BASE/healthz" >/dev/null
curl -fsS "$BASE/readyz" >/dev/null
```

## Production CLI Smoke

Run:

```bash
go run ./cmd/symphony --help
go run ./cmd/symphony run --help
go run ./cmd/symphony validate WORKFLOW.md
go run ./cmd/symphony run --workflow WORKFLOW.md --instance release-gate-smoke
go run ./cmd/symphony run --workflow WORKFLOW.md --port 0 --instance release-gate-smoke
```

Expected:

- help commands print usage and exit successfully
- `validate WORKFLOW.md` reports startup validation success
- run startup surfaces do not require browser/manual workflows
- if dispatch dependencies are configured, operator readiness can be checked on
  the printed loopback URL
- if dispatch dependencies are missing in a non-live smoke, the output must make
  that explicit and the live dogfood phase must still run separately

## Explicit Real Preflight

Run only after loading private dogfood inputs:

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
- real Linear candidate fetch returns exactly the target dogfood issue.
- `codex --version` succeeds for the runtime OS user.
- missing variables or API failures fail the command.

This is not full self-dogfood evidence by itself.

## Full Go Binary Self-Dogfood

Before starting:

1. Pause or keep stopped the external Elixir bootstrap runner for the target
   Linear project.
2. Confirm no external-runner session is mutating the dogfood issue.
3. Confirm only the dogfood issue matches the Go instance dispatch filter.
4. Confirm `SYMPHONY_WORKSPACE_ROOT` is outside this repository.
5. Confirm `SYMPHONY_STATE_DB` points to a dedicated SQLite file.

Start the Go binary:

```bash
go run ./cmd/symphony run \
  --workflow WORKFLOW.md \
  --port 0 \
  --instance "${SYMPHONY_DOGFOOD_INSTANCE:-release-gate-dogfood}"
```

Endpoint smoke:

```bash
BASE="$SYMPHONY_OPERATOR_ENDPOINT"
curl -fsS "$BASE/healthz"
curl -fsS "$BASE/readyz"
curl -fsS "$BASE/status"
curl -fsS "$BASE/runs"
curl -fsS "$BASE/metrics"
```

Pass criteria:

- process prints a loopback operator URL
- `/healthz`, `/readyz`, `/status`, `/runs`, and `/metrics` are reachable from
  the outer operator shell
- Go runtime admits exactly one issue
- workspace is created under `SYMPHONY_WORKSPACE_ROOT`
- `after_create`, `before_run`, and `after_run` hooks behave as expected
- `codex app-server` starts through the Go Codex runner
- Codex receives the target issue prompt and exits with a clear result
- Linear has exactly one active configured workpad comment for the issue
- state transition or PR link writeback matches the issue workflow
- state database records run, session, retry, and event evidence for the same
  issue
- logs and committed evidence do not expose secrets
- external Elixir runner is not responsible for dispatch

## Restart And Rollback Smoke

Restart smoke:

1. Pause or drain the Go instance through the operator endpoint.
2. Stop the Go process.
3. Restart with the same `WORKFLOW.md`, `SYMPHONY_WORKSPACE_ROOT`, and
   `SYMPHONY_STATE_DB`.
4. Query `/status` and `/runs`.

Pass criteria:

- completed work is not redispatched as a new first attempt
- interrupted work is recovered as issue-scoped retry work
- retry rows do not duplicate the same issue
- operator status matches state database and Linear activity

Rollback smoke:

1. Pause or stop the Go instance.
2. Capture sanitized `/status`, `/runs`, and state DB summaries.
3. Leave a concise Linear workpad note if Go mutated the issue.
4. Restore or resume the external runner only after Go is stopped.
5. Confirm no Go process is dispatching.
6. Move the dogfood issue to the intended review or rework state.

Pass criteria:

- external runner can resume without double-dispatching the dogfood issue
- dogfood issue has a human-readable rollback point
- workspace root and state DB are preserved until review completes

## Release Artifact Readiness

Run after every runtime and dogfood gate passes.

Classify changed files. On a release branch, compare against `origin/main`. On
`main`, compare against the previous release tag or another explicitly recorded
release baseline.

```bash
git diff --name-only <release-baseline>...HEAD \
  | python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py classify \
      --repo . --changed-files-from - --json
```

Archive `ChangeLog.md` only when the release version and date are fixed:

```bash
python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py release-archive \
  --repo . \
  --version vX.Y.Z \
  --date YYYYMMDD \
  --write --json
```

Build the release binary:

```bash
make clean
make build
bin/symphony --help
bin/symphony run --help
```

Release notes check:

- `ChangeLog.md` has no empty `Unreleased` ambiguity for the version being
  published.
- Release notes mention the supported v1 shape and out-of-scope boundaries.
- Release notes mention the real dogfood issue only in sanitized form.
- Release notes do not include raw credentials, local paths, raw Linear
  responses, state DB contents, or Codex transcripts.

The release artifact gate does not push tags, upload artifacts, or publish a
GitHub release by itself. Those actions happen only after the final decision is
`GO`.

## Final Decision

Return `GO` only when all are true:

1. every required gate in this runbook is `pass`
2. full Go binary self-dogfood ran from the release candidate revision without
   an uncommitted worktree overlay
3. restart and rollback smoke passed
4. operator UI production build and serving smoke passed
5. external Elixir runner was paused or otherwise proven not to dispatch the
   dogfood issue
6. release artifact readiness passed
7. sanitized evidence was written back to this runbook, the Linear workpad, and
   any release issue or PR used for the release

Return `NO-GO` when any are true:

1. any required gate fails, is blocked, or is not run
2. explicit real dogfood only reaches the default skip path
3. more than one issue is eligible for dispatch
4. `Merging` is eligible for first cutover dispatch
5. Go and external runner could dispatch from the same project at the same time
6. target workflow operates on multiple repos from one instance
7. required credentials are broad personal tokens instead of scoped runtime
   credentials
8. operator health/readiness/metrics cannot be confirmed
9. restart recovery or rollback path is unknown
10. committed evidence contains secrets or machine-local traces

## Result Writeback Template

After execution, replace the current result section with a sanitized summary:

```text
release_candidate: <short commit>
gate_started_at: <YYYY-MM-DD HH:MM TZ>
gate_finished_at: <YYYY-MM-DD HH:MM TZ>
decision: GO | NO-GO
dogfood_issue: <sanitized issue id>
operator_endpoint: loopback, port redacted or generic
workspace_root: dedicated path confirmed, full path not recorded
state_db: dedicated sqlite path confirmed, full path not recorded
external_runner: paused/restored/not-used
changelog_action: archived <version> | updated Unreleased | not-applicable with reason
changelog_version: <version> | Unreleased | not-applicable
verification_summary:
  - deterministic gates: pass/fail/blocker
  - focused production gates: pass/fail/blocker
  - operator UI gates: pass/fail/blocker
  - explicit real preflight: pass/fail/blocker
  - full Go dogfood: pass/fail/blocker
  - restart rollback: pass/fail/blocker
  - release artifact readiness: pass/fail/blocker
residual_risks:
  - <sanitized residual risk or none>
```

Keep raw logs, database files, local workspaces, and private credentials outside
committed docs.
