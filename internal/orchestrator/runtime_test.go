package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/agent"
	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/hooks"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
	"github.com/SisyphusSQ/symphony-go/internal/workspace"
)

func TestRuntimeReloadPreservesActiveStateAndUpdatesFutureDispatchConfig(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Old prompt")

	runtime, err := NewRuntime(workflowPath)
	if err != nil {
		t.Fatalf("NewRuntime() returned error: %v", err)
	}
	runtime.MarkActive("issue-1")

	before := runtime.FutureDispatchConfig()
	if before.PromptBody != "Old prompt" {
		t.Fatalf("initial PromptBody = %q, want Old prompt", before.PromptBody)
	}

	writeRuntimeWorkflowAt(t, workflowPath, 11000, 5, "New prompt")

	result := runtime.ReloadWorkflowIfChanged()
	if result.Status != config.ReloadApplied {
		t.Fatalf("Status = %q, want %q; err=%v", result.Status, config.ReloadApplied, result.Err)
	}
	if runtime.ActiveIssueCount() != 1 {
		t.Fatalf("ActiveIssueCount() = %d, want 1", runtime.ActiveIssueCount())
	}

	future := runtime.FutureDispatchConfig()
	if future.Polling.Interval != 11*time.Second {
		t.Fatalf("future Polling.Interval = %s, want 11s", future.Polling.Interval)
	}
	if future.Agent.MaxConcurrentAgents != 5 {
		t.Fatalf("future MaxConcurrentAgents = %d, want 5", future.Agent.MaxConcurrentAgents)
	}
	if future.PromptBody != "New prompt" {
		t.Fatalf("future PromptBody = %q, want New prompt", future.PromptBody)
	}
}

func TestRuntimeTickDispatchesEligibleIssueThroughWorkspaceHooksAndRunner(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Dispatch prompt")
	workspace := &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true}
	hookRunner := &fakeHookRunner{}
	runner := newFakeAgentRunner(nil)

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker: &fakeTrackerClient{candidates: []tracker.Issue{
			runtimeIssue("TOO-1", "Todo"),
			withRuntimeBlockers(runtimeIssue("TOO-2", "Todo"), tracker.BlockerRef{
				ID:         "blocker-id",
				Identifier: "TOO-0",
				State:      "In Progress",
			}),
		}},
		Workspace: workspace,
		Hooks:     hookRunner,
		Runner:    runner,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(summary.Dispatched) != 1 || summary.Dispatched[0].IssueKey != "TOO-1" {
		t.Fatalf("Dispatched = %#v, want only TOO-1", summary.Dispatched)
	}
	if len(summary.Skipped) != 1 || summary.Skipped[0].Reason != "blocked_by_non_terminal_issue" {
		t.Fatalf("Skipped = %#v, want blocked TOO-2", summary.Skipped)
	}

	waitUntil(t, func() bool {
		return runner.callCount() == 1 && hookRunner.callNamesJoined() == "after_create,before_run,after_run"
	})
	waitUntil(t, func() bool {
		return runtime.RunningIssueCount() == 0
	})

	requests := runner.requestsSnapshot()
	if got := requests[0].Prompt; got != "Dispatch prompt" {
		t.Fatalf("RunRequest.Prompt = %q, want Dispatch prompt", got)
	}
	if got := requests[0].WorkspacePath; got != filepath.Join(workspace.root, "TOO-1") {
		t.Fatalf("RunRequest.WorkspacePath = %q, want prepared workspace", got)
	}

	prepareRequests := workspace.requestsSnapshot()
	if len(prepareRequests) != 1 {
		t.Fatalf("Prepare calls = %d, want 1", len(prepareRequests))
	}
	if got := prepareRequests[0].IssueIdentifier; got != "TOO-1" {
		t.Fatalf("Prepare IssueIdentifier = %q, want TOO-1", got)
	}
}

func TestRuntimeTickHonorsGlobalConcurrencyLimit(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 1, "Prompt")
	release := make(chan struct{})
	runner := newFakeAgentRunner(release)

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker: &fakeTrackerClient{candidates: []tracker.Issue{
			runtimeIssue("TOO-1", "Todo"),
			runtimeIssue("TOO-2", "Todo"),
		}},
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    runner,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(summary.Dispatched) != 1 || summary.Dispatched[0].IssueKey != "TOO-1" {
		t.Fatalf("Dispatched = %#v, want only first issue", summary.Dispatched)
	}
	if len(summary.Skipped) != 1 || summary.Skipped[0].Reason != "global_concurrency_limit" {
		t.Fatalf("Skipped = %#v, want global concurrency skip", summary.Skipped)
	}
	requireStarted(t, runner.started, "TOO-1")
	if got := runtime.RunningIssueCount(); got != 1 {
		t.Fatalf("RunningIssueCount() = %d, want 1", got)
	}

	close(release)
	waitUntil(t, func() bool {
		return runtime.RunningIssueCount() == 0
	})
}

func TestRuntimeTickHonorsPerStateConcurrencyLimit(t *testing.T) {
	workflowPath := writeRuntimeWorkflowWithStateLimit(t, 5000, 3, "Prompt", "Rework", 1)
	release := make(chan struct{})
	runner := newFakeAgentRunner(release)

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker: &fakeTrackerClient{candidates: []tracker.Issue{
			runtimeIssue("TOO-1", "Rework"),
			runtimeIssue("TOO-2", "Rework"),
			runtimeIssue("TOO-3", "Todo"),
		}},
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    runner,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if got := issueKeys(summary.Dispatched); got != "TOO-1,TOO-3" {
		t.Fatalf("dispatched keys = %q, want TOO-1,TOO-3", got)
	}
	if len(summary.Skipped) != 1 || summary.Skipped[0].IssueKey != "TOO-2" ||
		summary.Skipped[0].Reason != "state_concurrency_limit" {
		t.Fatalf("Skipped = %#v, want TOO-2 state concurrency skip", summary.Skipped)
	}
	requireStartedSet(t, runner.started, []string{"TOO-1", "TOO-3"})
	requireNoStart(t, runner.started)

	close(release)
	waitUntil(t, func() bool {
		return runtime.RunningIssueCount() == 0
	})
}

func TestRuntimeRunPerformsImmediateAndIntervalTicks(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 20, 1, "Prompt")
	ctx, cancel := context.WithCancel(context.Background())
	trackerClient := &fakeTrackerClient{
		onFetch: func(calls int) {
			if calls >= 2 {
				cancel()
			}
		},
	}

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    newFakeAgentRunner(nil),
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}

	if got := trackerClient.fetchCandidateCallCount(); got < 2 {
		t.Fatalf("FetchCandidateIssues calls = %d, want immediate and interval ticks", got)
	}
	if got := runtime.Status(); got != StatusStopped {
		t.Fatalf("Status() = %q, want %q", got, StatusStopped)
	}
}

func TestRuntimeRunContinuesAfterTransientTrackerFetchError(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 20, 1, "Prompt")
	ctx, cancel := context.WithCancel(context.Background())
	trackerClient := &fakeTrackerClient{
		fetchErrors: []error{errors.New("temporary tracker outage"), nil},
		onFetch: func(calls int) {
			if calls >= 2 {
				cancel()
			}
		},
	}

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    newFakeAgentRunner(nil),
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := trackerClient.fetchCandidateCallCount(); got < 2 {
		t.Fatalf("FetchCandidateIssues calls = %d, want retry after transient error", got)
	}
}

func writeRuntimeWorkflow(t *testing.T, intervalMS int, maxAgents int, prompt string) string {
	t.Helper()

	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	writeRuntimeWorkflowAt(t, workflowPath, intervalMS, maxAgents, prompt)
	return workflowPath
}

func writeRuntimeWorkflowAt(t *testing.T, path string, intervalMS int, maxAgents int, prompt string) {
	t.Helper()

	content := strings.TrimLeft(`
---
tracker:
  kind: linear
  api_key: literal-token
  project_slug: symphony-go
  active_states:
    - Todo
    - In Progress
    - Rework
polling:
  interval_ms: {interval_ms}
agent:
  max_concurrent_agents: {max_agents}
---

{prompt}
`, "\n")
	content = strings.ReplaceAll(content, "{interval_ms}", strconv.Itoa(intervalMS))
	content = strings.ReplaceAll(content, "{max_agents}", strconv.Itoa(maxAgents))
	content = strings.ReplaceAll(content, "{prompt}", prompt)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
}

func writeRuntimeWorkflowWithStateLimit(
	t *testing.T,
	intervalMS int,
	maxAgents int,
	prompt string,
	state string,
	limit int,
) string {
	t.Helper()

	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	content := strings.TrimLeft(`
---
tracker:
  kind: linear
  api_key: literal-token
  project_slug: symphony-go
  active_states:
    - Todo
    - In Progress
    - Rework
polling:
  interval_ms: {interval_ms}
agent:
  max_concurrent_agents: {max_agents}
  max_concurrent_agents_by_state:
    {state}: {limit}
---

{prompt}
`, "\n")
	content = strings.ReplaceAll(content, "{interval_ms}", strconv.Itoa(intervalMS))
	content = strings.ReplaceAll(content, "{max_agents}", strconv.Itoa(maxAgents))
	content = strings.ReplaceAll(content, "{state}", state)
	content = strings.ReplaceAll(content, "{limit}", strconv.Itoa(limit))
	content = strings.ReplaceAll(content, "{prompt}", prompt)

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
	return workflowPath
}

type fakeTrackerClient struct {
	mu                  sync.Mutex
	candidates          []tracker.Issue
	fetchErrors         []error
	fetchCandidateCalls int
	onFetch             func(int)
}

func (f *fakeTrackerClient) FetchCandidateIssues(context.Context) ([]tracker.Issue, error) {
	f.mu.Lock()
	f.fetchCandidateCalls++
	calls := f.fetchCandidateCalls
	candidates := append([]tracker.Issue(nil), f.candidates...)
	var fetchErr error
	if calls <= len(f.fetchErrors) {
		fetchErr = f.fetchErrors[calls-1]
	}
	onFetch := f.onFetch
	f.mu.Unlock()

	if onFetch != nil {
		onFetch(calls)
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	return candidates, nil
}

func (f *fakeTrackerClient) FetchIssuesByStates(context.Context, []string) ([]tracker.Issue, error) {
	return nil, nil
}

func (f *fakeTrackerClient) FetchIssueStatesByIDs(context.Context, []string) ([]tracker.Issue, error) {
	return nil, nil
}

func (f *fakeTrackerClient) fetchCandidateCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetchCandidateCalls
}

type fakeWorkspacePreparer struct {
	mu         sync.Mutex
	root       string
	createdNow bool
	requests   []workspace.PrepareRequest
}

func (f *fakeWorkspacePreparer) Prepare(req workspace.PrepareRequest) (workspace.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, req)
	return workspace.Workspace{
		Path:       filepath.Join(f.root, req.IssueIdentifier),
		Key:        req.IssueIdentifier,
		CreatedNow: f.createdNow,
		IssueID:    req.IssueID,
		IssueKey:   req.IssueIdentifier,
	}, nil
}

func (f *fakeWorkspacePreparer) requestsSnapshot() []workspace.PrepareRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]workspace.PrepareRequest(nil), f.requests...)
}

type fakeHookRunner struct {
	mu    sync.Mutex
	calls []hooks.Name
}

func (f *fakeHookRunner) RunAfterCreate(
	ctx context.Context,
	cwd string,
	createdNow bool,
) (hooks.Result, error) {
	if !createdNow {
		return hooks.Result{Name: hooks.AfterCreate, CWD: cwd, Skipped: true, ExitCode: 0}, nil
	}
	return f.Run(ctx, hooks.AfterCreate, cwd)
}

func (f *fakeHookRunner) Run(ctx context.Context, name hooks.Name, cwd string) (hooks.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
	return hooks.Result{Name: name, CWD: cwd, ExitCode: 0}, nil
}

func (f *fakeHookRunner) callNamesJoined() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	names := make([]string, 0, len(f.calls))
	for _, name := range f.calls {
		names = append(names, name.String())
	}
	return strings.Join(names, ",")
}

type fakeAgentRunner struct {
	mu       sync.Mutex
	requests []agent.RunRequest
	started  chan agent.RunRequest
	release  <-chan struct{}
}

func newFakeAgentRunner(release <-chan struct{}) *fakeAgentRunner {
	return &fakeAgentRunner{
		started: make(chan agent.RunRequest, 20),
		release: release,
	}
}

func (f *fakeAgentRunner) Run(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()

	f.started <- req
	if f.release != nil {
		select {
		case <-ctx.Done():
			return agent.RunResult{}, ctx.Err()
		case <-f.release:
		}
	}
	return agent.RunResult{SessionID: "session-" + req.IssueKey}, nil
}

func (f *fakeAgentRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeAgentRunner) requestsSnapshot() []agent.RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agent.RunRequest(nil), f.requests...)
}

func runtimeIssue(identifier string, state string) tracker.Issue {
	return tracker.Issue{
		ID:         "issue-" + identifier,
		Identifier: identifier,
		Title:      "Title " + identifier,
		State:      state,
	}
}

func withRuntimeBlockers(issue tracker.Issue, blockers ...tracker.BlockerRef) tracker.Issue {
	issue.BlockedBy = blockers
	return issue
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func requireStarted(t *testing.T, started <-chan agent.RunRequest, wantIssueKey string) {
	t.Helper()

	select {
	case req := <-started:
		if req.IssueKey != wantIssueKey {
			t.Fatalf("started IssueKey = %q, want %q", req.IssueKey, wantIssueKey)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s to start", wantIssueKey)
	}
}

func requireStartedSet(t *testing.T, started <-chan agent.RunRequest, wantIssueKeys []string) {
	t.Helper()

	got := make([]string, 0, len(wantIssueKeys))
	for range wantIssueKeys {
		select {
		case req := <-started:
			got = append(got, req.IssueKey)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for starts; got %v want %v", got, wantIssueKeys)
		}
	}
	sort.Strings(got)
	want := append([]string(nil), wantIssueKeys...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("started issue keys = %v, want %v", got, want)
	}
}

func requireNoStart(t *testing.T, started <-chan agent.RunRequest) {
	t.Helper()

	select {
	case req := <-started:
		t.Fatalf("unexpected extra start: %#v", req)
	case <-time.After(30 * time.Millisecond):
	}
}

func issueKeys(dispatched []DispatchSummary) string {
	keys := make([]string, 0, len(dispatched))
	for _, item := range dispatched {
		keys = append(keys, item.IssueKey)
	}
	return strings.Join(keys, ",")
}
