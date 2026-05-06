# Real Linear + Codex Self-Dogfood Runbook

## Purpose

This runbook defines the controlled real Linear/Codex profile used for Go
`symphony-go` self-dogfood runs. It is separate from deterministic fake E2E
coverage and from the external Elixir Symphony runner.

The real preflight command proves credential and target-issue readiness only.
Full self-dogfood evidence requires starting the Go binary with real tracker,
workspace, hook, Codex runner, and state-store dependencies.

## Scope

Covered by this profile:

- real `WORKFLOW.md` config load with committed dogfood-safe defaults
- real Linear project candidate read using `LINEAR_API_KEY`
- isolated low-risk dogfood issue discovery by identifier
- local Codex command preflight
- explicit skipped result when the profile is not enabled

Not covered by this profile:

- Go binary dispatch
- workspace mutation by the Go binary
- Codex turn execution through the Go binary
- Linear workpad/state writeback by the Go binary
- production cutover
- external Elixir runner dogfood evidence
- fleet or multi-repo operation
- automatic merge behavior
- storing credentials or raw external command output in committed docs

## Execution Side Effects

The default command has no external side effects because it exits through a
Go test skip before reading Linear or touching a workspace.

When explicitly enabled, the current command performs real Linear read requests
against the configured project and verifies the local `codex` command. It does
not create, update, transition, or delete Linear issues. Full self-dogfood
execution must use the Go binary, an isolated issue/workspace, and the cleanup
steps below.

## Prerequisites

Required for explicit enablement:

```bash
export SYMPHONY_REAL_INTEGRATION=1
export LINEAR_API_KEY=<redacted>
export SYMPHONY_WORKSPACE_ROOT=/absolute/path/to/isolated/symphony-go-workspaces
export SOURCE_REPO_URL=https://github.com/SisyphusSQ/symphony-go.git
export SYMPHONY_REAL_DOGFOOD_ISSUE=TOO-xxx
```

Dogfood target assumptions:

- The issue is in the `symphony-go` Linear project.
- The issue is low risk and safe for a read/preflight dogfood run.
- The issue is in a default active state, currently `Todo`, `In Progress`, or `Rework`.
- The issue is not in `Merging`; default dogfood config does not dispatch `Merging`.

Credential handling:

- Do not commit token values, local auth files, raw command output, or workspace
  paths that reveal private host details.
- Use environment variables or private local operator notes under ignored
  paths such as `docs/symphony/`.

## Local Command

Default skipped profile:

```bash
make test-real-integration
```

Explicit real preflight:

```bash
SYMPHONY_REAL_INTEGRATION=1 \
LINEAR_API_KEY="$LINEAR_API_KEY" \
SYMPHONY_WORKSPACE_ROOT="$SYMPHONY_WORKSPACE_ROOT" \
SOURCE_REPO_URL="$(git remote get-url origin)" \
SYMPHONY_REAL_DOGFOOD_ISSUE="$SYMPHONY_REAL_DOGFOOD_ISSUE" \
make test-real-integration
```

Equivalent Go test command:

```bash
go test ./internal/orchestrator -run TestRealIntegrationProfile -count=1 -v
```

## Expected Results

Default result:

- The test reports `SKIP` with an explicit message that lists the required
  enablement variables.
- The command returns success so ordinary fake/CI tests are not broken by
  missing real credentials.

Explicit result:

- `WORKFLOW.md` loads successfully.
- `agent.max_concurrent_agents` is `1`.
- `Merging` is not part of default active dispatch.
- The target dogfood issue is returned by real Linear candidate fetch.
- `codex --version` succeeds.
- Missing required variables or real API/preflight failures fail the command.

## Cleanup Steps

For the current read/preflight profile, no external cleanup is required.

For a future full self-dogfood run that creates or modifies artifacts:

1. Move or restore the dogfood Linear issue to the intended review state.
2. Delete any temporary test-only Linear issue that was created solely for this profile.
3. Preserve or remove the isolated workspace directory according to the test
   plan. Preserve it when the operator requested evidence retention.
4. Remove local logs or raw outputs that contain host paths, tokens, or external IDs.
5. Record only a sanitized summary in this runbook and the Linear workpad.

## Current Validation Result

Last updated for `TOO-138` on 2026-05-06:

```text
go test ./... -count=1
PASS

make harness-check
PASS

SYMPHONY_REAL_INTEGRATION=1 ... make test-real-integration
PASS: real Linear/Codex preflight saw exactly one active candidate, TOO-138.
```

Observed environment summary, with values intentionally redacted:

```text
LINEAR_API_KEY: present
SYMPHONY_WORKSPACE_ROOT: present
SOURCE_REPO_URL: present
SYMPHONY_STATE_DB: present
SYMPHONY_REAL_DOGFOOD_ISSUE: TOO-138
codex command: present
explicit enablement: present
operator port: 4002
```

Full Go binary dogfood summary:

- The Go `bin/symphony` binary started the operator server on port `4002`.
- `/healthz`, `/readyz`, `/status`, `/runs`, and `/metrics` responded from the
  outer operator shell.
- The first attempt found a real workspace hook bug: the Symphony metadata file
  made `git clone "$SOURCE_REPO_URL" .` unsafe in a new workspace, and retries
  skipped `after_create`.
- After making the clone hook idempotent, the retained workspace became a real
  checkout, the Go runtime dispatched `TOO-138`, invoked Codex, updated the
  existing `## Codex Workpad`, and moved the issue to `Human Review`.
- The state database recorded dispatch/start/workspace events, hook failures,
  retry scheduling, and inactive-state stop after the issue left active states.
- The external Elixir Symphony runner was not used for this proof.

## Residual Risks

- The Codex worker sandbox could not directly connect to `127.0.0.1:4002`; HTTP
  endpoint response evidence came from the outer operator shell.
- The rerun used an ignored current-worktree overlay so the dogfood workspace
  could see uncommitted Go runtime wiring. A final replacement proof should
  rerun after those changes are committed or otherwise available from the clone
  source.
- Restart recovery after an interrupted Codex turn was not covered in this run;
  the observed recovery surface was retry after hook failure and inactive-state
  stop after Linear moved to `Human Review`.
- A successful external Elixir runner smoke remains useful operational evidence,
  but it is not proof that the Go binary can self-dogfood.
