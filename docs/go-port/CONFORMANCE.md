# Go Port Conformance Charter

## Purpose

This document is the Go port charter and conformance tracking surface for Symphony.
It records how this repository will implement the contract in `SPEC.md`, with the
initial matrix anchored to Section 18, "Implementation Checklist (Definition of
Done)".

The matrix is intentionally status-bearing. Future implementation issues must update
the relevant rows whenever they implement, defer, or make implementation-defined a
SPEC behavior.

## Port Charter

| Area | Charter |
| --- | --- |
| Primary contract | `SPEC.md` is the product and runtime contract. Section 17 defines validation profiles; Section 18 is the initial Definition of Done checklist. |
| Architecture route | `docs/design/architecture/symphony_go_design.md` defines the bootstrap route and Go package map used by this port. |
| Bootstrap model | External Symphony may run outside this repository as the bootstrap runner while this Go port is built. |
| Source boundary | This repository does not retain, vendor, or copy Elixir source. Existing Symphony behavior may be used only as behavioral reference outside this repo. |
| V1 operating shape | One Symphony instance manages one `WORKFLOW.md`, one Linear project, one execution repository, one workspace root, and one state database. |
| Multi-repo routing | Multi-repo progress under one Linear project is handled by multiple instances plus repo-label routing, not by one instance mutating several repos. |
| Conformance ownership | Every implementation slice owns the rows it changes and must update this matrix before closeout. |

## Conformance Categories

| Category | SPEC source | Review meaning |
| --- | --- | --- |
| Required conformance | Section 18.1 | These rows must be implemented with deterministic validation before the Go port can claim core SPEC conformance. |
| Recommended extensions | Section 18.2 | These rows are optional unless the Go port chooses to ship them; selected rows must then meet extension conformance. |
| Operational validation | Section 18.3 | These rows are recommended before production use and are tracked separately from core conformance. |
| Production baseline | Architecture Sections 6, 7, 8, 13, 14, and 15 | These rows are Go-port production requirements from the architecture route, including some work beyond Section 18.1. |

## Status Vocabulary

| Status | Meaning |
| --- | --- |
| `documented` | This issue documents the contract, but no runtime behavior is claimed. |
| `not_started` | No meaningful runtime implementation exists yet. |
| `partial` | A type, command surface, or placeholder exists, but behavior or tests are incomplete. |
| `implemented` | Runtime behavior and deterministic tests satisfy the listed contract. |
| `deferred` | Intentionally left for a later implementation slice or release phase. |
| `implementation_defined` | SPEC allows the implementation to choose policy; the chosen policy must be documented before the row can be completed. |
| `not_applicable` | The port intentionally does not ship the optional capability. |

## Architecture Route

The current bootstrap route from the design document is:

```text
external Symphony bootstrap runner
  -> reads this repo's WORKFLOW.md
  -> polls the configured Linear project
  -> dispatches one issue into one isolated workspace
  -> runs Codex against the Go repository
  -> prepares code review / PR handoff
```

The Go implementation route maps to these package areas:

| Runtime area | Planned package path |
| --- | --- |
| Workflow loading and path rules | `internal/workflow` |
| Typed workflow/config view | `internal/config` |
| Tracker normalization and Linear adapter | `internal/tracker`, `internal/tracker/linear` |
| Workspace lifecycle and path safety | `internal/workspace` |
| Lifecycle hooks | `internal/hooks` |
| Polling, dispatch, retry, and reconciliation | `internal/orchestrator` |
| Agent attempt orchestration | `internal/agent` |
| Codex app-server protocol client | `internal/agent/codex` |
| Agent-facing Linear GraphQL tool | `internal/tools/lineargraphql` |
| Durable run/session/retry state | `internal/state` |
| Logs, metrics, and status surfaces | `internal/observability`, `internal/server` |

## Required Conformance Matrix

These rows come from `SPEC.md` Section 18.1. They are required before the Go port
can claim core conformance.

| SPEC 18.1 item | Validation profile | Design / package route | Current status | Current evidence and owner |
| --- | --- | --- | --- | --- |
| Workflow path selection supports explicit runtime path and cwd default | Section 17.1, 17.7 | `cmd/symphony`, `internal/workflow` | `implemented` | `TOO-116` covers positional path, `--workflow`, cwd default `./WORKFLOW.md`, duplicate path rejection, missing path errors, and directory path errors with deterministic CLI/workflow tests. Full orchestrator runtime remains outside this row. |
| `WORKFLOW.md` loader with YAML front matter + prompt body split | Section 17.1 | `internal/workflow` | `implemented` | `TOO-117` adds reusable `internal/workflow.Load` / `Parse` behavior with deterministic tests for happy path, missing front matter, invalid YAML, non-map front matter, empty prompt body, and unterminated front matter diagnostics. |
| Typed config layer with defaults and `$` resolution | Section 17.1 | `internal/config` | `implemented` | `TOO-119` adds `config.FromWorkflow` / `config.Load`, SPEC defaults, `$VAR` resolution for explicit env-backed values, `workspace.root` `~` / relative-path normalization, typed validation errors, and deterministic coverage for defaults, env, invalid config, path normalization, and workflow-loader integration. `TOO-123` adds the Go-port `tracker.issue_filter` extension model for repo label routing. `TOO-134` adds production-safe Codex sandbox/approval defaults plus fail-closed validation for known unsafe sandbox, approval, env inheritance, and resource-limit shapes. Invalid per-state concurrency override entries are still ignored after state-key normalization. |
| Dynamic `WORKFLOW.md` watch/reload/re-apply for config and prompt | Section 17.1, 6.2 | `internal/workflow`, `internal/config`, `internal/orchestrator` | `implemented` | `TOO-118` adds content-hash polling via `config.Reloader`, last-known-good fallback for invalid reloads, deterministic `workflow_reload_invalid` / `workflow_reload_applied` result semantics, and `orchestrator.Runtime` future-dispatch integration tests proving reload updates later config/prompt snapshots without clearing active runtime state. |
| Polling orchestrator with single-authority mutable state | Section 17.4 | `internal/orchestrator`, `internal/policy` | `implemented` | `TOO-123` adds reusable dispatch policy decisions in `internal/policy`, covering active/terminal eligibility, Todo blocker gating, priority/created_at/identifier ordering, running/claimed guards, and repo label routing reasons. `TOO-124` adds the reload-aware orchestrator runtime loop with immediate tick, interval polling, runtime-owned mutable state, fake-tested tracker candidate dispatch, workspace prepare, `after_create` / `before_run` / `after_run` hook sequencing, agent runner launch, global concurrency limit, per-state concurrency limit, and CLI `run` startup wiring. `TOO-125` extends the same single-authority runtime state with in-memory retry entries, retry-claimed dispatch guards, active-run reconciliation, and terminal workspace cleanup. Durable restart state remains separate. |
| Issue tracker client with candidate fetch + state refresh + terminal fetch | Section 17.3 | `internal/tracker`, `internal/tracker/linear` | `implemented` | `TOO-122` adds the read-only `tracker.Client` interface plus `internal/tracker/linear` HTTP GraphQL adapter. Fake-server tests cover project `slugId` candidate filtering, active/terminal state fetches, GraphQL `[ID!]` state refresh, pagination integrity, Linear error mapping, and labels/blockers normalization. Real Linear dogfood remains deferred to the real integration profile. |
| Workspace manager with sanitized per-issue workspaces | Section 17.2 | `internal/workspace` | `implemented` | `TOO-120` adds `workspace.Manager` integration with typed `config.Workspace`, deterministic issue identifier sanitization, root containment checks, per-issue directory create/reuse metadata, and deterministic tests for sanitize, path containment, existing directory reuse, new directory creation, sanitized-key collision rejection, and fail-safe existing non-directory/symlink handling. Clone/fetch population, durable workspace database, and terminal cleanup remain separate rows/follow-ups. |
| Workspace lifecycle hooks (`after_create`, `before_run`, `after_run`, `before_remove`) | Section 17.2 | `internal/hooks` | `implemented` | `TOO-121` adds `hooks.Runner` from typed `config.Hooks`, `sh -lc` workspace-cwd execution, stdout/stderr/exit/timeout result capture, comparable hook errors, skipped empty-hook behavior, and create-only `after_create` helper. Deterministic tests cover cwd, success, failure output, timeout, skipped hooks, invalid cwd, and `after_create` only running for new workspaces. Orchestrator dispatch/cleanup policy remains for later rows. |
| Hook timeout config (`hooks.timeout_ms`, default `60000`) | Section 17.2 | `internal/config`, `internal/hooks` | `implemented` | `TOO-119` implements typed defaulting and positive-duration validation in `internal/config`; `TOO-121` enforces the effective timeout in `internal/hooks.Runner`, defaults direct zero-value runner construction to `config.DefaultHookTimeout` (`60000 ms`), and covers custom/default timeout behavior plus timeout termination in focused tests. |
| Coding-agent app-server subprocess client with JSON line protocol | Section 17.5 | `internal/agent/codex` | `implemented` | `TOO-126` adds a runner-facing Codex app-server JSONL client using the locally generated Codex CLI `0.128.0` protocol shape for `initialize`, `thread/start`, `turn/start`, notifications, token usage, and terminal/error events. Fake app-server helper-process tests cover success, malformed JSON, read timeout, stall timeout, turn timeout, process error, subprocess cwd/env behavior, and approval/sandbox payload forwarding. Real Codex credential dogfood remains deferred to the real integration profile. |
| Codex launch command config (`codex.command`, default `codex app-server`) | Section 17.1, 17.5 | `internal/config`, `internal/agent/codex` | `implemented` | `TOO-119` implements typed defaulting, non-empty validation, and preserves the shell command string without env/path rewriting; `TOO-126` launches `bash -lc <codex.command>` in the workspace cwd and validates this behavior with a fake app-server subprocess. |
| Strict prompt rendering with `issue` and `attempt` variables | Section 17.1, 12 | `internal/agent`, `internal/orchestrator` | `implemented` | `TOO-127` adds the orchestrator-facing `internal/agent` runner with strict Liquid-style dotted interpolation for normalized `issue` fields and optional `attempt`, render failures for missing variables / unknown filters before Codex launch, max-turn loop enforcement, continuation prompt handling, Codex timeout/cwd request wiring, and normalized run/session metadata. Fake turn-client tests cover prompt rendering, missing variables, unknown filters, first/retry attempt behavior, cwd, timeout propagation, max turns, and metadata; orchestrator tests cover issue/attempt/config wiring into the runner. Full Liquid control-flow tags and real Codex dogfood remain deferred to later profiles. |
| Exponential retry queue with continuation retries after normal exit | Section 17.4, 12.3 | `internal/orchestrator`, `internal/state` | `implemented` | `TOO-125` adds retry entries with issue id, identifier, attempt, due time, and error; normal worker exit schedules a 1s continuation retry, abnormal exit schedules 10s-based exponential retries, due retry handling re-fetches active candidates, dispatches eligible retries, requeues slot exhaustion, and releases missing/non-eligible candidates. `TOO-132` persists retry rows in SQLite when `state_store.path` is enabled, deletes released/claimed retry rows, and fake-tests restart recovery of persisted retries. |
| Configurable retry backoff cap (`agent.max_retry_backoff_ms`, default 5m) | Section 17.4 | `internal/config`, `internal/orchestrator` | `implemented` | `TOO-119` exposes the typed config field with the `300000` ms default and positive-duration validation; `TOO-125` enforces the cap in failure retry scheduling and covers capped attempt-2 timing with fake-clock orchestrator tests. |
| Reconciliation that stops runs on terminal/non-active tracker states | Section 17.4 | `internal/orchestrator`, `internal/tracker`, `internal/workspace` | `implemented` | `TOO-125` runs tracker state refresh before dispatch on each tick, updates active running entries in memory, cancels and releases non-active running issues without cleanup, and cancels terminal running issues with terminal workspace cleanup. Deterministic fake tracker/runner tests cover active state updates, non-active stop without retry/cleanup, and terminal stop with cleanup. Stall process termination remains outside this slice. |
| Workspace cleanup for terminal issues (startup sweep + active transition) | Section 17.2, 17.4 | `internal/workspace`, `internal/orchestrator` | `implemented` | `TOO-125` adds workspace cleanup target/remove APIs that resolve sanitized issue workspaces without creating missing paths, runs `before_remove` before removing real directories, ignores hook failures while still attempting removal, and invokes cleanup from both startup terminal sweep and terminal active-run reconciliation. CLI cleanup command behavior remains deferred to the operator-controls slice. |
| Structured logs with `issue_id`, `issue_identifier`, and `session_id` | Section 17.6 | `internal/observability`, `internal/orchestrator` | `implemented` | `TOO-129` adds shared observability event types, JSON logger, discard logger, deterministic recorder, and orchestrator wiring for dispatch, workspace, hook, agent, retry, reconcile, tracker-error, and cleanup events. Focused fake tests assert emitted issue identifiers, session id, run status, retry state, and error fields. |
| Operator-visible observability (structured logs; optional snapshot/status surface) | Section 17.6, 13 | `internal/observability`, `internal/orchestrator`, `internal/server` | `implemented` | `TOO-129` adds `Runtime.Snapshot()` as a read-only status snapshot covering lifecycle state, active runs, and retry queue rows. `TOO-133` adds the local HTTP status surface and CLI HTTP client commands for `/healthz`, `/readyz`, `/status`, `/runs`, and `/metrics`; focused tests cover endpoint status codes, JSON error envelopes, readiness degradation, and Prometheus-style active/retry/lifecycle gauges. Durable audit/dashboard history remains outside this row. |

## Recommended Extensions Matrix

These rows come from `SPEC.md` Section 18.2. They are not required for core SPEC
conformance unless this repository chooses to ship them. The design document selects
some of them as part of the Go product roadmap.

| SPEC 18.2 extension | Requirement boundary | Go port decision | Current status | Owner / notes |
| --- | --- | --- | --- | --- |
| HTTP server extension honors CLI `--port` over `server.port`, safe default bind host, and Section 13.7 endpoints/error semantics if shipped | Extension conformance only if HTTP server ships | Selected for production/operator controls, not required for bootstrap | `implemented` | `TOO-133` adds `internal/server` with loopback-local operator endpoints (`/healthz`, `/readyz`, `/metrics`, `/status`, `/doctor`, `/runs`, `/runs/{id}`, `/runs/{id}/cancel`, `/runs/{id}/retry`, `/orchestrator/pause`, `/orchestrator/resume`, `/orchestrator/drain`, `/orchestrator/cleanup`). `cmd/symphony run` now resolves HTTP enablement with CLI `--port` taking precedence over explicit workflow `server.port`, including `--port 0` ephemeral tests, and binds `127.0.0.1` by default. Runtime tests cover pause/resume/drain dispatch suppression, cancel without retry requeue, and retry wake-up semantics; endpoint tests cover 404/405/409 JSON error envelopes. Authentication/RBAC and fleet aggregation remain explicit follow-ups. |
| `linear_graphql` client-side tool extension exposes raw Linear GraphQL through app-server session using configured Symphony auth | Extension conformance only if tool ships | Selected by the architecture route as an L1 Go-port capability | `implemented` | `TOO-128` adds `internal/tools/lineargraphql` with the raw `{ query, variables }` schema, exactly-one-operation validation, configured Linear endpoint/auth reuse, structured success/error output, GraphQL `errors` failure handling, and fake Linear server tests for success, variables, transport error, invalid JSON/input, and GraphQL error. `internal/agent/codex` now advertises `linear_graphql` as an app-server dynamic tool only when `tracker.kind == linear` has auth, and fake app-server tests cover registration plus `item/tool/call` response wiring. |
| Persist retry queue and session metadata across process restarts | Recommended production readiness | Selected for production baseline | `implemented` | `TOO-132` adds optional `state_store.path` config, `internal/state` SQLite migrations, durable `runs` / `retry_queue` / `sessions` / `agent_events` tables, orchestrator Store interface integration, startup recovery, focused SQLite tests for missing/corrupt DB and idempotent recovery, and orchestrator tests proving persisted retry/interrupted-run recovery. |
| Make observability settings configurable in workflow front matter without prescribing UI details | Recommended production readiness | Selected after base observability exists | `deferred` | Depends on `TOO-129` / `TOO-133`. |
| Add first-class tracker write APIs for comments/state transitions instead of only agent tools | Recommended extension | Selected for Linear workpad/state transition reliability | `implemented` | `TOO-135` adds typed Linear write APIs in `internal/tracker/linear` for issue comment create/update, idempotent workpad upsert by heading, issue state transition by state ID or team state name lookup, and URL attachment links. Fake GraphQL server tests cover success paths, duplicate workpad heading handling without creating another active comment, missing issue/comment semantics, auth/status/GraphQL errors, state lookup failure, and link handling. The raw `linear_graphql` tool remains available as an escape hatch. |
| Add pluggable issue tracker adapters beyond Linear | Optional portability | Not selected for v1 | `not_applicable` | V1 targets Linear-compatible operation. Reconsider only after core Linear flow is stable. |

## Operational Validation Matrix

These rows come from `SPEC.md` Section 18.3. They are recommended before the Go port
is used as the production replacement for the bootstrap runner. They do not block
core conformance unless this repository explicitly promotes them into a release gate.

| SPEC 18.3 item | Architecture route | Current status | Owner / notes |
| --- | --- | --- | --- |
| Deterministic fake end-to-end profile can prove the core conformance path without real credentials or external services | Core bootstrap confidence before real dogfood, `TOO-130` | `implemented` | `TOO-130` adds `make test-fake-e2e`, which runs the real workflow/config loader, real Linear client against a local fake GraphQL server, real workspace manager/hooks, real Codex JSONL client against a fake app-server subprocess, retry/status assertions, terminal cleanup, and structured observability checks. Runbook: `docs/test/fake-e2e/RUNBOOK.md`. |
| Run the Real Integration Profile from Section 17.8 with valid credentials and network access | Phase 3 Go Dogfood, `TOO-131` | `partial` | `TOO-131` adds `make test-real-integration` and `docs/test/real-dogfood/RUNBOOK.md`. The profile is default-skipped unless `SYMPHONY_REAL_INTEGRATION=1` is set; missing required variables fail explicit runs. Current sanitized result: default skip verified, `LINEAR_API_KEY` and workspace root present, `SOURCE_REPO_URL` and explicit enablement absent, no external artifacts created. Full dogfood execution remains blocked until an isolated active issue, `SOURCE_REPO_URL`, workspace root, and local Codex auth are supplied. |
| Verify hook execution and workflow path resolution on the target host OS/shell environment | Phase 1 bootstrap and Phase 3 dogfood, `internal/workflow`, `internal/hooks` | `deferred` | Deterministic unit coverage should land in `TOO-116` / `TOO-121`; host smoke belongs to dogfood or cutover validation. |
| If the optional HTTP server is shipped, verify configured port behavior and loopback/default bind expectations on the target environment | Operator controls, `TOO-133` | `deferred` | HTTP server behavior is selected for the production baseline but is not part of the current bootstrap runtime. |

## Production Baseline Matrix

The architecture document separates SPEC conformance from the production baseline.
These rows are not all required by Section 18.1, but they are required before using
the Go port as the production replacement.

| Production baseline area | Design source | Current status | Owner / notes |
| --- | --- | --- | --- |
| Durable state store, crash recovery, retry/session persistence | Architecture Sections 6.2, 7, 14 | `implemented` | `TOO-132` adds SQLite-backed local state in `internal/state`, optional `state_store` config, automatic migration, durable run/session/retry/event write-through, startup conversion of interrupted running rows into due retries, and deterministic tests covering create/update/query, retry persistence, idempotent restart recovery, claim/lease behavior, and corrupt/missing DB. |
| Orphan workspace reconciliation and terminal cleanup hardening | Architecture Sections 7, 14 | `partial` | `TOO-125` implements tracker-driven startup terminal cleanup and active terminal transition cleanup for known terminal issues. `TOO-132` adds durable running-state crash recovery by marking persisted running rows interrupted and queueing due retries. Filesystem orphan discovery remains a follow-up outside this issue. |
| Safe sandbox defaults and scoped credentials | Architecture Section 8 | `implemented` | `TOO-134` changes `WORKFLOW.md` and config defaults to `approval_policy: on-request`, `thread_sandbox: workspace-write`, and `turn_sandbox_policy.type: workspaceWrite`; validation rejects `never`, `danger-full-access` / `dangerFullAccess`, and all-shell environment inheritance. This is a production baseline posture and does not claim enterprise RBAC, approval queues, container isolation, or a secret-manager provider. |
| Secret redaction, audit log, sandbox/cost guardrails | Architecture Sections 8, 15 | `implemented` | `TOO-134` adds centralized redaction in `internal/safety`, applies it before runtime logger and durable audit writes, persists redacted hook/agent/tool/guardrail events through the existing state store event surface, and enforces per-issue max-turn, max-duration, max-token, and configured estimated-cost guardrails. |
| Workflow validation command | Architecture Sections 6.2, 13 | `partial` | `TOO-116` makes `symphony validate [workflow]` a successful readable-file startup preflight with CLI tests; `TOO-117` adds reusable YAML/front matter parsing in `internal/workflow`; `TOO-119` adds reusable typed config validation via `internal/config.Load`, while wiring that validation into the CLI command remains a later runtime/CLI slice. |
| Health/readiness/metrics and operator controls | Architecture Sections 6.2, 13 | `implemented` | `TOO-133` adds loopback-local HTTP health/readiness/status/runs/metrics/doctor and control endpoints with focused endpoint/runtime tests. |
| Pause/resume/drain/cancel/retry command behavior | Architecture Section 13 | `implemented` | `TOO-133` wires pause/resume/drain/cancel/retry/cleanup runtime behavior through the local operator HTTP surface and CLI client commands; authentication/RBAC remains an enterprise hardening follow-up. |
| Per-project concurrency, per-issue timeout, max turns, and cost limits | Architecture Sections 6.2, 8, 13 | `implemented` | Earlier orchestrator slices enforce global and per-state concurrency. `TOO-134` adds per-issue max turns, max run duration, max total tokens, and optional estimated token-cost limits, with guardrail stops treated as non-retryable resource boundary events. |
| Typed Linear writes for comments and state transitions | Architecture Section 6.2 | `implemented` | `TOO-135` adds first-class Linear write APIs for comment create/update, single-heading workpad upsert, state transition, and URL attachment link writeback, with deterministic fake server coverage and no browser-based workflow. |
| Cutover runbook and replacement gate | Architecture Section 14 | `documented` | `TOO-136` adds `docs/go-port/CUTOVER_RUNBOOK.md`, a final cutover decision gate, rollback plan, post-cutover monitoring standards, and residual-risk summary. Current decision remains `NO-GO` until the explicit real dogfood and production cutover smoke gates pass in the target environment; external Symphony remains the bootstrap runner. |

## Final Cutover Conformance Summary

`TOO-136` defines the final Go replacement gate for the v1 operating shape:

```text
one Go Symphony instance
  -> one WORKFLOW.md
  -> one Linear project
  -> one execution repo
  -> one workspace root
  -> one SQLite state database
```

Current cutover decision:

| Field | Value |
| --- | --- |
| Decision | `NO-GO` |
| Reason | `TOO-136` passed the deterministic/local gates and the default real-dogfood skip gate, but the explicit real dogfood/full production cutover gate still requires target-environment credentials, an isolated active issue, repo URL, workspace root, and local Codex auth. |
| Replacement claim | Not made. The Go implementation is not documented as the production replacement until every required gate passes. |
| Fallback | Keep the external Symphony bootstrap runner active or immediately recoverable. |
| Runbook | `docs/go-port/CUTOVER_RUNBOOK.md` |

`TOO-136` validation evidence:

| Gate | Current evidence |
| --- | --- |
| Full Go regression | `go test ./...` passed. |
| Harness consistency | `make harness-check` passed. |
| Fake E2E | `make test-fake-e2e` passed. |
| Real dogfood default safety | `make test-real-integration` passed with explicit skip because `SYMPHONY_REAL_INTEGRATION` was not set. |
| Durable state | Focused SQLite/orchestrator recovery tests passed. |
| Operator controls | Focused orchestrator/server/CLI control tests passed. |
| Production safety | Focused safety/config/observability/agent/orchestrator/state tests passed. |
| Typed Linear writes | Focused Linear write API and raw GraphQL tool tests passed. |
| Production baseline smoke | CLI help, validate, run, and loopback ephemeral port smoke commands passed. |
| Explicit real dogfood | Blocked until target-environment enablement is supplied. |

Required gate evidence for a future `GO` decision:

| Gate | Required outcome |
| --- | --- |
| Full Go regression | `go test ./...` passes. |
| Harness consistency | `make harness-check` passes. |
| Fake E2E | `make test-fake-e2e` passes. |
| Real dogfood default safety | `make test-real-integration` succeeds with explicit skip when disabled. |
| Real dogfood explicit run | `SYMPHONY_REAL_INTEGRATION=1 ... make test-real-integration` passes against an isolated active issue. |
| Durable state | SQLite migration, retry/session persistence, restart recovery, and interrupted-run retry recovery pass. |
| Operator controls | pause/resume/drain/cancel/retry/status/ready/metrics and CLI startup behavior pass. |
| Production safety | safe sandbox defaults, redaction, audit events, and runtime/cost guardrails pass. |
| Typed Linear writes | comment/workpad/state/link writes and raw Linear GraphQL escape hatch pass deterministic tests. |
| Production baseline smoke | CLI validate/run/help and loopback operator startup smoke pass. |

## Cutover Residual Risk List

| Risk | Status | Cutover impact |
| --- | --- | --- |
| Explicit real dogfood not yet proven in the target environment | blocking for replacement | Required `GO` gate; default skip is not enough. |
| Filesystem orphan discovery beyond tracker-known terminal cleanup | residual follow-up | Does not block the documented gate, but should remain an operator monitoring item during cutover. |
| Observability settings front matter remains deferred | residual follow-up | Current local status/metrics endpoints exist; configurable observability sinks remain future work. |
| Enterprise hardening | out of scope | RBAC, approval queue, container workers, secret manager, and policy engine are not part of this v1 cutover gate. |
| Fleet manager / single-instance multi-repo | out of scope | V1 cutover is one instance per repo; fleet aggregation must be implemented separately. |
| Additional tracker adapters | out of scope | Linear remains the only v1 tracker target. |

## Deferred and Implementation-Defined Decisions

| Decision | Status | Current decision |
| --- | --- | --- |
| Elixir source in this repository | `implementation_defined` | The Go repository must not retain, vendor, or copy Elixir source. |
| Bootstrap runner | `implementation_defined` | External Symphony is allowed only as bootstrap runner while the Go port is built. It is not part of the Go repository's runtime source. |
| Workspace population/synchronization | `implementation_defined` | SPEC allows implementation-defined population logic. This port will document the chosen clone/sync policy when workspace management is implemented. |
| Existing non-directory workspace path policy | `implementation_defined` | `TOO-120` chooses fail-safe behavior: if the computed per-issue workspace path already exists as a file, symlink, or other non-real directory, workspace preparation returns an error and does not replace or follow it. |
| Approval, sandbox, and user-input policy | `implementation_defined` | `TOO-134` chooses a production baseline of non-`never` approval plus workspace-write sandbox defaults. Config validation rejects the known unsafe local posture values rather than silently passing them through. Full approval queue semantics remain out of scope. |
| Codex config validation depth | `implementation_defined` | `TOO-134` keeps Codex config mostly pass-through, but adds explicit production-baseline rejection for `approval_policy: never`, `thread_sandbox: danger-full-access`, `turn_sandbox_policy.type: dangerFullAccess`, and all-shell environment inheritance. The port still does not maintain a full Codex enum catalog. |
| Per-state concurrency override errors | `implementation_defined` | `TOO-119` follows SPEC by normalizing state keys to lowercase and ignoring invalid `agent.max_concurrent_agents_by_state` entries instead of failing the whole config. |
| Tracker `issue_filter` extension | `implementation_defined` | `TOO-123` adds `tracker.issue_filter` as a Go-port extension, not core SPEC schema. It supports `require_labels`, `reject_labels`, `require_any_labels`, and `require_exactly_one_label_prefix`; policy rejects missing or ambiguous repo routing labels with machine-readable reasons instead of guessing an execution repo. |
| Dynamic workflow reload strategy | `implementation_defined` | `TOO-118` uses a passive poll/check API with SHA-256 content hashing rather than a long-lived file watcher dependency. Valid content changes replace the last-known-good typed config and prompt for future dispatch. Invalid changed content returns `workflow_reload_invalid`, emits a deterministic operator message with `keeping_last_known_good=true`, and keeps the previous effective config; in-flight sessions are not restarted automatically. |
| Real integration tests | `implementation_defined` | `TOO-131` defines the opt-in real profile through `make test-real-integration`. The default path reports an explicit skip; explicit enablement requires `LINEAR_API_KEY`, `SYMPHONY_WORKSPACE_ROOT`, `SOURCE_REPO_URL`, and `SYMPHONY_REAL_DOGFOOD_ISSUE`, and failures must fail the command. |
| Durable local state store | `implementation_defined` | `TOO-132` uses SQLite through `modernc.org/sqlite` for repo-local durability. Empty `state_store.path` preserves in-memory behavior. Missing DB files are created and migrated; corrupt DB files fail startup without replacement. Startup recovery treats local `running` rows as interrupted crash artifacts, clears their lease, and upserts one due retry per issue. Retry rows are keyed by issue id to avoid duplicate restart retries. |
| Tracker writes | `implementation_defined` | `TOO-135` chooses Linear-only typed write APIs in `internal/tracker/linear` for standard comment/workpad/state/link writebacks while keeping raw `linear_graphql` as an escape hatch. Provider-agnostic tracker writes and full PR synchronization remain follow-ups. |
| Pluggable trackers | `not_applicable` | Linear-compatible tracking is the v1 target. |
| Single-instance multi-repo orchestration | `not_applicable` | V1 uses one instance per execution repo. Cross-repo work must be split into repo-specific executable issues. |

## Update Rules for Future Issues

1. Update one or more matrix rows whenever a future issue implements, defers, or makes
   implementation-defined a SPEC behavior.
2. A row may move to `implemented` only when both runtime behavior and deterministic
   validation exist for that row.
3. Optional features must stay in the recommended-extension matrix until they are
   selected or explicitly marked `not_applicable`.
4. Production-only requirements must stay separate from required conformance so the
   repository can distinguish "SPEC core" from "ready to replace the bootstrap runner".
5. New conformance rows discovered later should be added here before the relevant
   issue closes.

## Validation Commands for This Matrix

Run these commands for changes to this document:

```bash
go test ./...
make harness-check
```
