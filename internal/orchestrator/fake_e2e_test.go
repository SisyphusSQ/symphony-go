package orchestrator

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/agent/codex"
	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/hooks"
	"github.com/SisyphusSQ/symphony-go/internal/observability"
	"github.com/SisyphusSQ/symphony-go/internal/tracker/linear"
	"github.com/SisyphusSQ/symphony-go/internal/workspace"
)

func TestFakeE2EProfileCoversCoreLoopWithoutExternalServices(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	terminalWorkspace := filepath.Join(workspaceRoot, "TOO-E2E-TERMINAL")
	if err := os.MkdirAll(terminalWorkspace, 0o755); err != nil {
		t.Fatalf("create terminal workspace fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(terminalWorkspace, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write terminal workspace fixture: %v", err)
	}

	fakeLinear := newFakeE2ELinearServer(t)
	defer fakeLinear.close()

	workflowPath := writeFakeE2EWorkflow(t, fakeLinear.url(), workspaceRoot)
	cfg, err := config.Load(workflowPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	trackerClient, err := linear.NewFromTrackerConfig(cfg.Tracker)
	if err != nil {
		t.Fatalf("linear.NewFromTrackerConfig() error = %v", err)
	}
	workspaceManager, err := workspace.NewManager(cfg.Workspace)
	if err != nil {
		t.Fatalf("workspace.NewManager() error = %v", err)
	}
	clock := newFakeClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC))
	recorder := observability.NewRecorder(observability.WithRecorderClock(clock.Now))

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: workspaceManager,
		Hooks:     hooks.NewRunner(cfg.Hooks),
		Runner: codex.NewRunner(cfg.Codex, codex.WithEnv(
			"SYMPHONY_FAKE_E2E_CODEX=1",
			"SYMPHONY_FAKE_E2E_EXPECT_ENV=present",
		)),
		Logger: recorder,
		Clock:  clock.Now,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	cleanup := runtime.CleanupTerminalWorkspaces(ctx)
	if cleanup.TrackerErr != nil {
		t.Fatalf("startup cleanup tracker error = %v", cleanup.TrackerErr)
	}
	if cleanup.Issues != 1 || len(cleanup.Cleanups) != 1 || !cleanup.Cleanups[0].Removed {
		t.Fatalf("startup cleanup = %#v, want one removed terminal workspace", cleanup)
	}
	if _, err := os.Stat(terminalWorkspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("terminal workspace stat error = %v, want removed", err)
	}

	summary, err := runtime.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if summary.Candidates != 4 {
		t.Fatalf("Candidates = %d, want 4", summary.Candidates)
	}
	if got := issueKeys(summary.Dispatched); got != "TOO-E2E-HOLD,TOO-E2E-SUCCESS,TOO-E2E-FAIL" {
		t.Fatalf("Dispatched = %q, want hold, success, fail", got)
	}
	if !hasSkip(summary.Skipped, "TOO-E2E-BLOCKED", "blocked_by_non_terminal_issue") {
		t.Fatalf("Skipped = %#v, want blocked issue skip", summary.Skipped)
	}

	waitUntilFakeE2E(t, func() bool {
		return runtime.RunningIssueCount() == 1 &&
			runtime.RetryIssueCount() == 1 &&
			runtime.SuppressionIssueCount() == 1
	}, func() string {
		return fmt.Sprintf(
			"running=%d retries=%d suppressions=%d events=%#v",
			runtime.RunningIssueCount(),
			runtime.RetryIssueCount(),
			runtime.SuppressionIssueCount(),
			recorder.Events(),
		)
	})
	requireWorkspaceFile(t, workspaceRoot, "TOO-E2E-SUCCESS", "after_create.txt", "after_create")
	requireWorkspaceFile(t, workspaceRoot, "TOO-E2E-SUCCESS", "before_run.txt", "before_run")
	requireWorkspaceFile(t, workspaceRoot, "TOO-E2E-SUCCESS", "after_run.txt", "after_run")
	requireWorkspaceFile(t, workspaceRoot, "TOO-E2E-FAIL", "after_run.txt", "after_run")
	requireRetryEntry(t, runtime, "TOO-E2E-FAIL", "fake codex failure")
	requireRecordedEvent(t, recorder, observability.EventAgentRunCompleted)
	failed := requireRecordedEvent(t, recorder, observability.EventAgentRunFailed)
	if failed.IssueIdentifier != "TOO-E2E-FAIL" || !strings.Contains(failed.Error, "fake codex failure") {
		t.Fatalf("failed event = %#v, want fake Codex failure for TOO-E2E-FAIL", failed)
	}

	secondSummary, err := runtime.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick(reconcile terminal) error = %v", err)
	}
	waitUntil(t, func() bool {
		return runtime.RunningIssueCount() == 0
	})
	if len(secondSummary.Reconciliation.Stopped) != 1 ||
		secondSummary.Reconciliation.Stopped[0].IssueKey != "TOO-E2E-HOLD" ||
		secondSummary.Reconciliation.Stopped[0].Reason != "terminal_state" ||
		!secondSummary.Reconciliation.Stopped[0].Cleanup.Removed {
		t.Fatalf(
			"second summary = %#v running=%#v counts=%#v, want terminal cleanup for hold issue",
			secondSummary,
			runtime.RunningRecords(),
			fakeLinear.countsSnapshot(),
		)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, "TOO-E2E-HOLD")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hold workspace stat error = %v, want removed", err)
	}

	snapshot := runtime.Snapshot()
	if len(snapshot.ActiveRuns) != 0 || len(snapshot.RetryQueue) != 1 {
		t.Fatalf("snapshot = %#v, want no active runs and one failure retry row", snapshot)
	}
	counts := fakeLinear.countsSnapshot()
	if counts.candidateFetches == 0 || counts.terminalFetches == 0 || counts.stateRefreshes == 0 {
		t.Fatalf("fake Linear counts = %#v, want candidate, terminal, and refresh coverage", counts)
	}
}

func waitUntilFakeE2E(t *testing.T, condition func() bool, debug func() string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not met before timeout: %s", debug())
}

func writeFakeE2EWorkflow(t *testing.T, endpoint string, workspaceRoot string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "WORKFLOW.md")
	content := strings.TrimLeft(fmt.Sprintf(`
---
tracker:
  kind: linear
  endpoint: %q
  api_key: fake-linear-token
  project_slug: fake-project
  active_states:
    - Todo
    - In Progress
    - Rework
  terminal_states:
    - Done
    - Closed
    - Canceled
    - Cancelled
    - Duplicate
polling:
  interval_ms: 5000
workspace:
  root: %q
hooks:
  after_create: |
    printf after_create > after_create.txt
  before_run: |
    printf before_run > before_run.txt
  after_run: |
    printf after_run > after_run.txt
  before_remove: |
    printf before_remove > before_remove.txt
  timeout_ms: 2000
agent:
  max_concurrent_agents: 3
  max_turns: 1
  max_retry_backoff_ms: 10000
codex:
  command: %q
  approval_policy: on-request
  thread_sandbox: workspace-write
  turn_sandbox_policy:
    type: workspaceWrite
  read_timeout_ms: 1000
  turn_timeout_ms: 5000
  stall_timeout_ms: 5000
---

Run {{ issue.identifier }}: {{ issue.title }}
`, endpoint, workspaceRoot, fakeE2ECodexCommand()), "\n")

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fake E2E workflow: %v", err)
	}
	return path
}

func fakeE2ECodexCommand() string {
	return strconv.Quote(os.Args[0]) + " -test.run=TestFakeE2ECodexAppServer --"
}

func requireWorkspaceFile(
	t *testing.T,
	root string,
	issueKey string,
	name string,
	want string,
) {
	t.Helper()

	path := filepath.Join(root, issueKey, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("%s = %q, want %q", path, strings.TrimSpace(string(data)), want)
	}
}

func requireRetryEntry(t *testing.T, runtime *Runtime, issueKey string, errorContains string) {
	t.Helper()

	for _, entry := range runtime.RetryEntries() {
		if entry.IssueKey != issueKey {
			continue
		}
		if errorContains == "" && entry.Error != "" {
			t.Fatalf("retry %s error = %q, want empty", issueKey, entry.Error)
		}
		if errorContains != "" && !strings.Contains(entry.Error, errorContains) {
			t.Fatalf("retry %s error = %q, want containing %q", issueKey, entry.Error, errorContains)
		}
		return
	}
	t.Fatalf("missing retry entry for %s in %#v", issueKey, runtime.RetryEntries())
}

func hasSkip(skipped []SkipSummary, issueKey string, reason string) bool {
	for _, item := range skipped {
		if item.IssueKey == issueKey && item.Reason == reason {
			return true
		}
	}
	return false
}

type fakeE2ELinearServer struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	counts fakeE2ELinearCounts
}

type fakeE2ELinearCounts struct {
	candidateFetches int
	terminalFetches  int
	stateRefreshes   int
}

func newFakeE2ELinearServer(t *testing.T) *fakeE2ELinearServer {
	t.Helper()

	fake := &fakeE2ELinearServer{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveGraphQL))
	return fake
}

func (f *fakeE2ELinearServer) url() string {
	return f.server.URL
}

func (f *fakeE2ELinearServer) close() {
	f.server.Close()
}

func (f *fakeE2ELinearServer) countsSnapshot() fakeE2ELinearCounts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts
}

func (f *fakeE2ELinearServer) serveGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "fake-linear-token" {
		f.t.Fatalf("Authorization = %q, want fake-linear-token", r.Header.Get("Authorization"))
	}

	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatalf("decode fake Linear request: %v", err)
	}

	switch {
	case strings.Contains(req.Query, "RefreshIssueStates"):
		f.recordStateRefresh()
		f.writeIssues(w, []map[string]any{
			f.issueNode("issue-hold", "TOO-E2E-HOLD", "Hold until terminal refresh", "Done", 1, nil),
		})
	case strings.Contains(req.Query, "FetchIssuesByStates"):
		stateNames := stringValues(req.Variables["stateNames"])
		if reflect.DeepEqual(stateNames, []string{"Done", "Closed", "Canceled", "Cancelled", "Duplicate"}) {
			f.recordTerminalFetch()
			f.writeIssues(w, []map[string]any{
				f.issueNode("issue-terminal", "TOO-E2E-TERMINAL", "Terminal cleanup", "Done", 1, nil),
			})
			return
		}
		f.recordCandidateFetch()
		f.writeIssues(w, []map[string]any{
			f.issueNode("issue-hold", "TOO-E2E-HOLD", "Hold until terminal refresh", "Todo", 1, nil),
			f.issueNode("issue-success", "TOO-E2E-SUCCESS", "Happy path", "Todo", 2, nil),
			f.issueNode("issue-fail", "TOO-E2E-FAIL", "Failure path", "Todo", 3, nil),
			f.issueNode("issue-blocked", "TOO-E2E-BLOCKED", "Blocked path", "Todo", 4, []map[string]any{{
				"type": "blocks",
				"issue": map[string]any{
					"id":         "issue-blocker",
					"identifier": "TOO-E2E-BLOCKER",
					"state":      map[string]any{"name": "In Progress"},
				},
			}}),
		})
	default:
		f.t.Fatalf("unexpected fake Linear query: %s", req.Query)
	}
}

func (f *fakeE2ELinearServer) recordCandidateFetch() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts.candidateFetches++
}

func (f *fakeE2ELinearServer) recordTerminalFetch() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts.terminalFetches++
}

func (f *fakeE2ELinearServer) recordStateRefresh() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts.stateRefreshes++
}

func (f *fakeE2ELinearServer) writeIssues(w http.ResponseWriter, nodes []map[string]any) {
	writeJSON := map[string]any{
		"data": map[string]any{
			"issues": map[string]any{
				"nodes":    nodes,
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(writeJSON); err != nil {
		f.t.Fatalf("encode fake Linear response: %v", err)
	}
}

func (f *fakeE2ELinearServer) issueNode(
	id string,
	identifier string,
	title string,
	state string,
	priority int,
	blockers []map[string]any,
) map[string]any {
	return map[string]any{
		"id":          id,
		"identifier":  identifier,
		"title":       title,
		"description": "fake E2E fixture",
		"priority":    priority,
		"branchName":  strings.ToLower(identifier) + "-branch",
		"url":         "https://linear.invalid/" + identifier,
		"createdAt":   "2026-05-03T10:00:00Z",
		"updatedAt":   "2026-05-03T10:00:00Z",
		"state":       map[string]any{"name": state},
		"labels": map[string]any{"nodes": []any{
			map[string]any{"name": "fake-e2e"},
		}},
		"inverseRelations": map[string]any{"nodes": blockers},
	}
}

func stringValues(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, fmt.Sprint(value))
	}
	return result
}

func TestFakeE2ECodexAppServer(t *testing.T) {
	if os.Getenv("SYMPHONY_FAKE_E2E_CODEX") != "1" {
		return
	}
	if err := runFakeE2ECodexAppServer(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

type fakeE2ERPCRequest struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func runFakeE2ECodexAppServer() error {
	if os.Getenv("SYMPHONY_FAKE_E2E_EXPECT_ENV") != "present" {
		return errors.New("missing fake E2E inherited env")
	}

	reader := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	encoder := json.NewEncoder(writer)
	read := func(wantMethod string) (fakeE2ERPCRequest, error) {
		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return fakeE2ERPCRequest{}, err
			}
			return fakeE2ERPCRequest{}, fmt.Errorf("stdin closed before %s", wantMethod)
		}
		var req fakeE2ERPCRequest
		if err := json.Unmarshal(reader.Bytes(), &req); err != nil {
			return fakeE2ERPCRequest{}, err
		}
		if req.Method != wantMethod {
			return fakeE2ERPCRequest{}, fmt.Errorf("method = %s, want %s", req.Method, wantMethod)
		}
		return req, nil
	}
	write := func(value any) error {
		if err := encoder.Encode(value); err != nil {
			return err
		}
		return writer.Flush()
	}

	initialize, err := read("initialize")
	if err != nil {
		return err
	}
	if err := write(map[string]any{
		"id": initialize.ID,
		"result": map[string]any{
			"codexHome":      "/tmp/fake-e2e-codex-home",
			"platformFamily": "unix",
			"platformOs":     "macos",
			"userAgent":      "fake-e2e-codex",
		},
	}); err != nil {
		return err
	}

	threadStart, err := read("thread/start")
	if err != nil {
		return err
	}
	if err := write(map[string]any{
		"id":     threadStart.ID,
		"result": map[string]any{"thread": map[string]any{"id": "thread-fake-e2e"}},
	}); err != nil {
		return err
	}

	turnStart, err := read("turn/start")
	if err != nil {
		return err
	}
	prompt, err := fakeE2ETurnPrompt(turnStart.Params)
	if err != nil {
		return err
	}
	if err := write(map[string]any{
		"id": "3",
		"result": map[string]any{
			"turn": map[string]any{"id": "turn-fake-e2e", "status": "inProgress"},
		},
	}); err != nil {
		return err
	}

	switch {
	case strings.Contains(prompt, "TOO-E2E-HOLD"):
		for {
			time.Sleep(time.Hour)
		}
	case strings.Contains(prompt, "TOO-E2E-FAIL"):
		return fakeE2EWriteFailure(write)
	default:
		return fakeE2EWriteSuccess(write)
	}
}

func fakeE2ETurnPrompt(raw json.RawMessage) (string, error) {
	var params struct {
		Input []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return "", err
	}
	if len(params.Input) != 1 || params.Input[0].Type != "text" {
		return "", fmt.Errorf("turn input = %#v", params.Input)
	}
	return params.Input[0].Text, nil
}

func fakeE2EWriteSuccess(write func(any) error) error {
	if err := fakeE2EWriteUsage(write); err != nil {
		return err
	}
	return write(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{
			"threadId": "thread-fake-e2e",
			"turn":     map[string]any{"id": "turn-fake-e2e", "status": "completed"},
		},
	})
}

func fakeE2EWriteFailure(write func(any) error) error {
	if err := fakeE2EWriteUsage(write); err != nil {
		return err
	}
	return write(map[string]any{
		"method": "error",
		"params": map[string]any{
			"threadId": "thread-fake-e2e",
			"turnId":   "turn-fake-e2e",
			"error":    map[string]any{"message": "fake codex failure"},
		},
	})
}

func fakeE2EWriteUsage(write func(any) error) error {
	return write(map[string]any{
		"method": "thread/tokenUsage/updated",
		"params": map[string]any{
			"threadId": "thread-fake-e2e",
			"turnId":   "turn-fake-e2e",
			"tokenUsage": map[string]any{
				"last":  fakeE2ETokenUsage(1, 2, 3),
				"total": fakeE2ETokenUsage(10, 20, 30),
			},
		},
	})
}

func fakeE2ETokenUsage(input int, output int, total int) map[string]any {
	return map[string]any{
		"cachedInputTokens":     0,
		"inputTokens":           input,
		"outputTokens":          output,
		"reasoningOutputTokens": 0,
		"totalTokens":           total,
	}
}
