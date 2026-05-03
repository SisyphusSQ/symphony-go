package observability

import "time"

// Level names the severity of an operator-visible event.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// EventType names shared event classes emitted by runtime components.
type EventType string

const (
	EventOrchestratorDispatchSkipped EventType = "orchestrator.dispatch.skipped"
	EventOrchestratorRunDispatched   EventType = "orchestrator.run.dispatched"
	EventOrchestratorRunStarted      EventType = "orchestrator.run.started"
	EventOrchestratorRunCompleted    EventType = "orchestrator.run.completed"
	EventOrchestratorRunFailed       EventType = "orchestrator.run.failed"
	EventOrchestratorRunStopped      EventType = "orchestrator.run.stopped"
	EventOrchestratorMissingDeps     EventType = "orchestrator.dependencies.missing"

	EventTrackerCandidateFetchFailed EventType = "tracker.candidates.fetch_failed"
	EventTrackerStateRefreshFailed   EventType = "tracker.states.refresh_failed"
	EventTrackerTerminalFetchFailed  EventType = "tracker.terminal.fetch_failed"

	EventWorkspacePrepared      EventType = "workspace.prepared"
	EventWorkspacePrepareFailed EventType = "workspace.prepare.failed"
	EventWorkspaceCleanup       EventType = "workspace.cleanup"
	EventWorkspaceCleanupFailed EventType = "workspace.cleanup.failed"

	EventHookCompleted EventType = "hooks.completed"
	EventHookFailed    EventType = "hooks.failed"

	EventAgentRunCompleted EventType = "agent.run.completed"
	EventAgentRunFailed    EventType = "agent.run.failed"

	EventRetryScheduled  EventType = "retry.scheduled"
	EventRetryDispatched EventType = "retry.dispatched"
	EventRetryRequeued   EventType = "retry.requeued"
	EventRetryReleased   EventType = "retry.released"
)

const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusStopped   = "stopped"
	RunStatusSkipped   = "skipped"

	RetryStateContinuation = "continuation"
	RetryStateFailure      = "failure"
	RetryStateRequeued     = "requeued"
	RetryStateReleased     = "released"
)

// Event is the structured log unit shared by orchestration packages.
type Event struct {
	Time            time.Time      `json:"time"`
	Level           Level          `json:"level"`
	Type            EventType      `json:"event_type"`
	Message         string         `json:"message,omitempty"`
	IssueID         string         `json:"issue_id,omitempty"`
	IssueIdentifier string         `json:"issue_identifier,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	RunStatus       string         `json:"run_status,omitempty"`
	RetryState      string         `json:"retry_state,omitempty"`
	RetryAttempt    int            `json:"retry_attempt,omitempty"`
	RetryDueAt      *time.Time     `json:"retry_due_at,omitempty"`
	Error           string         `json:"error,omitempty"`
	Fields          map[string]any `json:"fields,omitempty"`
}

func (event Event) normalize(now time.Time) Event {
	if event.Time.IsZero() {
		event.Time = now
	}
	if event.Level == "" {
		event.Level = LevelInfo
	}
	if event.Fields != nil && len(event.Fields) == 0 {
		event.Fields = nil
	}
	return event
}

// RetryStateForError classifies a retry row without coupling callers to the
// internal retry queue representation.
func RetryStateForError(errText string) string {
	if errText == "" {
		return RetryStateContinuation
	}
	return RetryStateFailure
}
