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
| Logs, metrics, and status surfaces | `internal/observability` |

## Required Conformance Matrix

These rows come from `SPEC.md` Section 18.1. They are required before the Go port
can claim core conformance.

| SPEC 18.1 item | Validation profile | Design / package route | Current status | Current evidence and owner |
| --- | --- | --- | --- | --- |
| Workflow path selection supports explicit runtime path and cwd default | Section 17.1, 17.7 | `cmd/symphony`, `internal/workflow` | `implemented` | `TOO-116` covers positional path, `--workflow`, cwd default `./WORKFLOW.md`, duplicate path rejection, missing path errors, and directory path errors with deterministic CLI/workflow tests. Full orchestrator runtime remains outside this row. |
| `WORKFLOW.md` loader with YAML front matter + prompt body split | Section 17.1 | `internal/workflow` | `implemented` | `TOO-117` adds reusable `internal/workflow.Load` / `Parse` behavior with deterministic tests for happy path, missing front matter, invalid YAML, non-map front matter, empty prompt body, and unterminated front matter diagnostics. |
| Typed config layer with defaults and `$` resolution | Section 17.1 | `internal/config` | `implemented` | `TOO-119` adds `config.FromWorkflow` / `config.Load`, SPEC defaults, `$VAR` resolution for explicit env-backed values, `workspace.root` `~` / relative-path normalization, typed validation errors, and deterministic coverage for defaults, env, invalid config, path normalization, and workflow-loader integration. Implementation-defined choices: Codex approval/sandbox values are pass-through strings/maps rather than local enum validation; invalid per-state concurrency override entries are ignored after state-key normalization. |
| Dynamic `WORKFLOW.md` watch/reload/re-apply for config and prompt | Section 17.1, 6.2 | `internal/workflow`, `internal/config`, `internal/orchestrator` | `not_started` | Planned for `TOO-118`. |
| Polling orchestrator with single-authority mutable state | Section 17.4 | `internal/orchestrator` | `partial` | Lifecycle status constants exist; poll/dispatch ownership is not implemented; planned for `TOO-124`. |
| Issue tracker client with candidate fetch + state refresh + terminal fetch | Section 17.3 | `internal/tracker`, `internal/tracker/linear` | `partial` | Normalized issue types exist; Linear read adapter behavior remains for `TOO-122`. |
| Workspace manager with sanitized per-issue workspaces | Section 17.2 | `internal/workspace` | `partial` | Workspace data shape exists; create/reuse/sanitize/root containment behavior remains for `TOO-120`. |
| Workspace lifecycle hooks (`after_create`, `before_run`, `after_run`, `before_remove`) | Section 17.2 | `internal/hooks` | `partial` | Hook data shape exists; execution, cwd, failure, and timeout semantics remain for `TOO-121`. |
| Hook timeout config (`hooks.timeout_ms`, default `60000`) | Section 17.2 | `internal/config`, `internal/hooks` | `partial` | `TOO-119` implements typed defaulting and positive-duration validation in `internal/config`; actual hook execution and timeout enforcement remain for `TOO-121`. |
| Coding-agent app-server subprocess client with JSON line protocol | Section 17.5 | `internal/agent/codex` | `not_started` | Package placeholder exists; protocol client remains for `TOO-126`. |
| Codex launch command config (`codex.command`, default `codex app-server`) | Section 17.1, 17.5 | `internal/config`, `internal/agent/codex` | `partial` | `TOO-119` implements typed defaulting, non-empty validation, and preserves the shell command string without env/path rewriting; actual `bash -lc` process launch remains for `TOO-126`. |
| Strict prompt rendering with `issue` and `attempt` variables | Section 17.1, 12 | `internal/agent`, `internal/orchestrator` | `not_started` | Planned for `TOO-127`. |
| Exponential retry queue with continuation retries after normal exit | Section 17.4, 12.3 | `internal/orchestrator`, `internal/state` | `partial` | Run state shape exists; retry queue and continuation behavior remain for `TOO-125`. |
| Configurable retry backoff cap (`agent.max_retry_backoff_ms`, default 5m) | Section 17.4 | `internal/config`, `internal/orchestrator` | `partial` | `TOO-119` exposes the typed config field with the `300000` ms default and positive-duration validation; retry queue scheduling and cap enforcement remain for `TOO-125`. |
| Reconciliation that stops runs on terminal/non-active tracker states | Section 17.4 | `internal/orchestrator`, `internal/tracker`, `internal/workspace` | `not_started` | Planned for `TOO-125`. |
| Workspace cleanup for terminal issues (startup sweep + active transition) | Section 17.2, 17.4 | `internal/workspace`, `cmd/symphony cleanup` | `not_started` | CLI cleanup command is currently a placeholder; cleanup behavior remains for `TOO-125` / `TOO-132`. |
| Structured logs with `issue_id`, `issue_identifier`, and `session_id` | Section 17.6 | `internal/observability` | `partial` | Generic event shape exists; required fields and sinks remain for `TOO-129`. |
| Operator-visible observability (structured logs; optional snapshot/status surface) | Section 17.6, 13 | `internal/observability`, `cmd/symphony status` | `partial` | Status command and observability package exist as placeholders; structured logs are required, status/snapshot surfaces are optional unless shipped. Planned for `TOO-129` and `TOO-133`. |

## Recommended Extensions Matrix

These rows come from `SPEC.md` Section 18.2. They are not required for core SPEC
conformance unless this repository chooses to ship them. The design document selects
some of them as part of the Go product roadmap.

| SPEC 18.2 extension | Requirement boundary | Go port decision | Current status | Owner / notes |
| --- | --- | --- | --- | --- |
| HTTP server extension honors CLI `--port` over `server.port`, safe default bind host, and Section 13.7 endpoints/error semantics if shipped | Extension conformance only if HTTP server ships | Selected for production/operator controls, not required for bootstrap | `deferred` | Planned for `TOO-133`. |
| `linear_graphql` client-side tool extension exposes raw Linear GraphQL through app-server session using configured Symphony auth | Extension conformance only if tool ships | Selected by the architecture route as an L1 Go-port capability | `partial` | Package placeholder exists; implementation planned for `TOO-128`. |
| Persist retry queue and session metadata across process restarts | Recommended production readiness | Selected for production baseline | `deferred` | Durable state planned for `TOO-132`. |
| Make observability settings configurable in workflow front matter without prescribing UI details | Recommended production readiness | Selected after base observability exists | `deferred` | Depends on `TOO-129` / `TOO-133`. |
| Add first-class tracker write APIs for comments/state transitions instead of only agent tools | Recommended extension | Selected for Linear workpad/state transition reliability | `deferred` | Planned for `TOO-135`. |
| Add pluggable issue tracker adapters beyond Linear | Optional portability | Not selected for v1 | `not_applicable` | V1 targets Linear-compatible operation. Reconsider only after core Linear flow is stable. |

## Operational Validation Matrix

These rows come from `SPEC.md` Section 18.3. They are recommended before the Go port
is used as the production replacement for the bootstrap runner. They do not block
core conformance unless this repository explicitly promotes them into a release gate.

| SPEC 18.3 item | Architecture route | Current status | Owner / notes |
| --- | --- | --- | --- |
| Run the Real Integration Profile from Section 17.8 with valid credentials and network access | Phase 3 Go Dogfood, `TOO-131` | `deferred` | Real Linear/Codex dogfood is intentionally deferred until fake deterministic coverage exists. |
| Verify hook execution and workflow path resolution on the target host OS/shell environment | Phase 1 bootstrap and Phase 3 dogfood, `internal/workflow`, `internal/hooks` | `deferred` | Deterministic unit coverage should land in `TOO-116` / `TOO-121`; host smoke belongs to dogfood or cutover validation. |
| If the optional HTTP server is shipped, verify configured port behavior and loopback/default bind expectations on the target environment | Operator controls, `TOO-133` | `deferred` | HTTP server behavior is selected for the production baseline but is not part of the current bootstrap runtime. |

## Production Baseline Matrix

The architecture document separates SPEC conformance from the production baseline.
These rows are not all required by Section 18.1, but they are required before using
the Go port as the production replacement.

| Production baseline area | Design source | Current status | Owner / notes |
| --- | --- | --- | --- |
| Durable state store, crash recovery, retry/session persistence | Architecture Sections 6.2, 7, 14 | `deferred` | Planned for `TOO-132`. |
| Orphan workspace reconciliation and terminal cleanup hardening | Architecture Sections 7, 14 | `deferred` | Starts with `TOO-125`; durable recovery in `TOO-132`. |
| Safe sandbox defaults and scoped credentials | Architecture Section 8 | `documented` | Defaults documented in `WORKFLOW.md`; hardening/redaction planned for `TOO-134`. |
| Secret redaction, audit log, sandbox/cost guardrails | Architecture Sections 8, 15 | `deferred` | Planned for `TOO-134`. |
| Workflow validation command | Architecture Sections 6.2, 13 | `partial` | `TOO-116` makes `symphony validate [workflow]` a successful readable-file startup preflight with CLI tests; `TOO-117` adds reusable YAML/front matter parsing in `internal/workflow`; `TOO-119` adds reusable typed config validation via `internal/config.Load`, while wiring that validation into the CLI command remains a later runtime/CLI slice. |
| Health/readiness/metrics and operator controls | Architecture Sections 6.2, 13 | `deferred` | Planned for `TOO-133`. |
| Pause/resume/drain/cancel/retry command behavior | Architecture Section 13 | `partial` | CLI command surface exists; runtime behavior planned for `TOO-133`. |
| Per-project concurrency, per-issue timeout, max turns, and cost limits | Architecture Sections 6.2, 8, 13 | `partial` | Some config fields exist; policy and enforcement are later implementation work. |
| Typed Linear writes for comments and state transitions | Architecture Section 6.2 | `deferred` | Planned for `TOO-135`. |
| Cutover runbook and replacement gate | Architecture Section 14 | `deferred` | Planned for `TOO-136`; external Symphony remains bootstrap runner until cutover. |

## Deferred and Implementation-Defined Decisions

| Decision | Status | Current decision |
| --- | --- | --- |
| Elixir source in this repository | `implementation_defined` | The Go repository must not retain, vendor, or copy Elixir source. |
| Bootstrap runner | `implementation_defined` | External Symphony is allowed only as bootstrap runner while the Go port is built. It is not part of the Go repository's runtime source. |
| Workspace population/synchronization | `implementation_defined` | SPEC allows implementation-defined population logic. This port will document the chosen clone/sync policy when workspace management is implemented. |
| Existing non-directory workspace path policy | `implementation_defined` | SPEC allows replace-or-fail behavior. This port must choose and test the policy in the workspace slice. |
| Approval, sandbox, and user-input policy | `implementation_defined` | Defaults are documented in `WORKFLOW.md`; production hardening must document non-stalling approval/user-input behavior before production use. |
| Codex config validation depth | `implementation_defined` | `TOO-119` treats `codex.approval_policy`, `codex.thread_sandbox`, and `codex.turn_sandbox_policy` as Codex pass-through values and validates only local type shape / required command presence. Enum-level validation belongs to the Codex client/runtime slice if the port chooses to enforce it. |
| Per-state concurrency override errors | `implementation_defined` | `TOO-119` follows SPEC by normalizing state keys to lowercase and ignoring invalid `agent.max_concurrent_agents_by_state` entries instead of failing the whole config. |
| Real integration tests | `deferred` | Real Linear/Codex dogfood is deferred until `TOO-131`; fake deterministic tests should precede real credentials/network tests. |
| Tracker writes | `deferred` | Initial bootstrap may use agent-side Linear tools. First-class typed write APIs are deferred to `TOO-135`. |
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
