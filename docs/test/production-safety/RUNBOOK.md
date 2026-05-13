# Production Safety Runbook

## Purpose

Validate the production baseline safety controls shipped by `TOO-134`.

This runbook covers:

- safe Codex sandbox and approval defaults
- config validation for unsafe sandbox, approval, and environment inheritance,
  plus explicit trusted-local opt-in
- secret redaction for logs, hook output, errors, workflow/config display, and audit payloads
- durable audit event persistence for important runtime, hook, agent, tool, and guardrail events
- per-issue max-turn, max-duration, opt-in positive max-token, and optional estimated-cost guardrails

## Safety And Side Effects

- The deterministic tests do not require real Linear or Codex credentials.
- Tests may create temporary directories and temporary SQLite databases through Go test helpers.
- No real token, cookie, credential, database host, connection string, or machine-local secret should be written into this document.
- Guardrail cost enforcement is an estimate based on configured token price and observed token usage. It is not a billing reconciliation system.
- Token usage is tracked for observability by default; `agent.max_total_tokens` only hard-stops a run when set to a positive value.
- This baseline does not implement enterprise RBAC, approval queues, containerized workers, a policy engine, fleet governance, or a secret-manager provider.

## Preconditions

- Current working directory is the repository root.
- The branch under test contains the `TOO-134` safety implementation.
- `go` and `make` are available.
- The local plan cache exists at `.agents/plans/TOO-134.md` when running the harness review gate.

## Commands

Focused safety regression:

```bash
go test ./internal/safety ./internal/config ./internal/observability ./internal/agent ./internal/agent/codex ./internal/orchestrator ./internal/state
```

Full regression:

```bash
go test ./...
```

Harness gates:

```bash
make harness-check
make harness-review-gate PLAN=.agents/plans/TOO-134.md
```

## Expected Results

- Config defaults resolve to workspace-write sandbox posture and non-`never` approval.
- Config validation rejects known unsafe sandbox, approval, and all-shell
  environment inheritance values by default.
- `symphony run --allow-unsafe-codex ...` and
  `SYMPHONY_ALLOW_UNSAFE_CODEX=true symphony run ...` allow those high-trust
  values only for an explicit trusted local run and print an operator warning.
- Redaction tests prove literal secrets, secret-like keys, token patterns, and secret-bearing paths are replaced before output.
- Hook, agent, tool, and guardrail audit payloads are persisted only after redaction.
- Guardrail exceeded results stop the current run, write a local-terminal
  suppression, and do not schedule a retry.
- Full Go tests and harness gates exit successfully.

## Failure Handling

| Failure point | Stop condition | Record | Recovery / rerun |
| --- | --- | --- | --- |
| Focused tests fail | Stop | Package and test name with a sanitized failure summary | Fix the safety slice and rerun focused tests |
| Full regression fails | Stop | Failing package and sanitized failure summary | Fix regression and rerun `go test ./...` |
| Harness check fails | Stop | Harness finding and related file | Update required docs/checklist and rerun |
| Review gate fails | Stop | Blocking finding and plan section | Update plan/code/docs, rerun verification and review gate |

## Current Verification Result

Executed in the `TOO-134` workspace:

```bash
go test ./internal/safety ./internal/config ./internal/observability ./internal/agent ./internal/agent/codex ./internal/orchestrator ./internal/state
go test ./...
make harness-check
make harness-review-gate PLAN=.agents/plans/TOO-134.md
```

Result:

- focused package tests: passed
- sanitized secret-leak assertions: passed
- guardrail non-retry assertions: passed
- full Go regression: passed
- harness check: passed
- harness review gate: passed with `blocking_findings=none`

Sensitive information handling:

- No real credentials, tokens, cookies, database hosts, connection strings, row keys, full download URLs, or raw external responses are recorded here.
