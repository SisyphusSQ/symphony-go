import {
  createMockOperatorApiClient,
  type RunDetail,
  type RunRow,
  type StateResponse,
  type TokenTotals,
} from "../api/operator";

const generatedAt = "2026-05-06T08:40:00Z";

const zeroTokens: TokenTotals = {
  input_tokens: 0,
  output_tokens: 0,
  reasoning_tokens: 0,
  cached_tokens: 0,
  total_tokens: 0,
};

export const mockRuns: RunRow[] = [
  {
    run_id: "run-too-142-active",
    issue_id: "38889372-4017-4d44-9d75-3f0b413ecd9d",
    issue_identifier: "TOO-142",
    status: "running",
    attempt: 1,
    workspace_path: "/workspace/symphony-go/TOO-142",
    session_id: "sess_142",
    session_status: "active",
    session_summary: "Scaffolding operator UI shell",
    thread_id: "thread_142",
    turn_id: "turn_3",
    started_at: "2026-05-06T08:21:10Z",
    runtime_seconds: 1130,
    token_totals: {
      input_tokens: 48210,
      output_tokens: 11940,
      reasoning_tokens: 3210,
      cached_tokens: 8800,
      total_tokens: 63360,
    },
    latest_event: {
      id: "evt-142-3",
      type: "command",
      at: "2026-05-06T08:39:20Z",
      summary: "npm test running for dashboard shell",
    },
  },
  {
    run_id: "run-too-141-completed",
    issue_id: "issue-too-141",
    issue_identifier: "TOO-141",
    status: "completed",
    attempt: 1,
    workspace_path: "/workspace/symphony-go/TOO-141",
    session_id: "sess_141",
    session_status: "completed",
    session_summary: "TUI status and run detail landed",
    thread_id: "thread_141",
    started_at: "2026-05-06T06:11:04Z",
    finished_at: "2026-05-06T07:33:18Z",
    runtime_seconds: 4934,
    token_totals: {
      input_tokens: 38900,
      output_tokens: 9100,
      reasoning_tokens: 2600,
      cached_tokens: 6400,
      total_tokens: 50600,
    },
  },
  {
    run_id: "run-too-143-retry",
    issue_id: "issue-too-143",
    issue_identifier: "TOO-143",
    status: "retrying",
    attempt: 2,
    started_at: "2026-05-06T09:12:00Z",
    error_summary: "waiting for Web run detail scope",
    token_totals: zeroTokens,
    retry: {
      attempt: 2,
      due_at: "2026-05-06T09:12:00Z",
      backoff_ms: 300000,
      error: "blocked by current shell delivery",
    },
  },
  {
    run_id: "run-too-140-failed",
    issue_id: "issue-too-140",
    issue_identifier: "TOO-140",
    status: "failed",
    attempt: 1,
    started_at: "2026-05-05T21:10:00Z",
    finished_at: "2026-05-05T21:48:12Z",
    runtime_seconds: 2292,
    error_summary: "review gate rejected stale plan metadata",
    token_totals: {
      input_tokens: 21000,
      output_tokens: 4300,
      reasoning_tokens: 900,
      cached_tokens: 3200,
      total_tokens: 26200,
    },
  },
];

export const mockState: StateResponse = {
  generated_at: generatedAt,
  lifecycle: {
    state: "running",
  },
  ready: {
    ok: true,
  },
  counts: {
    running: 1,
    retrying: 1,
    completed: 1,
    failed: 1,
    stopped: 0,
    interrupted: 0,
  },
  running: mockRuns.filter((run) => run.status === "running"),
  retrying: mockRuns.filter((run) => run.status === "retrying"),
  latest_completed_or_failed: mockRuns.filter((run) => ["completed", "failed"].includes(run.status)),
  tokens: {
    input_tokens: 108110,
    output_tokens: 25340,
    reasoning_tokens: 6710,
    cached_tokens: 18400,
    total_tokens: 140160,
  },
  runtime: {
    total_seconds: 8356,
  },
  rate_limit: {
    latest: {
      primary_remaining: 4820,
      reset_seconds: 1200,
    },
    updated_at: "2026-05-06T08:35:44Z",
  },
  state_store: {
    configured: true,
  },
};

export const mockRunDetails: Record<string, RunDetail> = Object.fromEntries(
  mockRuns.map((run) => [
    run.run_id,
    {
      metadata: {
        run_id: run.run_id,
        status: run.status,
        attempt: run.attempt,
        started_at: run.started_at,
        finished_at: run.finished_at,
        runtime_seconds: run.runtime_seconds,
      },
      issue: {
        id: run.issue_id,
        identifier: run.issue_identifier,
      },
      workspace: {
        path: run.workspace_path,
      },
      session: {
        id: run.session_id,
        thread_id: run.thread_id,
        turn_id: run.turn_id,
        status: run.session_status,
        summary: run.session_summary,
      },
      latest_event: run.latest_event,
      token_totals: run.token_totals,
      failure: run.error_summary ? { error: run.error_summary } : undefined,
      retry: run.retry,
    },
  ]),
);

export const mockOperatorApiClient = createMockOperatorApiClient(
  mockState,
  mockRuns,
  mockRunDetails,
);
