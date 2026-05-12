package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidStoreRequest marks a store request that cannot be persisted.
	ErrInvalidStoreRequest = errors.New("invalid_state_store_request")
	// ErrRunClaimed marks an issue that already has an active durable claim.
	ErrRunClaimed = errors.New("state_run_claimed")
)

// RunStatus names durable execution states persisted for one issue attempt.
type RunStatus string

const (
	RunStatusRunning     RunStatus = "running"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusStopped     RunStatus = "stopped"
	RunStatusInterrupted RunStatus = "interrupted"

	RestartRecoveryError = "restart_recovery_interrupted_run"
)

// Store persists the local orchestration state needed for crash recovery.
type Store interface {
	RecoverStartup(ctx context.Context, now time.Time) (RecoverySnapshot, error)
	ClaimRun(ctx context.Context, run Run, leaseExpiresAt time.Time) error
	UpdateRun(ctx context.Context, run Run) error
	CompleteRun(ctx context.Context, runID string, status RunStatus, finishedAt time.Time, errText string) error
	UpsertRetry(ctx context.Context, retry Retry) error
	DeleteRetry(ctx context.Context, issueID string) error
	UpsertSuppression(ctx context.Context, suppression Suppression) error
	DeleteSuppression(ctx context.Context, issueID string) error
	RecordSession(ctx context.Context, session Session) error
	RecordEvent(ctx context.Context, event Event) error
}

// RecoverySnapshot records durable state loaded during process startup.
type RecoverySnapshot struct {
	InterruptedRuns []Run
	Retries         []Retry
	Suppressions    []Suppression
}

// Run records one issue execution attempt.
type Run struct {
	ID             string
	InstanceID     string
	IssueID        string
	IssueKey       string
	Status         RunStatus
	Attempt        int
	WorkspacePath  string
	SessionID      string
	ThreadID       string
	TurnID         string
	WorkflowRef    string
	ClaimedBy      string
	LeaseExpiresAt *time.Time
	Error          string
	StartedAt      time.Time
	FinishedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Retry records one retry queue entry.
type Retry struct {
	RunID     string
	IssueID   string
	IssueKey  string
	Attempt   int
	DueAt     time.Time
	BackoffMS int
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Suppression records an issue-local terminal outcome that should not be
// redispatched while the tracker keeps the issue in the same active state.
type Suppression struct {
	IssueID   string
	IssueKey  string
	State     string
	RunID     string
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Session records the latest coding-agent session metadata for one run.
type Session struct {
	ID              string
	RunID           string
	IssueID         string
	IssueKey        string
	ThreadID        string
	TurnID          string
	Status          string
	Summary         string
	WorkspacePath   string
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	TotalTokens     int64
	CachedTokens    int64
	TurnCount       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Event records one durable event payload for crash inspection.
type Event struct {
	ID          string
	RunID       string
	IssueID     string
	IssueKey    string
	SessionID   string
	Type        string
	PayloadJSON string
	CreatedAt   time.Time
}

// NewID returns a random text id with the given prefix.
func NewID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(raw[:]))
	}
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
