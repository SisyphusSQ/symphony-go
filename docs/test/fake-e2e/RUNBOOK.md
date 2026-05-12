# Fake Linear + Fake Codex E2E Runbook

## Purpose

This runbook validates the deterministic fake end-to-end profile for Symphony Go.
It proves the core runtime loop can execute without real Linear credentials,
real Codex credentials, or network-dependent external services.

## Scope

Covered by this profile:

- workflow load and typed config resolution
- fake Linear candidate fetch, blocker normalization, state refresh, and terminal cleanup
- workspace creation, sanitized per-issue paths, and lifecycle hooks
- fake Codex app-server success and failure sessions through the real JSONL client
- retry/status rows and structured observability events

Not covered by this profile:

- real Linear API smoke tests
- real Codex authentication or model execution
- production durable state, cost, security, and cutover hardening

## Side Effects

The test creates only temporary directories and an in-process `httptest` Linear
server. The fake Codex app-server is the current Go test binary launched as a
subprocess with test-only environment variables. No persistent workspace,
credential, or external service artifact is created.

## Local Command

```bash
make test-fake-e2e
```

Equivalent Go test command:

```bash
go test ./internal/orchestrator -run TestFakeE2EProfile -count=1
```

## CI Command

Use the same deterministic target in CI:

```bash
make test-fake-e2e
```

No secret variables are required. CI must not inject real `LINEAR_API_KEY`,
Codex credentials, or network service dependencies for this profile.

## Expected Result

- The fake Linear server receives candidate fetch, terminal fetch, and state
  refresh requests.
- The runtime dispatches a happy-path issue, suppresses completed active work
  from immediate redispatch, records a representative fake Codex failure, and
  schedules a failure retry.
- A blocked issue is skipped because its blocker is non-terminal.
- Terminal workspace cleanup runs through `before_remove` and removes the
  temporary workspace.
- Structured events include issue identifiers, session ids, retry state,
  suppression behavior, and failure error text.

## Current Validation Result

Last updated for `TOO-130`:

```text
make test-fake-e2e
PASS
```
