package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
	"github.com/SisyphusSQ/symphony-go/internal/orchestrator"
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
)

func TestHandlerServesStatusRunsReadinessAndMetrics(t *testing.T) {
	runtime := &fakeRuntime{
		status: orchestrator.StatusRunning,
		snapshot: observability.Snapshot{
			GeneratedAt:    time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
			LifecycleState: string(orchestrator.StatusRunning),
			ActiveRuns: []observability.RunSnapshot{{
				IssueID:         "issue-1",
				IssueIdentifier: "TOO-1",
				RunStatus:       observability.RunStatusRunning,
			}},
			RetryQueue: []observability.RetrySnapshot{{
				IssueID:         "issue-2",
				IssueIdentifier: "TOO-2",
				RetryState:      observability.RetryStateFailure,
				Attempt:         2,
			}},
		},
	}
	handler := NewHandler(runtime, Config{Instance: "dev"})

	assertStatus(t, handler, http.MethodGet, "/healthz", http.StatusOK, `"status":"ok"`)
	assertStatus(t, handler, http.MethodGet, "/readyz", http.StatusOK, `"status":"ready"`)
	assertStatus(t, handler, http.MethodGet, "/status", http.StatusOK, `"running":1`)
	assertStatus(t, handler, http.MethodGet, "/runs", http.StatusOK, `"retrying":1`)
	assertStatus(t, handler, http.MethodGet, "/runs/TOO-1", http.StatusOK, `"status":"running"`)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{
		`symphony_runs_active{instance="dev"} 1`,
		`symphony_retry_count{instance="dev"} 1`,
		`symphony_ready{instance="dev"} 1`,
		`symphony_lifecycle_state{instance="dev",state="running"} 1`,
	} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("metrics missing %q:\n%s", want, recorder.Body.String())
		}
	}
}

func TestHandlerControlEndpointsAndErrorSemantics(t *testing.T) {
	runtime := &fakeRuntime{
		status: orchestrator.StatusRunning,
		snapshot: observability.Snapshot{
			GeneratedAt:    time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
			LifecycleState: string(orchestrator.StatusRunning),
		},
	}
	handler := NewHandler(runtime, Config{})

	assertStatus(t, handler, http.MethodPost, "/orchestrator/pause", http.StatusAccepted, `"lifecycle_state":"paused"`)
	if runtime.status != orchestrator.StatusPaused {
		t.Fatalf("status after pause = %q, want paused", runtime.status)
	}
	assertStatus(t, handler, http.MethodGet, "/readyz", http.StatusServiceUnavailable, `"code":"not_ready"`)
	assertStatus(t, handler, http.MethodPost, "/orchestrator/resume", http.StatusAccepted, `"lifecycle_state":"running"`)
	assertStatus(t, handler, http.MethodPost, "/orchestrator/drain", http.StatusAccepted, `"lifecycle_state":"draining"`)

	runtime.status = orchestrator.StatusRunning
	runtime.cancelTargets = map[string]bool{"TOO-1": true}
	assertStatus(t, handler, http.MethodPost, "/runs/TOO-1/cancel", http.StatusAccepted, `"status":"canceled"`)
	if !runtime.canceled["TOO-1"] {
		t.Fatal("cancel endpoint did not call runtime")
	}

	runtime.retryTargets = map[string]bool{"TOO-2": true}
	assertStatus(t, handler, http.MethodPost, "/runs/TOO-2/retry", http.StatusAccepted, `"status":"queued"`)
	if !runtime.retried["TOO-2"] {
		t.Fatal("retry endpoint did not call runtime")
	}

	assertStatus(t, handler, http.MethodGet, "/runs/UNKNOWN", http.StatusNotFound, `"code":"run_not_found"`)
	assertStatus(t, handler, http.MethodPost, "/runs/UNKNOWN/cancel", http.StatusNotFound, `"code":"not_found"`)
	runtime.runningTargets = map[string]bool{"TOO-3": true}
	assertStatus(t, handler, http.MethodPost, "/runs/TOO-3/retry", http.StatusConflict, `"code":"conflict"`)
	assertStatus(t, handler, http.MethodPost, "/runs/TOO-3", http.StatusMethodNotAllowed, `"code":"method_not_allowed"`)
}

func assertStatus(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	wantStatus int,
	wantBody string,
) {
	t.Helper()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	if wantBody != "" && !strings.Contains(recorder.Body.String(), wantBody) {
		t.Fatalf("%s %s body missing %q:\n%s", method, path, wantBody, recorder.Body.String())
	}
}

type fakeRuntime struct {
	status         orchestrator.Status
	snapshot       observability.Snapshot
	readyErr       error
	cancelTargets  map[string]bool
	retryTargets   map[string]bool
	runningTargets map[string]bool
	canceled       map[string]bool
	retried        map[string]bool
}

func (f *fakeRuntime) Status() orchestrator.Status {
	return f.status
}

func (f *fakeRuntime) Snapshot() observability.Snapshot {
	f.snapshot.LifecycleState = string(f.status)
	return f.snapshot
}

func (f *fakeRuntime) DispatchReady() error {
	return f.readyErr
}

func (f *fakeRuntime) Pause() (orchestrator.ControlResult, error) {
	previous := f.status
	f.status = orchestrator.StatusPaused
	return lifecycleResult("pause", previous, f.status), nil
}

func (f *fakeRuntime) Resume() (orchestrator.ControlResult, error) {
	previous := f.status
	f.status = orchestrator.StatusRunning
	return lifecycleResult("resume", previous, f.status), nil
}

func (f *fakeRuntime) Drain() (orchestrator.ControlResult, error) {
	previous := f.status
	f.status = orchestrator.StatusDraining
	return lifecycleResult("drain", previous, f.status), nil
}

func (f *fakeRuntime) CancelRun(_ context.Context, target string) (orchestrator.ControlResult, error) {
	if f.cancelTargets[target] {
		if f.canceled == nil {
			f.canceled = map[string]bool{}
		}
		f.canceled[target] = true
		return orchestrator.ControlResult{Action: "cancel", Target: target, Status: "canceled"}, nil
	}
	return orchestrator.ControlResult{Action: "cancel", Target: target, Status: "not_found", Message: "not found"},
		orchestrator.ErrControlTargetNotFound
}

func (f *fakeRuntime) RetryRun(_ context.Context, target string) (orchestrator.ControlResult, error) {
	if f.runningTargets[target] {
		return orchestrator.ControlResult{Action: "retry", Target: target, Status: "conflict", Message: "running"},
			orchestrator.ErrControlConflict
	}
	if f.retryTargets[target] {
		if f.retried == nil {
			f.retried = map[string]bool{}
		}
		f.retried[target] = true
		return orchestrator.ControlResult{Action: "retry", Target: target, Status: "queued"}, nil
	}
	return orchestrator.ControlResult{Action: "retry", Target: target, Status: "not_found", Message: "not found"},
		orchestrator.ErrControlTargetNotFound
}

func (f *fakeRuntime) CleanupTerminalWorkspaces(context.Context) orchestrator.StartupCleanupSummary {
	if f.readyErr != nil && errors.Is(f.readyErr, orchestrator.ErrMissingDependency) {
		return orchestrator.StartupCleanupSummary{TrackerErr: f.readyErr}
	}
	return orchestrator.StartupCleanupSummary{Issues: 1}
}

func lifecycleResult(action string, previous orchestrator.Status, next orchestrator.Status) orchestrator.ControlResult {
	return orchestrator.ControlResult{
		Action:         action,
		Status:         "changed",
		PreviousState:  string(previous),
		LifecycleState: string(next),
	}
}

func TestRunsPayloadJSONShape(t *testing.T) {
	payload := runsPayload(observability.Snapshot{
		GeneratedAt:    time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
		LifecycleState: string(orchestrator.StatusRunning),
	})
	if payload.Counts["running"] != 0 || payload.Counts["retrying"] != 0 {
		t.Fatalf("payload counts = %#v, want zero running/retrying", payload.Counts)
	}
}

func TestAPIV1StateRunsAndRunDetailWithDurableStore(t *testing.T) {
	ctx := context.Background()
	store, err := runstate.OpenSQLiteStore(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	started := time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC)
	finished := started.Add(2 * time.Minute)
	seedCompletedRun(t, ctx, store, started, finished)

	runtime := &fakeRuntime{
		status: orchestrator.StatusRunning,
		snapshot: observability.Snapshot{
			GeneratedAt:    started.Add(3 * time.Minute),
			LifecycleState: string(orchestrator.StatusRunning),
		},
	}
	handler := NewHandler(runtime, Config{StateStore: store})

	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"configured":true`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"completed":1`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"run_id":"run-completed"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"total_tokens":30`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"remaining":42`)

	assertStatus(
		t,
		handler,
		http.MethodGet,
		"/api/v1/runs?status=completed&issue=TOO-10&limit=1",
		http.StatusOK,
		`"run_id":"run-completed"`,
	)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs/run-completed", http.StatusOK, `"summary":"agent finished"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs/run-completed", http.StatusOK, `"session":{"id":"session-1"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs/missing", http.StatusNotFound, `"code":"run_not_found"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs/run-completed/events", http.StatusNotFound, `"code":"not_found"`)
}

func TestAPIV1NoStateStoreUsesRuntimeSnapshot(t *testing.T) {
	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	runtime := &fakeRuntime{
		status: orchestrator.StatusRunning,
		snapshot: observability.Snapshot{
			GeneratedAt:    now,
			LifecycleState: string(orchestrator.StatusRunning),
			ActiveRuns: []observability.RunSnapshot{{
				RunID:           "run-active",
				IssueID:         "issue-active",
				IssueIdentifier: "TOO-1",
				SessionID:       "session-active",
				RunStatus:       observability.RunStatusRunning,
				Attempt:         1,
				WorkspacePath:   "/tmp/workspaces/TOO-1",
				StartedAt:       now.Add(-time.Minute),
				SecondsRunning:  60,
			}},
			RetryQueue: []observability.RetrySnapshot{{
				RunID:           "run-retry",
				IssueID:         "issue-retry",
				IssueIdentifier: "TOO-2",
				Attempt:         2,
				DueAt:           now.Add(time.Minute),
				Error:           "temporary failure",
			}},
		},
	}
	handler := NewHandler(runtime, Config{})

	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"configured":false`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"running":1`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/state", http.StatusOK, `"retrying":1`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?limit=1", http.StatusOK, `"next_cursor":`)
	cursor := base64.RawURLEncoding.EncodeToString([]byte("offset:1"))
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?limit=1&cursor="+cursor, http.StatusOK, `"run_id":"run-active"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?status=running&issue=TOO-1", http.StatusOK, `"run_id":"run-active"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?status=completed", http.StatusOK, `"rows":[]`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs/run-active", http.StatusOK, `"session":{"id":"session-active"`)
}

func TestAPIV1QueryValidationAndMethodSemantics(t *testing.T) {
	runtime := &fakeRuntime{
		status: orchestrator.StatusRunning,
		snapshot: observability.Snapshot{
			GeneratedAt:    time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC),
			LifecycleState: string(orchestrator.StatusRunning),
		},
	}
	handler := NewHandler(runtime, Config{})

	assertStatus(t, handler, http.MethodPost, "/api/v1/state", http.StatusMethodNotAllowed, `"code":"method_not_allowed"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?status=bogus", http.StatusBadRequest, `"code":"invalid_status"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?limit=0", http.StatusBadRequest, `"code":"invalid_limit"`)
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?cursor=not-base64", http.StatusBadRequest, `"code":"invalid_cursor"`)

	cursor := base64.RawURLEncoding.EncodeToString([]byte("not-offset"))
	assertStatus(t, handler, http.MethodGet, "/api/v1/runs?cursor="+cursor, http.StatusBadRequest, `"code":"invalid_cursor"`)
	assertStatus(t, handler, http.MethodPost, "/api/v1/runs/run-id", http.StatusMethodNotAllowed, `"code":"method_not_allowed"`)
}

func seedCompletedRun(
	t *testing.T,
	ctx context.Context,
	store *runstate.SQLiteStore,
	started time.Time,
	finished time.Time,
) {
	t.Helper()

	run := runstate.Run{
		ID:            "run-completed",
		IssueID:       "issue-completed",
		IssueKey:      "TOO-10",
		Attempt:       1,
		WorkspacePath: "/tmp/workspaces/TOO-10",
		SessionID:     "session-1",
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		StartedAt:     started,
	}
	if err := store.ClaimRun(ctx, run, started.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun() error = %v", err)
	}
	if err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun() error = %v", err)
	}
	if err := store.RecordSession(ctx, runstate.Session{
		ID:            "session-1",
		RunID:         "run-completed",
		IssueID:       "issue-completed",
		IssueKey:      "TOO-10",
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		Status:        "completed",
		Summary:       "done",
		WorkspacePath: "/tmp/workspaces/TOO-10",
		InputTokens:   10,
		OutputTokens:  20,
		TotalTokens:   30,
		TurnCount:     1,
		CreatedAt:     started.Add(time.Second),
		UpdatedAt:     started.Add(time.Second),
	}); err != nil {
		t.Fatalf("RecordSession() error = %v", err)
	}
	if err := store.RecordEvent(ctx, runstate.Event{
		ID:          "event-rate-limit",
		RunID:       "run-completed",
		IssueID:     "issue-completed",
		IssueKey:    "TOO-10",
		SessionID:   "session-1",
		Type:        "agent.rate_limits_updated",
		PayloadJSON: `{"kind":"rate_limits_updated","payload":{"remaining":42}}`,
		CreatedAt:   started.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("RecordEvent(rate limit) error = %v", err)
	}
	if err := store.RecordEvent(ctx, runstate.Event{
		ID:          "event-latest",
		RunID:       "run-completed",
		IssueID:     "issue-completed",
		IssueKey:    "TOO-10",
		SessionID:   "session-1",
		Type:        "agent.run.completed",
		PayloadJSON: `{"message":"agent finished"}`,
		CreatedAt:   started.Add(time.Minute),
	}); err != nil {
		t.Fatalf("RecordEvent(latest) error = %v", err)
	}
	if err := store.CompleteRun(ctx, "run-completed", runstate.RunStatusCompleted, finished, ""); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
}
