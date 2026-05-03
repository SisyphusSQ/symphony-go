package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

var (
	// ErrControlTargetNotFound marks a control request for an unknown run or retry.
	ErrControlTargetNotFound = errors.New("control_target_not_found")
	// ErrControlConflict marks a control request that conflicts with current run state.
	ErrControlConflict = errors.New("control_conflict")
	// ErrControlUnavailable marks a control request that cannot run in this lifecycle state.
	ErrControlUnavailable = errors.New("control_unavailable")
)

const (
	controlStatusChanged   = "changed"
	controlStatusUnchanged = "unchanged"
	controlStatusCanceled  = "canceled"
	controlStatusQueued    = "queued"
)

// ControlResult is the operator-visible outcome of one runtime control action.
type ControlResult struct {
	Action         string `json:"action"`
	Target         string `json:"target,omitempty"`
	Status         string `json:"status"`
	Message        string `json:"message,omitempty"`
	PreviousState  string `json:"previous_state,omitempty"`
	LifecycleState string `json:"lifecycle_state,omitempty"`
}

// Pause stops dispatching new issue attempts without canceling active runs.
func (r *Runtime) Pause() (ControlResult, error) {
	return r.transitionLifecycle("pause", StatusPaused)
}

// Resume returns a paused or draining runtime to normal dispatch behavior.
func (r *Runtime) Resume() (ControlResult, error) {
	return r.transitionLifecycle("resume", StatusRunning)
}

// Drain stops dispatching new work while allowing active runs to finish.
func (r *Runtime) Drain() (ControlResult, error) {
	return r.transitionLifecycle("drain", StatusDraining)
}

// CancelRun cancels a running attempt or removes a queued retry by issue id or identifier.
func (r *Runtime) CancelRun(ctx context.Context, target string) (ControlResult, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ControlResult{Action: "cancel", Status: "rejected"}, ErrControlTargetNotFound
	}

	r.mu.Lock()
	status := r.status
	if status == StatusStopped {
		r.mu.Unlock()
		result := ControlResult{
			Action:         "cancel",
			Target:         target,
			Status:         "rejected",
			LifecycleState: string(status),
			Message:        "runtime is stopped",
		}
		r.emitControlEvent(ctx, result, ErrControlUnavailable)
		return result, ErrControlUnavailable
	}

	record, ok := findRunRecord(r.state.running, target)
	if ok {
		delete(r.state.running, record.IssueID)
	} else if retry, retryOK := findRetryEntry(r.state.retries, target); retryOK {
		delete(r.state.retries, retry.IssueID)
		r.mu.Unlock()
		r.deleteRetry(ctx, retry.IssueID)
		result := ControlResult{
			Action:         "cancel",
			Target:         target,
			Status:         controlStatusCanceled,
			LifecycleState: string(status),
			Message:        fmt.Sprintf("queued retry for %s canceled", retryTargetLabel(retry)),
		}
		r.emitControlEvent(ctx, result, nil)
		return result, nil
	} else {
		r.mu.Unlock()
		result := ControlResult{
			Action:         "cancel",
			Target:         target,
			Status:         "not_found",
			LifecycleState: string(status),
			Message:        "run or retry not found",
		}
		r.emitControlEvent(ctx, result, ErrControlTargetNotFound)
		return result, ErrControlTargetNotFound
	}
	r.mu.Unlock()

	if record.cancel != nil {
		record.cancel()
	}
	r.persistRunCompletion(ctx, record, runstate.RunStatusStopped, r.deps.Clock(), "operator_cancel")
	result := ControlResult{
		Action:         "cancel",
		Target:         target,
		Status:         controlStatusCanceled,
		LifecycleState: string(status),
		Message:        fmt.Sprintf("running issue %s canceled", runRecordTargetLabel(record)),
	}
	r.emitControlEvent(ctx, result, nil)
	r.emitEvent(ctx, runEvent(
		record.issue(),
		observability.EventOrchestratorRunStopped,
		observability.RunStatusStopped,
		record.Attempt,
		"action=operator_cancel run_status=stopped",
	))
	return result, nil
}

// RetryRun wakes an existing retry row by making it due immediately.
func (r *Runtime) RetryRun(ctx context.Context, target string) (ControlResult, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ControlResult{Action: "retry", Status: "rejected"}, ErrControlTargetNotFound
	}

	r.mu.Lock()
	status := r.status
	if status == StatusStopped {
		r.mu.Unlock()
		result := ControlResult{
			Action:         "retry",
			Target:         target,
			Status:         "rejected",
			LifecycleState: string(status),
			Message:        "runtime is stopped",
		}
		r.emitControlEvent(ctx, result, ErrControlUnavailable)
		return result, ErrControlUnavailable
	}
	if record, ok := findRunRecord(r.state.running, target); ok {
		r.mu.Unlock()
		result := ControlResult{
			Action:         "retry",
			Target:         target,
			Status:         "conflict",
			LifecycleState: string(status),
			Message:        fmt.Sprintf("issue %s is already running", runRecordTargetLabel(record)),
		}
		r.emitControlEvent(ctx, result, ErrControlConflict)
		return result, ErrControlConflict
	}
	entry, ok := findRetryEntry(r.state.retries, target)
	if !ok {
		r.mu.Unlock()
		result := ControlResult{
			Action:         "retry",
			Target:         target,
			Status:         "not_found",
			LifecycleState: string(status),
			Message:        "retry not found",
		}
		r.emitControlEvent(ctx, result, ErrControlTargetNotFound)
		return result, ErrControlTargetNotFound
	}
	entry.DueAt = r.deps.Clock()
	entry.BackoffMS = 0
	r.state.scheduleRetry(entry)
	r.mu.Unlock()

	r.persistRetry(ctx, entry)
	result := ControlResult{
		Action:         "retry",
		Target:         target,
		Status:         controlStatusQueued,
		LifecycleState: string(status),
		Message:        fmt.Sprintf("retry for %s queued immediately", retryTargetLabel(entry)),
	}
	r.emitControlEvent(ctx, result, nil)
	return result, nil
}

func (r *Runtime) transitionLifecycle(action string, next Status) (ControlResult, error) {
	r.mu.Lock()
	previous := r.status
	if previous == StatusStopped {
		r.mu.Unlock()
		result := ControlResult{
			Action:         action,
			Status:         "rejected",
			PreviousState:  string(previous),
			LifecycleState: string(previous),
			Message:        "runtime is stopped",
		}
		r.emitControlEvent(context.Background(), result, ErrControlUnavailable)
		return result, ErrControlUnavailable
	}

	status := controlStatusChanged
	message := fmt.Sprintf("runtime transitioned from %s to %s", previous, next)
	if previous == next {
		status = controlStatusUnchanged
		message = fmt.Sprintf("runtime already %s", next)
	} else {
		r.status = next
	}
	result := ControlResult{
		Action:         action,
		Status:         status,
		PreviousState:  string(previous),
		LifecycleState: string(r.status),
		Message:        message,
	}
	r.mu.Unlock()

	r.emitControlEvent(context.Background(), result, nil)
	return result, nil
}

func (r *Runtime) controlDispatchPaused() (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.status {
	case StatusPaused, StatusDraining:
		return r.status, true
	default:
		return r.status, false
	}
}

func (r *Runtime) emitControlEvent(ctx context.Context, result ControlResult, err error) {
	level := observability.LevelInfo
	errorText := ""
	if err != nil {
		level = observability.LevelWarn
		errorText = err.Error()
	}
	event := observability.Event{
		Level:   level,
		Type:    observability.EventOperatorControl,
		Message: fmt.Sprintf("action=%s status=%s", result.Action, result.Status),
		Error:   errorText,
		Fields: map[string]any{
			"action":          result.Action,
			"target":          result.Target,
			"status":          result.Status,
			"message":         result.Message,
			"previous_state":  result.PreviousState,
			"lifecycle_state": result.LifecycleState,
		},
	}
	r.emitEvent(ctx, event)
}

func findRunRecord(records map[string]RunRecord, target string) (RunRecord, bool) {
	for _, record := range records {
		if targetMatches(record.IssueID, record.IssueKey, target) {
			return record, true
		}
	}
	return RunRecord{}, false
}

func findRetryEntry(entries map[string]runstate.Retry, target string) (runstate.Retry, bool) {
	for _, entry := range entries {
		if targetMatches(entry.IssueID, entry.IssueKey, target) {
			return entry, true
		}
	}
	return runstate.Retry{}, false
}

func targetMatches(issueID string, issueKey string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	return strings.EqualFold(issueID, target) || strings.EqualFold(issueKey, target)
}

func runRecordTargetLabel(record RunRecord) string {
	if record.IssueKey != "" {
		return record.IssueKey
	}
	return record.IssueID
}

func retryTargetLabel(entry runstate.Retry) string {
	if entry.IssueKey != "" {
		return entry.IssueKey
	}
	return entry.IssueID
}

func (record RunRecord) issue() tracker.Issue {
	return tracker.Issue{
		ID:         record.IssueID,
		Identifier: record.IssueKey,
		State:      record.State,
	}
}
