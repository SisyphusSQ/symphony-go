---
tracker:
  kind: linear
  api_key: "$LINEAR_API_KEY"
  project_slug: "760daeff8700"
  active_states:
    - Todo
    - In Progress
    - Rework
  terminal_states:
    - Done
    - Closed
    - Canceled
    - Cancelled
    - Duplicate

polling:
  interval_ms: 5000

server:
  port: 0

workspace:
  root: "$SYMPHONY_WORKSPACE_ROOT"

# Durable state records local runs, sessions, retry queue rows, and agent events
# for restart recovery and release-gate evidence.
state_store:
  path: "$SYMPHONY_STATE_DB"
  lease_timeout_ms: 300000

hooks:
  after_create: |
    if [ ! -d .git ]; then
      tmp="$(mktemp -d)"
      trap 'rm -rf "$tmp"' EXIT
      git clone "$SOURCE_REPO_URL" "$tmp/repo"
      cp -a "$tmp/repo"/. .
    fi
    go version
    go mod download

  before_run: |
    if [ ! -d .git ]; then
      tmp="$(mktemp -d)"
      trap 'rm -rf "$tmp"' EXIT
      git clone "$SOURCE_REPO_URL" "$tmp/repo"
      cp -a "$tmp/repo"/. .
    fi
    git fetch origin main
    git status --short

  after_run: |
    git status --short || true

  before_remove: |
    git clean -fdx || true

  timeout_ms: 60000

agent:
  max_concurrent_agents: 1
  max_turns: 20
  max_run_duration_ms: 14400000
  # Token usage is tracked for observability by default. Set max_total_tokens
  # to a positive value only when a hard token-count stop is desired.
  # Set both max_cost_usd and cost_per_million_tokens_usd to enable an
  # estimated token-cost guardrail for deployments with a known pricing model.
  max_cost_usd: 0
  cost_per_million_tokens_usd: 0
  max_concurrent_agents_by_state:
    Rework: 1

codex:
  command: codex app-server
  approval_policy: on-request
  thread_sandbox: workspace-write
  turn_sandbox_policy:
    type: workspaceWrite
  turn_timeout_ms: 3600000
  read_timeout_ms: 5000
  stall_timeout_ms: 300000
---

You are working on Linear issue `{{ issue.identifier }}`.

Title: {{ issue.title }}

Description:
{{ issue.description }}

## Mission

Implement the Go version of Symphony in this repository.

Treat `SPEC.md` as the product and runtime contract. Treat `AGENTS.md`,
`docs/harness/*`, `.agents/PLANS.md`, and `.agents/prompts/*` as required
workflow instructions for every run. The ignored `docs/symphony/` directory may
hold local operator notes and real machine credentials, but it is not the
repository-owned source of truth.

## Execution Model

Use the Symphony single-issue loop:

1. Read the current Linear issue, comments, and existing workpad.
2. Scope the current issue only; do not create or depend on a parent-child
   issue hierarchy.
3. Freeze the issue-local plan and acceptance criteria.
4. Implement, verify, review, write back, and prepare the PR for this issue.
5. Move the issue only according to the workflow state semantics below.

## Status Map

- `Backlog`: out of scope. Do not modify.
- `Todo`: move to `In Progress`, create or update `## Codex Workpad`, then execute.
- `In Progress`: continue implementation.
- `Human Review`: do not code. Wait for review.
- `Merging`: follow `.codex/skills/land/SKILL.md`; do not call provider merge
  buttons or direct merge commands unless that skill allows it.
- `Rework`: treat as an approach reset and re-read all review feedback.
- `Done`: terminal. Do nothing.

## Workpad Protocol

Use exactly one persistent issue comment titled:

`## Codex Workpad`

Keep it updated in place. It must contain:

```text
<hostname>:<absolute-workspace-path>@<short-git-sha>
```

Sections:

- `### Plan`
- `### Acceptance Criteria`
- `### Validation`
- `### Notes`
- `### Confusions`

## Completion Bar Before Human Review

Only move to `Human Review` when all are true:

- Acceptance criteria are complete.
- Relevant conformance or design documentation is updated.
- `gofmt` has been run.
- `go test ./...` passes.
- Targeted package tests pass.
- PR is linked to the Linear issue.
- PR feedback sweep is complete.
- PR checks are green or documented as irrelevant with justification.
- Workpad is accurate and current.
