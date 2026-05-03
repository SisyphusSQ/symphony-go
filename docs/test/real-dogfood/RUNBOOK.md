# Real Linear + Codex Dogfood Runbook

## Purpose

This runbook defines the controlled real dogfood profile for Symphony Go. It is
separate from the deterministic fake E2E profile and exists to prove that the Go
runtime can use real Linear credentials, a real workspace root, and the local
Codex command in a low-risk environment.

## Scope

Covered by this profile:

- real `WORKFLOW.md` config load with committed dogfood-safe defaults
- real Linear project candidate read using `LINEAR_API_KEY`
- isolated low-risk dogfood issue discovery by identifier
- local Codex command preflight
- explicit skipped result when the profile is not enabled

Not covered by this profile:

- production cutover
- fleet or multi-repo operation
- automatic merge behavior
- storing credentials or raw external command output in committed docs

## Execution Side Effects

The default command has no external side effects because it exits through a
Go test skip before reading Linear or touching a workspace.

When explicitly enabled, the current command performs real Linear read requests
against the configured project and verifies the local `codex` command. It does
not create, update, transition, or delete Linear issues. Full dogfood execution
must use an isolated issue/workspace and the cleanup steps below.

## Prerequisites

Required for explicit enablement:

```bash
export SYMPHONY_REAL_INTEGRATION=1
export LINEAR_API_KEY=<redacted>
export SYMPHONY_WORKSPACE_ROOT=/absolute/path/to/isolated/symphony-go-workspaces
export SOURCE_REPO_URL=https://github.com/SisyphusSQ/symphony-go.git
export SYMPHONY_REAL_DOGFOOD_ISSUE=TOO-131
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
SYMPHONY_REAL_DOGFOOD_ISSUE=TOO-131 \
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

For a future full dogfood run that creates or modifies artifacts:

1. Move or restore the dogfood Linear issue to the intended review state.
2. Delete any temporary test-only Linear issue that was created solely for this profile.
3. Remove the isolated workspace directory under `SYMPHONY_WORKSPACE_ROOT`.
4. Remove local logs or raw outputs that contain host paths, tokens, or external IDs.
5. Record only a sanitized summary in this runbook and the Linear workpad.

## Current Validation Result

Last updated for `TOO-131` on 2026-05-03:

```text
make test-real-integration
PASS with explicit SKIP: SYMPHONY_REAL_INTEGRATION was not set.
```

Observed environment summary, with values intentionally redacted:

```text
LINEAR_API_KEY: present
SYMPHONY_WORKSPACE_ROOT: present
SOURCE_REPO_URL: missing
codex command: present
explicit enablement: missing
```

## Residual Risks

- The current committed command proves skip/fail semantics and real Linear/Codex
  preflight readiness, but it did not run a full agent turn in this workspace
  because the profile was not explicitly enabled.
- Full dogfood remains blocked until `SYMPHONY_REAL_INTEGRATION=1`, an isolated
  active dogfood issue, `SOURCE_REPO_URL`, workspace root, and local Codex auth
  are all supplied for the run.
- The Go CLI `run` command still reports missing dispatch dependencies when
  constructed without injected runtime collaborators; full production-style
  daemon dogfood remains separate from this controlled profile.
