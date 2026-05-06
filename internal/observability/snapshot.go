package observability

import "time"

// Snapshot is a read-only operator view of orchestrator runtime state.
type Snapshot struct {
	GeneratedAt    time.Time       `json:"generated_at"`
	LifecycleState string          `json:"lifecycle_state"`
	ActiveRuns     []RunSnapshot   `json:"active_runs"`
	RetryQueue     []RetrySnapshot `json:"retry_queue"`
}

// RunSnapshot is the status row for one active run.
type RunSnapshot struct {
	RunID           string    `json:"run_id,omitempty"`
	IssueID         string    `json:"issue_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	SessionID       string    `json:"session_id,omitempty"`
	State           string    `json:"state,omitempty"`
	RunStatus       string    `json:"run_status"`
	Attempt         int       `json:"attempt"`
	WorkspacePath   string    `json:"workspace_path,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	SecondsRunning  float64   `json:"seconds_running"`
}

// RetrySnapshot is the status row for one queued retry.
type RetrySnapshot struct {
	RunID           string    `json:"run_id,omitempty"`
	IssueID         string    `json:"issue_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	RetryState      string    `json:"retry_state"`
	Attempt         int       `json:"attempt"`
	DueAt           time.Time `json:"due_at"`
	Error           string    `json:"error,omitempty"`
}
