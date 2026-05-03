package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
	"github.com/SisyphusSQ/symphony-go/internal/orchestrator"
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
