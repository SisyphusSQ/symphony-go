package observability

import (
	"encoding/json"
	"time"
)

const (
	// RunStatusRetrying names a queued retry row in the dashboard API.
	RunStatusRetrying    = "retrying"
	RunStatusInterrupted = "interrupted"
)

// TokenTotals summarizes coding-agent token usage for one or more runs.
type TokenTotals struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

// RuntimeTotals summarizes wall-clock runtime for one or more runs.
type RuntimeTotals struct {
	TotalSeconds float64 `json:"total_seconds"`
}

// RateLimitSummary records the latest persisted rate-limit payload, if any.
type RateLimitSummary struct {
	Latest    json.RawMessage `json:"latest"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
}

// EventSummary is the bounded latest-event shape used by run detail responses.
type EventSummary struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	At      time.Time `json:"at"`
	Summary string    `json:"summary"`
}

// RetryInfo summarizes queued retry state associated with a run.
type RetryInfo struct {
	Attempt   int       `json:"attempt"`
	DueAt     time.Time `json:"due_at"`
	BackoffMS int       `json:"backoff_ms"`
	Error     string    `json:"error,omitempty"`
}

// RunRow is the stable row shape returned by /api/v1 state and runs endpoints.
type RunRow struct {
	RunID           string        `json:"run_id"`
	IssueID         string        `json:"issue_id"`
	IssueIdentifier string        `json:"issue_identifier"`
	Status          string        `json:"status"`
	Attempt         int           `json:"attempt"`
	WorkspacePath   string        `json:"workspace_path,omitempty"`
	SessionID       string        `json:"session_id,omitempty"`
	SessionStatus   string        `json:"session_status,omitempty"`
	SessionSummary  string        `json:"session_summary,omitempty"`
	ThreadID        string        `json:"thread_id,omitempty"`
	TurnID          string        `json:"turn_id,omitempty"`
	StartedAt       time.Time     `json:"started_at"`
	FinishedAt      *time.Time    `json:"finished_at,omitempty"`
	RuntimeSeconds  float64       `json:"runtime_seconds,omitempty"`
	ErrorSummary    string        `json:"error_summary,omitempty"`
	TokenTotals     TokenTotals   `json:"token_totals"`
	Retry           *RetryInfo    `json:"retry,omitempty"`
	LatestEvent     *EventSummary `json:"latest_event,omitempty"`
	SortAt          time.Time     `json:"-"`
}

// RunQuery describes a validated run-list query.
type RunQuery struct {
	Statuses []string
	Issue    string
	Limit    int
	Offset   int
}

// RunPage is a stable paginated run-list response.
type RunPage struct {
	Rows       []RunRow `json:"rows"`
	Limit      int      `json:"limit"`
	NextCursor string   `json:"next_cursor,omitempty"`
	HasMore    bool     `json:"-"`
}

const (
	TimelineCategoryLifecycle = "lifecycle"
	TimelineCategoryMessage   = "message"
	TimelineCategoryCommand   = "command"
	TimelineCategoryTool      = "tool"
	TimelineCategoryDiff      = "diff"
	TimelineCategoryResource  = "resource"
	TimelineCategoryGuardrail = "guardrail"
	TimelineCategoryError     = "error"
)

// TimelineCategories lists the stable event categories accepted by the
// operator timeline API.
var TimelineCategories = []string{
	TimelineCategoryLifecycle,
	TimelineCategoryMessage,
	TimelineCategoryCommand,
	TimelineCategoryTool,
	TimelineCategoryDiff,
	TimelineCategoryResource,
	TimelineCategoryGuardrail,
	TimelineCategoryError,
}

// TimelineQuery describes a validated run event timeline query.
type TimelineQuery struct {
	Category string
	Limit    int
	Offset   int
}

// TimelinePage is a stable paginated event timeline response.
type TimelinePage struct {
	Rows       []TimelineEventRow `json:"rows"`
	Limit      int                `json:"limit"`
	NextCursor string             `json:"next_cursor,omitempty"`
	HasMore    bool               `json:"-"`
}

// TimelineEventRow is one redacted, humanized event returned by the timeline API.
type TimelineEventRow struct {
	Sequence        int             `json:"sequence"`
	ID              string          `json:"id"`
	At              time.Time       `json:"at"`
	Category        string          `json:"category"`
	Severity        string          `json:"severity"`
	Title           string          `json:"title"`
	Summary         string          `json:"summary"`
	IssueID         string          `json:"issue_id"`
	IssueIdentifier string          `json:"issue_identifier"`
	RunID           string          `json:"run_id"`
	SessionID       string          `json:"session_id,omitempty"`
	ThreadID        string          `json:"thread_id,omitempty"`
	TurnID          string          `json:"turn_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

// RawTimelineEvent is the durable event read model projected into TimelineEventRow.
type RawTimelineEvent struct {
	ID              string
	RunID           string
	IssueID         string
	IssueIdentifier string
	SessionID       string
	ThreadID        string
	TurnID          string
	Type            string
	PayloadJSON     string
	At              time.Time
}

// TimelineRedactor is the subset of safety.Redactor used by timeline projection.
type TimelineRedactor interface {
	String(string) string
	JSON(string) string
}

// StoreSummary is the durable-state contribution to /api/v1/state.
type StoreSummary struct {
	Counts                  map[string]int
	LatestCompletedOrFailed []RunRow
	Tokens                  TokenTotals
	Runtime                 RuntimeTotals
	RateLimit               RateLimitSummary
}

// RunMetadata is the run-level metadata section for /api/v1/runs/{run_id}.
type RunMetadata struct {
	RunID          string     `json:"run_id"`
	Status         string     `json:"status"`
	Attempt        int        `json:"attempt"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	RuntimeSeconds float64    `json:"runtime_seconds,omitempty"`
}

// IssueIdentity is the issue section for /api/v1/runs/{run_id}.
type IssueIdentity struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
}

// WorkspaceSummary is the workspace section for /api/v1/runs/{run_id}.
type WorkspaceSummary struct {
	Path string `json:"path,omitempty"`
}

// SessionSummary is the session section for /api/v1/runs/{run_id}.
type SessionSummary struct {
	ID       string `json:"id,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
	TurnID   string `json:"turn_id,omitempty"`
	Status   string `json:"status,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

// FailureSummary records terminal failure information when present.
type FailureSummary struct {
	Error string `json:"error"`
}

// RunDetail is the stable response for /api/v1/runs/{run_id}.
type RunDetail struct {
	Metadata    RunMetadata      `json:"metadata"`
	Issue       IssueIdentity    `json:"issue"`
	Workspace   WorkspaceSummary `json:"workspace"`
	Session     SessionSummary   `json:"session"`
	LatestEvent *EventSummary    `json:"latest_event,omitempty"`
	TokenTotals TokenTotals      `json:"token_totals"`
	Failure     *FailureSummary  `json:"failure,omitempty"`
	Retry       *RetryInfo       `json:"retry,omitempty"`
}
