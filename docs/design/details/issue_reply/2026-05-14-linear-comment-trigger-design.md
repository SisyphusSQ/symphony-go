# Linear Issue Comment Trigger Design

Last updated: 2026-05-14

## Goal

Add a state-driven Linear issue comment trigger to `symphony-go`.

When an issue is in a configured Linear state, Symphony should poll recent issue
comments. If it finds a new comment that was not written by Symphony itself and
has not been processed before, it starts a comment-triggered run and writes an
answer or performs the configured action.

The feature extends the existing polling model. It does not require Linear
webhooks, Linear Agent Sessions, or a public HTTP endpoint.

## Current Repo Context

Current `symphony-go` already has these relevant pieces:

- Linear issue polling by `tracker.active_states`.
- Linear issue normalization with state, labels, blockers, timestamps, and URL.
- Typed Linear write APIs for issue comments, Workpad upsert, state transition,
  and URL attachment.
- Agent-facing raw `linear_graphql` tool for escape-hatch Linear queries and
  mutations.
- Optional SQLite state store for runs, retries, sessions, events, and restart
  recovery.
- A loopback operator API and local observability surfaces.

The missing capability is not comment writing. The missing capability is the
product loop:

1. discover new user comments,
2. filter and deduplicate them,
3. map the issue state to a configured handling mode,
4. run the agent with the triggering comment as first-class context,
5. mark the comment as processed safely.

## Non-goals

- No webhook receiver in the first design.
- No Linear Agent Session or Agent Activity integration in the first design.
- No threaded comment reply requirement in the first design.
- No provider-agnostic comment trigger adapter in the first design.
- No hard-coded Linear workflow states.
- No automatic processing for states that are not configured.

## Recommended Approach

Use a new `comment_triggers` workflow block. It is independent from
`tracker.active_states`.

`tracker.active_states` remains the main issue dispatch queue. `comment_triggers`
defines a separate comment-driven queue keyed by configurable Linear state names.

This keeps normal issue execution and comment-triggered execution separate while
reusing the same runtime, tracker, runner, state store, and observability
surfaces.

## Configuration

Example:

```yaml
comment_triggers:
  enabled: true
  default_mode: ignore
  poll_limit: 20
  context_limit: 10
  ignore_authors:
    - "$LINEAR_BOT_USER_ID"
  rules:
    - states: ["Human Review", "Awaiting Review"]
      mode: answer
    - states: ["Rework", "Needs Changes"]
      mode: execute
    - states: ["Merging"]
      mode: land
```

Fields:

| Field | Meaning |
| --- | --- |
| `enabled` | Feature flag. Defaults to `false`. |
| `default_mode` | Explicit fail-closed fallback. Initial supported value is `ignore`; unconfigured states are not fetched. |
| `poll_limit` | Maximum recent comments fetched per issue in one tick. |
| `context_limit` | Maximum recent comments included in the agent prompt. |
| `ignore_authors` | Linear user IDs or bot IDs to ignore. Environment variables are expanded. |
| `rules[].states` | Arbitrary Linear state names. These are not hard-coded. |
| `rules[].mode` | One of `ignore`, `answer`, `execute`, or `land`. |

Validation rules:

- `enabled=false` requires no other fields.
- `poll_limit` and `context_limit` must be positive when provided.
- `rules[].states` must not be empty.
- `rules[].mode` must be a known mode.
- The same normalized state name must not appear in two rules.
- State names are matched case-insensitively after trimming spaces, but the
  configured display text should be preserved for diagnostics.
- Unconfigured states are ignored by omission: Symphony does not fetch comments
  for them.

## Modes

| Mode | Behavior |
| --- | --- |
| `ignore` | Do not start an agent run. Record the comment as ignored only when a configured rule explicitly maps the current state to `ignore`. |
| `answer` | Start a read-only response run. The agent may inspect issue context and write a concise Linear comment answer. It must not edit files, create branches, or mutate PRs. |
| `execute` | Start a normal implementation/rework run. The trigger comment is treated as fresh user input and may lead to code changes, validation, Workpad updates, and PR updates. |
| `land` | Start a merge/land-scoped run. The agent may handle merge readiness, PR closeout, and Linear writeback, but must not expand into new implementation work. |

The mode is selected only from configuration. New custom Linear states can be
added later by editing workflow YAML, not by changing Go code.

## Polling Data Flow

Each orchestrator tick performs the existing issue dispatch flow and then, when
`comment_triggers.enabled=true`, performs comment trigger discovery.

Comment discovery:

1. Collect all state names referenced in `comment_triggers.rules`.
2. Fetch issues in those states from Linear.
3. For each issue, fetch the most recent comments up to `poll_limit`.
4. Normalize comment ID, body, author ID, created time, updated time, and URL.
5. Drop comments that are empty, deleted, authored by an ignored author, or
   already recorded in the state store.
6. Select the oldest unprocessed comment for that issue.
7. Check current runtime state for the issue. If the issue is already running,
   retrying, or locally suppressed, skip starting a new comment run.
8. Create a durable `comment_event` row and claim a run.
9. Start the agent with a comment-triggered prompt.

Processing one comment per issue per tick preserves comment order and prevents
two related user comments from racing each other.

## Linear Tracker Additions

Extend the Linear tracker implementation with a typed read API for comments.

Suggested types:

```go
type IssueComment struct {
    ID        string
    IssueID   string
    Body      string
    URL       string
    AuthorID  string
    Author    string
    CreatedAt *time.Time
    UpdatedAt *time.Time
}

type IssueCommentQuery struct {
    IssueID string
    First   int
}
```

The existing private `fetchIssueComments` function can be promoted or wrapped,
but it needs author identity and URL fields. The current comment query only
returns `id`, `body`, `createdAt`, and `updatedAt`, which is not enough for
self-comment filtering.

The implementation should also support fetching issue comments for context,
limited by `context_limit`.

## State Store Additions

Add a durable comment-event table. The important idempotency key is the Linear
comment ID.

Suggested schema:

```text
comment_events
- comment_id text primary key
- issue_id text not null
- issue_identifier text not null
- state_at_detection text not null
- author_id text
- body_hash text not null
- mode text not null
- status text not null
- run_id text
- detected_at text not null
- updated_at text not null
- last_error text
```

Statuses:

| Status | Meaning |
| --- | --- |
| `ignored` | The comment was intentionally ignored. |
| `pending` | The comment was discovered and is ready to process. |
| `running` | A run has claimed the comment. |
| `completed` | The run finished and the comment should not be processed again. |
| `failed` | The run failed after claiming the comment. |

Restart behavior:

- A completed comment must never be reprocessed.
- A running comment associated with an interrupted run should recover through
  the same run/retry recovery model instead of creating a second first attempt.
- If `comment_triggers.enabled=true` and no durable state store is configured,
  the runtime should fail closed for comment triggers. It should not process
  comments without durable idempotency.

## Prompt Contract

Comment-triggered runs should use an explicit prompt shape. The prompt must make
the trigger and mode impossible to miss.

Prompt fields:

- issue identifier, title, URL, and current state,
- trigger mode,
- trigger comment ID, author, timestamp, URL, and body,
- recent comment context,
- existing issue description,
- existing Workpad summary when available,
- allowed and forbidden actions for the mode.

Prompt sketch:

```text
You are handling a Linear issue comment trigger.

Issue: TOO-123
Current state: Human Review
Trigger mode: answer

Trigger comment:
<comment id, author, timestamp, body>

Recent comment context:
<bounded recent comments>

Rules:
- If mode is answer, do not edit files, create branches, push, or mutate PRs.
- If mode is execute, treat the trigger comment as rework or implementation input.
- If mode is land, keep the run scoped to merge and closeout work.
- Reply in Linear with the result.
- Preserve the single active Workpad as the durable recovery surface when the
  run changes issue status or execution state.
```

For `answer`, the runner should strongly prefer a read-only execution profile.
If a hard read-only sandbox cannot be guaranteed immediately, the prompt must
still state the no-edit rule and the implementation should record this as a
known enforcement boundary in tests and docs.

## Writeback

First version writeback should create a new Linear issue comment, not a threaded
reply.

Comment format:

```md
针对这条评论的答复：

> <short quoted excerpt or comment URL>

<answer>
```

The writeback should avoid duplicating the Workpad for simple answers. Workpad
updates are appropriate when the run changes execution state, records recovery
information, or performs `execute` / `land` work.

Threaded replies can be added later if Linear's comment API support is promoted
into the tracker layer. The initial design favors a reliable issue-comment
writeback path because the repo already has typed create/update comment APIs.

## Concurrency And Safety

Safety rules:

- A single issue must not have two active Symphony runs at the same time.
- A comment-triggered run must respect existing global and per-state concurrency
  limits unless a future config explicitly separates comment limits.
- Symphony-authored comments must be ignored.
- A comment must be claimed before the agent starts.
- A completed comment ID must not be processed again after restart.
- `answer` mode must not modify repository files or PR state.
- `land` mode must not expand into new implementation work.

If a normal issue run is already active, comment-triggered work for that issue is
skipped until a later tick. This avoids injecting a new instruction into a
separate agent while the current one is still running.

## Error Handling

| Failure | Behavior |
| --- | --- |
| Fetch configured-state issues fails | Record an event and retry next tick. |
| Fetch issue comments fails | Record an event and retry next tick. |
| State store unavailable | Fail closed for comment triggers. |
| Comment claim fails because it is already processed | Skip it. |
| Agent run fails | Mark event failed and reuse retry/backoff semantics for the same comment ID. |
| Linear reply write fails | Keep the event failed with `last_error`; retry should not create duplicate answers unless the comment event is claimed again. |
| Comment is edited | Initial version does not retrigger on edit. Store `body_hash` for diagnostics. |
| Multiple new comments exist | Process oldest first; later comments wait for later ticks. |

## Observability

Add structured events for:

- comment trigger discovery start/end,
- issue comment fetch failure,
- comment ignored with reason,
- comment claimed,
- comment run dispatched,
- comment run completed,
- comment run failed.

Operator surfaces should be able to show:

- trigger comment ID,
- mode,
- issue identifier,
- status,
- associated run ID,
- last error.

The first implementation can expose these through the state store and run
timeline. A dedicated operator UI panel is not required for the core loop.

## Testing Plan

Focused tests:

- `internal/config`: parse `comment_triggers`; expand env in `ignore_authors`;
  reject invalid modes, empty states, and invalid limits.
- `internal/tracker/linear`: fetch comments with author ID, URL, body, and
  timestamps; paginate safely; surface GraphQL/status errors.
- `internal/state`: migrate and persist `comment_events`; claim idempotently;
  recover running/failed/completed rows without duplication.
- `internal/orchestrator`: match arbitrary configured states; ignore unmatched
  states; ignore configured authors; process oldest unhandled comment; do not
  dispatch when issue already has a running/retry/suppressed record.
- `internal/agent`: render comment-triggered prompt fields and mode rules.
- fake E2E: fake Linear issue in a configured state with a user comment causes
  exactly one reply/comment run; a second tick does not reprocess it.

Manual validation for real dogfood:

1. Configure an isolated Linear state such as `Waiting for User`.
2. Add a single test issue in that state.
3. Add one user comment.
4. Start Symphony with a dedicated state DB.
5. Verify one comment-triggered run appears in local state.
6. Verify one Linear answer comment is written.
7. Restart Symphony and verify the same user comment is not answered again.

## Rollout Strategy

1. Add config parsing and validation with `enabled=false` by default.
2. Add Linear comment read support with author identity.
3. Add state store comment-event persistence and idempotent claim semantics.
4. Add orchestrator comment discovery behind the feature flag.
5. Add comment-triggered prompt construction and mode enforcement.
6. Add fake E2E coverage.
7. Enable only in local dogfood workflow after deterministic tests pass.

This sequence keeps the existing issue dispatch path stable while introducing
the new comment-driven path behind an explicit workflow opt-in.
