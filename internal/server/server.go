// Package server exposes local operator HTTP endpoints for one Symphony runtime.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
	"github.com/SisyphusSQ/symphony-go/internal/orchestrator"
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
)

const (
	// DefaultBindHost keeps the optional HTTP server local unless explicitly changed later.
	DefaultBindHost = "127.0.0.1"

	contentTypeJSON = "application/json; charset=utf-8"
	contentTypeText = "text/plain; version=0.0.4; charset=utf-8"
)

// Config describes the local operator server.
type Config struct {
	BindHost   string
	Port       int
	Instance   string
	StateStore runstate.QueryStore
}

// Runtime is the orchestrator surface needed by the operator HTTP API.
type Runtime interface {
	Status() orchestrator.Status
	Snapshot() observability.Snapshot
	DispatchReady() error
	Pause() (orchestrator.ControlResult, error)
	Resume() (orchestrator.ControlResult, error)
	Drain() (orchestrator.ControlResult, error)
	CancelRun(context.Context, string) (orchestrator.ControlResult, error)
	RetryRun(context.Context, string) (orchestrator.ControlResult, error)
	CleanupTerminalWorkspaces(context.Context) orchestrator.StartupCleanupSummary
}

type handler struct {
	runtime Runtime
	config  Config
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error responseError `json:"error"`
}

type runsResponse struct {
	GeneratedAt    string                        `json:"generated_at"`
	LifecycleState string                        `json:"lifecycle_state"`
	Counts         map[string]int                `json:"counts"`
	Running        []observability.RunSnapshot   `json:"running"`
	Retrying       []observability.RetrySnapshot `json:"retrying"`
}

type runDetailResponse struct {
	IssueID         string                       `json:"issue_id"`
	IssueIdentifier string                       `json:"issue_identifier"`
	Status          string                       `json:"status"`
	Running         *observability.RunSnapshot   `json:"running,omitempty"`
	Retry           *observability.RetrySnapshot `json:"retry,omitempty"`
}

// NewHandler creates a stdlib HTTP handler for local operator endpoints.
func NewHandler(runtime Runtime, cfg Config) http.Handler {
	if cfg.BindHost == "" {
		cfg.BindHost = DefaultBindHost
	}
	mux := http.NewServeMux()
	h := &handler{runtime: runtime, config: cfg}
	mux.HandleFunc("/", h.handleRoot)
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/readyz", h.handleReadyz)
	mux.HandleFunc("/metrics", h.handleMetrics)
	mux.HandleFunc("/status", h.handleStatus)
	mux.HandleFunc("/doctor", h.handleDoctor)
	mux.HandleFunc("/runs", h.handleRuns)
	mux.HandleFunc("/runs/", h.handleRunPath)
	mux.HandleFunc("/api/v1/state", h.handleAPIState)
	mux.HandleFunc("/api/v1/runs", h.handleAPIRuns)
	mux.HandleFunc("/api/v1/runs/", h.handleAPIRunPath)
	mux.HandleFunc("/orchestrator/pause", h.handleLifecycleControl("pause"))
	mux.HandleFunc("/orchestrator/resume", h.handleLifecycleControl("resume"))
	mux.HandleFunc("/orchestrator/drain", h.handleLifecycleControl("drain"))
	mux.HandleFunc("/orchestrator/cleanup", h.handleCleanup)
	return mux
}

func (h *handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "symphony",
		"status":  h.lifecycleState(),
		"routes": []string{
			"/healthz",
			"/readyz",
			"/metrics",
			"/runs",
			"/status",
			"/doctor",
		},
	})
}

func (h *handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"lifecycle_state": h.lifecycleState(),
	})
}

func (h *handler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	ready, err := h.ready()
	if ready {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "ready",
			"lifecycle_state": h.lifecycleState(),
		})
		return
	}
	message := "runtime is not ready"
	if err != nil {
		message = err.Error()
	}
	writeError(w, http.StatusServiceUnavailable, "not_ready", message)
}

func (h *handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	w.Header().Set("Content-Type", contentTypeText)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.renderMetrics()))
}

func (h *handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	snapshot := h.runtime.Snapshot()
	ready, readyErr := h.ready()
	payload := runsPayload(snapshot)
	writeJSON(w, http.StatusOK, map[string]any{
		"lifecycle_state": snapshot.LifecycleState,
		"ready":           ready,
		"ready_error":     errorString(readyErr),
		"counts":          payload.Counts,
		"generated_at":    payload.GeneratedAt,
	})
}

func (h *handler) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	ready, readyErr := h.ready()
	status := "ok"
	if !ready {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          status,
		"dispatch_ready":  ready,
		"dispatch_error":  errorString(readyErr),
		"lifecycle_state": h.lifecycleState(),
		"bind_host":       h.config.BindHost,
		"port":            h.config.Port,
	})
}

func (h *handler) handleRuns(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, runsPayload(h.runtime.Snapshot()))
}

func (h *handler) handleRunPath(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/runs/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "not_found", "run id is required")
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) > 2 {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}

	target, err := pathUnescape(parts[0])
	if err != nil || strings.TrimSpace(target) == "" {
		writeError(w, http.StatusBadRequest, "invalid_target", "run id is invalid")
		return
	}
	if len(parts) == 1 {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		detail, ok := findRunDetail(h.runtime.Snapshot(), target)
		if !ok {
			writeError(w, http.StatusNotFound, "run_not_found", "run or retry not found")
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	switch parts[1] {
	case "cancel":
		result, err := h.runtime.CancelRun(r.Context(), target)
		writeControlResult(w, result, err)
	case "retry":
		result, err := h.runtime.RetryRun(r.Context(), target)
		writeControlResult(w, result, err)
	default:
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

func (h *handler) handleLifecycleControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		if h.runtime == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
			return
		}
		var (
			result orchestrator.ControlResult
			err    error
		)
		switch action {
		case "pause":
			result, err = h.runtime.Pause()
		case "resume":
			result, err = h.runtime.Resume()
		case "drain":
			result, err = h.runtime.Drain()
		default:
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		writeControlResult(w, result, err)
	}
}

func (h *handler) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	if terminal := strings.TrimSpace(r.URL.Query().Get("terminal")); terminal != "" && terminal != "true" {
		writeError(w, http.StatusBadRequest, "unsupported_cleanup", "only terminal cleanup is supported")
		return
	}
	summary := h.runtime.CleanupTerminalWorkspaces(r.Context())
	status := http.StatusAccepted
	if summary.TrackerErr != nil {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"action":      "cleanup",
		"scope":       "terminal",
		"issues":      summary.Issues,
		"cleanups":    summary.Cleanups,
		"tracker_err": errorString(summary.TrackerErr),
	})
}

func (h *handler) ready() (bool, error) {
	if h.runtime == nil {
		return false, errors.New("runtime is unavailable")
	}
	status := h.runtime.Status()
	if status != orchestrator.StatusRunning {
		return false, fmt.Errorf("runtime lifecycle_state=%s", status)
	}
	if err := h.runtime.DispatchReady(); err != nil {
		return false, err
	}
	return true, nil
}

func (h *handler) lifecycleState() string {
	if h.runtime == nil {
		return "unavailable"
	}
	return string(h.runtime.Status())
}

func (h *handler) renderMetrics() string {
	snapshot := h.runtime.Snapshot()
	instance := h.config.Instance
	if instance == "" {
		instance = "default"
	}
	labels := fmt.Sprintf(`instance=%q`, escapeLabel(instance))
	lines := []string{
		"# HELP symphony_runs_active Active Symphony issue runs.",
		"# TYPE symphony_runs_active gauge",
		fmt.Sprintf("symphony_runs_active{%s} %d", labels, len(snapshot.ActiveRuns)),
		"# HELP symphony_retry_count Queued Symphony retries.",
		"# TYPE symphony_retry_count gauge",
		fmt.Sprintf("symphony_retry_count{%s} %d", labels, len(snapshot.RetryQueue)),
		"# HELP symphony_ready Runtime readiness.",
		"# TYPE symphony_ready gauge",
		fmt.Sprintf("symphony_ready{%s} %d", labels, boolMetric(h.ready())),
		"# HELP symphony_lifecycle_state Runtime lifecycle state.",
		"# TYPE symphony_lifecycle_state gauge",
	}
	for _, state := range []orchestrator.Status{
		orchestrator.StatusStarting,
		orchestrator.StatusRunning,
		orchestrator.StatusPaused,
		orchestrator.StatusDraining,
		orchestrator.StatusStopped,
	} {
		value := 0
		if snapshot.LifecycleState == string(state) {
			value = 1
		}
		lines = append(lines, fmt.Sprintf(
			"symphony_lifecycle_state{%s,state=%q} %d",
			labels,
			escapeLabel(string(state)),
			value,
		))
	}
	return strings.Join(lines, "\n") + "\n"
}

func runsPayload(snapshot observability.Snapshot) runsResponse {
	return runsResponse{
		GeneratedAt:    snapshot.GeneratedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		LifecycleState: snapshot.LifecycleState,
		Counts: map[string]int{
			"running":  len(snapshot.ActiveRuns),
			"retrying": len(snapshot.RetryQueue),
		},
		Running:  snapshot.ActiveRuns,
		Retrying: snapshot.RetryQueue,
	}
}

func findRunDetail(snapshot observability.Snapshot, target string) (runDetailResponse, bool) {
	for _, run := range snapshot.ActiveRuns {
		if targetMatches(run.IssueID, run.IssueIdentifier, target) {
			runCopy := run
			return runDetailResponse{
				IssueID:         run.IssueID,
				IssueIdentifier: run.IssueIdentifier,
				Status:          "running",
				Running:         &runCopy,
			}, true
		}
	}
	for _, retry := range snapshot.RetryQueue {
		if targetMatches(retry.IssueID, retry.IssueIdentifier, target) {
			retryCopy := retry
			return runDetailResponse{
				IssueID:         retry.IssueID,
				IssueIdentifier: retry.IssueIdentifier,
				Status:          "retrying",
				Retry:           &retryCopy,
			}, true
		}
	}
	return runDetailResponse{}, false
}

func writeControlResult(w http.ResponseWriter, result orchestrator.ControlResult, err error) {
	if err == nil {
		writeJSON(w, http.StatusAccepted, result)
		return
	}
	switch {
	case errors.Is(err, orchestrator.ErrControlTargetNotFound):
		writeError(w, http.StatusNotFound, result.Status, result.Message)
	case errors.Is(err, orchestrator.ErrControlConflict):
		writeError(w, http.StatusConflict, result.Status, result.Message)
	case errors.Is(err, orchestrator.ErrControlUnavailable):
		writeError(w, http.StatusConflict, result.Status, result.Message)
	default:
		writeError(w, http.StatusInternalServerError, "control_failed", err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	if code == "" {
		code = "error"
	}
	if message == "" {
		message = code
	}
	writeJSON(w, status, errorEnvelope{Error: responseError{Code: code, Message: message}})
}

func allowMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	return false
}

func pathUnescape(value string) (string, error) {
	return url.PathUnescape(value)
}

func targetMatches(issueID string, issueKey string, target string) bool {
	target = strings.TrimSpace(target)
	return target != "" &&
		(strings.EqualFold(issueID, target) || strings.EqualFold(issueKey, target))
}

func boolMetric(value bool, _ error) int {
	if value {
		return 1
	}
	return 0
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
