export type RunStatus =
  | "running"
  | "retrying"
  | "completed"
  | "failed"
  | "stopped"
  | "interrupted";

export type OperatorDataSource = "api" | "mock";

export interface TokenTotals {
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  cached_tokens: number;
  total_tokens: number;
}

export interface RuntimeTotals {
  total_seconds: number;
}

export interface RateLimitSummary {
  latest: unknown;
  updated_at?: string;
}

export interface EventSummary {
  id: string;
  type: string;
  at: string;
  summary: string;
}

export interface RetryInfo {
  attempt: number;
  due_at: string;
  backoff_ms: number;
  error?: string;
}

export interface RunRow {
  run_id: string;
  issue_id: string;
  issue_identifier: string;
  status: RunStatus | string;
  attempt: number;
  workspace_path?: string;
  session_id?: string;
  session_status?: string;
  session_summary?: string;
  thread_id?: string;
  turn_id?: string;
  started_at: string;
  finished_at?: string;
  runtime_seconds?: number;
  error_summary?: string;
  token_totals: TokenTotals;
  retry?: RetryInfo;
  latest_event?: EventSummary;
}

export interface RunPage {
  rows: RunRow[];
  limit: number;
  next_cursor?: string;
}

export interface StateResponse {
  generated_at: string;
  lifecycle: {
    state: string;
  };
  ready: {
    ok: boolean;
    error?: string;
  };
  counts: Record<string, number>;
  running: RunRow[];
  retrying: RunRow[];
  latest_completed_or_failed: RunRow[];
  tokens: TokenTotals;
  runtime: RuntimeTotals;
  rate_limit: RateLimitSummary;
  state_store: {
    configured: boolean;
  };
}

export interface RunDetail {
  metadata: {
    run_id: string;
    status: string;
    attempt: number;
    started_at: string;
    finished_at?: string;
    runtime_seconds?: number;
  };
  issue: {
    id: string;
    identifier: string;
  };
  workspace: {
    path?: string;
  };
  session: {
    id?: string;
    thread_id?: string;
    turn_id?: string;
    status?: string;
    summary?: string;
  };
  latest_event?: EventSummary;
  token_totals: TokenTotals;
  failure?: {
    error: string;
  };
  retry?: RetryInfo;
}

export type TimelineCategory =
  | "lifecycle"
  | "message"
  | "command"
  | "tool"
  | "diff"
  | "resource"
  | "guardrail"
  | "error";

export type TimelineCategoryFilter = TimelineCategory | "all";

export interface RunTimelineEvent {
  sequence: number;
  id: string;
  at: string;
  category: TimelineCategory | string;
  severity: "debug" | "info" | "warning" | "error" | string;
  title: string;
  summary: string;
  issue_id?: string;
  issue_identifier?: string;
  run_id: string;
  session_id?: string;
  thread_id?: string;
  turn_id?: string;
  duration_ms?: number;
  token_totals?: Partial<TokenTotals>;
  payload: unknown;
}

export interface RunEventPage {
  rows: RunTimelineEvent[];
  limit: number;
  next_cursor?: string;
}

export interface RunEventFilters {
  category: TimelineCategoryFilter;
  limit: number;
}

export interface RunFilters {
  statuses: RunStatus[];
  issue: string;
  limit: number;
}

export interface OperatorResult<T> {
  data: T;
  source: OperatorDataSource;
  fallbackReason?: string;
}

export interface OperatorApiClient {
  getState(): Promise<OperatorResult<StateResponse>>;
  getRuns(filters: RunFilters): Promise<OperatorResult<RunPage>>;
  getRunDetail(runID: string): Promise<OperatorResult<RunDetail>>;
  getRunEvents(runID: string, filters: RunEventFilters): Promise<OperatorResult<RunEventPage>>;
}

export type FallbackMode = "auto" | "always" | "never";

export class OperatorApiError extends Error {
  readonly code: string;
  readonly status?: number;

  constructor(message: string, options: { code?: string; status?: number } = {}) {
    super(message);
    this.name = "OperatorApiError";
    this.code = options.code || "operator_api_error";
    this.status = options.status;
  }
}

interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
  };
}

export interface FetchOperatorApiClientOptions {
  baseURL?: string;
  fallback?: OperatorApiClient;
  fallbackMode?: FallbackMode;
  fetcher?: typeof fetch;
}

const DEFAULT_RUN_LIMIT = 50;
export const DEFAULT_EVENT_LIMIT = 100;

export const defaultRunFilters: RunFilters = {
  statuses: [],
  issue: "",
  limit: DEFAULT_RUN_LIMIT,
};

export const runStatusOptions: RunStatus[] = [
  "running",
  "retrying",
  "completed",
  "failed",
  "stopped",
  "interrupted",
];

export const timelineCategories: TimelineCategory[] = [
  "lifecycle",
  "message",
  "command",
  "tool",
  "diff",
  "resource",
  "guardrail",
  "error",
];

export const defaultRunEventFilters: RunEventFilters = {
  category: "all",
  limit: DEFAULT_EVENT_LIMIT,
};

export function createFetchOperatorApiClient(
  options: FetchOperatorApiClientOptions = {},
): OperatorApiClient {
  const baseURL = normalizeBaseURL(options.baseURL ?? import.meta.env.VITE_OPERATOR_API_BASE ?? "");
  const fallback = options.fallback;
  const fallbackMode = options.fallbackMode ?? import.meta.env.VITE_OPERATOR_MOCK ?? "auto";
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);

  const request = async <T>(path: string, fallbackCall: () => Promise<OperatorResult<T>>) => {
    if (fallbackMode === "always") {
      return fallbackCall();
    }
    try {
      return {
        data: await fetchJSON<T>(joinURL(baseURL, path), fetcher),
        source: "api" as const,
      };
    } catch (error) {
      if (fallbackMode === "auto" && fallback) {
        const result = await fallbackCall();
        return {
          ...result,
          fallbackReason: errorMessage(error),
        };
      }
      throw error;
    }
  };

  return {
    getState() {
      return request("/api/v1/state", () => requiredFallback(fallback).getState());
    },
    getRuns(filters) {
      return request(buildRunsPath(filters), () => requiredFallback(fallback).getRuns(filters));
    },
    getRunDetail(runID) {
      const cleanRunID = runID.trim();
      if (!cleanRunID) {
        throw new OperatorApiError("run id is required", { code: "invalid_run_id" });
      }
      return request(`/api/v1/runs/${encodeURIComponent(cleanRunID)}`, () =>
        requiredFallback(fallback).getRunDetail(cleanRunID),
      );
    },
    getRunEvents(runID, filters) {
      const cleanRunID = runID.trim();
      if (!cleanRunID) {
        throw new OperatorApiError("run id is required", { code: "invalid_run_id" });
      }
      return request(buildRunEventsPath(cleanRunID, filters), () =>
        requiredFallback(fallback).getRunEvents(cleanRunID, filters),
      );
    },
  };
}

export function createMockOperatorApiClient(
  state: StateResponse,
  runs: RunRow[],
  details: Record<string, RunDetail>,
  events: Record<string, RunTimelineEvent[]> = {},
): OperatorApiClient {
  return {
    async getState() {
      return { data: clone(state), source: "mock" };
    },
    async getRuns(filters) {
      const rows = applyRunFilters(runs, filters);
      return {
        data: {
          rows,
          limit: filters.limit || DEFAULT_RUN_LIMIT,
        },
        source: "mock",
      };
    },
    async getRunDetail(runID) {
      const detail = details[runID];
      if (!detail) {
        throw new OperatorApiError("mock run not found", {
          code: "run_not_found",
          status: 404,
        });
      }
      return { data: clone(detail), source: "mock" };
    },
    async getRunEvents(runID, filters) {
      if (!details[runID]) {
        throw new OperatorApiError("mock run not found", {
          code: "run_not_found",
          status: 404,
        });
      }
      const rows = applyRunEventFilters(events[runID] ?? [], filters);
      return {
        data: {
          rows,
          limit: filters.limit || DEFAULT_EVENT_LIMIT,
        },
        source: "mock",
      };
    },
  };
}

export function buildRunsPath(filters: RunFilters): string {
  const values = new URLSearchParams();
  const limit = filters.limit || DEFAULT_RUN_LIMIT;
  values.set("limit", String(limit));
  if (filters.statuses.length > 0) {
    values.set("status", filters.statuses.join(","));
  }
  const issue = filters.issue.trim();
  if (issue) {
    values.set("issue", issue);
  }
  return `/api/v1/runs?${values.toString()}`;
}

export function buildRunEventsPath(runID: string, filters: RunEventFilters): string {
  const cleanRunID = runID.trim();
  if (!cleanRunID) {
    throw new OperatorApiError("run id is required", { code: "invalid_run_id" });
  }
  const values = new URLSearchParams();
  const limit = filters.limit || DEFAULT_EVENT_LIMIT;
  values.set("limit", String(limit));
  if (isTimelineCategory(filters.category)) {
    values.set("category", filters.category);
  }
  return `/api/v1/runs/${encodeURIComponent(cleanRunID)}/events?${values.toString()}`;
}

async function fetchJSON<T>(url: string, fetcher: typeof fetch): Promise<T> {
  const response = await fetcher(url, {
    method: "GET",
    headers: {
      Accept: "application/json",
    },
  });
  const raw = await response.text();
  if (!response.ok) {
    throw decodeHTTPError(response.status, raw);
  }
  try {
    return JSON.parse(raw) as T;
  } catch (error) {
    throw new OperatorApiError(`decode operator API response: ${errorMessage(error)}`, {
      code: "decode_failed",
      status: response.status,
    });
  }
}

function decodeHTTPError(status: number, raw: string): OperatorApiError {
  try {
    const envelope = JSON.parse(raw) as ErrorEnvelope;
    const code = envelope.error?.code || "http_error";
    const message = envelope.error?.message || `operator API returned HTTP ${status}`;
    return new OperatorApiError(`${code}: ${message}`, { code, status });
  } catch {
    return new OperatorApiError(raw.trim() || `operator API returned HTTP ${status}`, {
      code: "http_error",
      status,
    });
  }
}

function requiredFallback(fallback: OperatorApiClient | undefined): OperatorApiClient {
  if (!fallback) {
    throw new OperatorApiError("mock fallback is not configured", { code: "mock_unavailable" });
  }
  return fallback;
}

function applyRunFilters(rows: RunRow[], filters: RunFilters): RunRow[] {
  const issue = filters.issue.trim().toLowerCase();
  const statuses = new Set(filters.statuses);
  const limit = filters.limit || DEFAULT_RUN_LIMIT;
  return rows
    .filter((row) => statuses.size === 0 || statuses.has(row.status as RunStatus))
    .filter((row) => {
      if (!issue) {
        return true;
      }
      return (
        row.issue_id.toLowerCase().includes(issue) ||
        row.issue_identifier.toLowerCase().includes(issue)
      );
    })
    .slice(0, limit)
    .map(clone);
}

function applyRunEventFilters(
  rows: RunTimelineEvent[],
  filters: RunEventFilters,
): RunTimelineEvent[] {
  const limit = filters.limit || DEFAULT_EVENT_LIMIT;
  return rows
    .filter((row) => !isTimelineCategory(filters.category) || row.category === filters.category)
    .slice(0, limit)
    .map(clone);
}

export function isTimelineCategory(value: string): value is TimelineCategory {
  return timelineCategories.includes(value as TimelineCategory);
}

function normalizeBaseURL(baseURL: string): string {
  return baseURL.trim().replace(/\/+$/, "");
}

function joinURL(baseURL: string, path: string): string {
  if (!baseURL) {
    return path;
  }
  return `${baseURL}${path}`;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
