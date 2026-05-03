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
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
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
	if got := requests[0].PromptTemplate; got != "Dispatch prompt" {
		t.Fatalf("RunRequest.PromptTemplate = %q, want Dispatch prompt", got)
	}
	if got := requests[0].Issue.Identifier; got != "TOO-1" {
		t.Fatalf("RunRequest.Issue.Identifier = %q, want TOO-1", got)
	}
	if requests[0].Attempt != nil {
		t.Fatalf("RunRequest.Attempt = %v, want nil on first dispatch", *requests[0].Attempt)
	}
	if got := requests[0].MaxTurns; got != config.DefaultMaxTurns {
		t.Fatalf("RunRequest.MaxTurns = %d, want default %d", got, config.DefaultMaxTurns)
	}
	if requests[0].Codex.TurnTimeout != config.DefaultCodexTurnTimeout ||
		requests[0].Codex.ReadTimeout != config.DefaultCodexReadTimeout ||
		requests[0].Codex.StallTimeout != config.DefaultCodexStallTimeout {
		t.Fatalf("RunRequest.Codex timeouts = %#v", requests[0].Codex)
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

func TestRuntimeNormalExitSchedulesContinuationRetryAndRedispatchesWhenDue(t *testing.T) {
	start := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Prompt")
	release := make(chan struct{})
	runner := newFakeAgentRunner(release)

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker: &fakeTrackerClient{candidates: []tracker.Issue{
			runtimeIssue("TOO-1", "Todo"),
		}},
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    runner,
		Clock:     clock.Now,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	requireStarted(t, runner.started, "TOO-1")
	close(release)
	waitUntil(t, func() bool {
		return runtime.RetryIssueCount() == 1
	})

	entry := onlyRetryEntry(t, runtime)
	if entry.Attempt != 1 || !entry.DueAt.Equal(start.Add(time.Second)) || entry.Error != "" {
		t.Fatalf("retry entry = %#v, want continuation attempt due in 1s", entry)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick(before due) error = %v", err)
	}
	if got := runner.callCount(); got != 1 {
		t.Fatalf("runner calls before retry due = %d, want 1", got)
	}

	clock.Advance(time.Second)
	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick(after due) error = %v", err)
	}
	waitUntil(t, func() bool {
		return runner.callCount() == 2
	})
	if len(summary.Retries.Dispatched) != 1 || summary.Retries.Dispatched[0].Attempt != 1 {
		t.Fatalf("retry dispatch summary = %#v, want attempt 1 dispatch", summary.Retries.Dispatched)
	}
	requests := runner.requestsSnapshot()
	if len(requests) != 2 || requests[1].Attempt == nil || *requests[1].Attempt != 1 {
		t.Fatalf("retry RunRequest.Attempt = %#v, want attempt 1", requests)
	}
}

func TestRuntimeAbnormalExitSchedulesExponentialRetryWithConfiguredCap(t *testing.T) {
	start := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	workflowPath := writeRuntimeWorkflowWithRetryBackoff(t, 5000, 2, 15000, "Prompt")
	runner := newFakeAgentRunner(nil)
	runner.runErrors = []error{errors.New("agent failed"), errors.New("agent failed again")}

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker: &fakeTrackerClient{candidates: []tracker.Issue{
			runtimeIssue("TOO-1", "Todo"),
		}},
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    runner,
		Clock:     clock.Now,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	waitUntil(t, func() bool {
		return runtime.RetryIssueCount() == 1
	})
	entry := onlyRetryEntry(t, runtime)
	if entry.Attempt != 1 || !entry.DueAt.Equal(start.Add(10*time.Second)) || entry.Error != "agent failed" {
		t.Fatalf("first retry = %#v, want attempt 1 after 10s", entry)
	}

	clock.Advance(10 * time.Second)
	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick(retry) error = %v", err)
	}
	waitUntil(t, func() bool {
		entries := runtime.RetryEntries()
		return len(entries) == 1 && entries[0].Attempt == 2
	})
	entry = onlyRetryEntry(t, runtime)
	wantDue := start.Add(25 * time.Second)
	if entry.Attempt != 2 || !entry.DueAt.Equal(wantDue) || entry.Error != "agent failed again" {
		t.Fatalf("second retry = %#v, want attempt 2 capped at 15s due %s", entry, wantDue)
	}
}

func TestRuntimeDueRetryReleasesWhenIssueIsNoLongerCandidate(t *testing.T) {
	start := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Prompt")
	trackerClient := &fakeTrackerClient{candidates: []tracker.Issue{
		runtimeIssue("TOO-1", "Todo"),
	}}
	runner := newFakeAgentRunner(nil)
	runner.runErrors = []error{errors.New("agent failed")}

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    runner,
		Clock:     clock.Now,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	waitUntil(t, func() bool {
		return runtime.RetryIssueCount() == 1
	})

	trackerClient.setCandidates(nil)
	clock.Advance(10 * time.Second)
	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick(release retry) error = %v", err)
	}
	if got := runtime.RetryIssueCount(); got != 0 {
		t.Fatalf("RetryIssueCount() = %d, want 0 after release", got)
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner calls = %d, want no redispatch", runner.callCount())
	}
	if len(summary.Retries.Released) != 1 || summary.Retries.Released[0].Reason != "retry_issue_not_candidate" {
		t.Fatalf("Released = %#v, want retry_issue_not_candidate", summary.Retries.Released)
	}
}

func TestRuntimeReconcileUpdatesActiveRunningState(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Prompt")
	release := make(chan struct{})
	trackerClient := &fakeTrackerClient{candidates: []tracker.Issue{
		runtimeIssue("TOO-1", "Todo"),
	}}

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true},
		Hooks:     &fakeHookRunner{},
		Runner:    newFakeAgentRunner(release),
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	waitUntil(t, func() bool {
		return runtime.RunningIssueCount() == 1
	})

	trackerClient.setCandidates(nil)
	trackerClient.setRefreshed([]tracker.Issue{runtimeIssue("TOO-1", "Rework")})
	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick(reconcile active) error = %v", err)
	}
	records := runtime.RunningRecords()
	if len(records) != 1 || records[0].State != "Rework" {
		t.Fatalf("running records = %#v, want Rework state", records)
	}
	if len(summary.Reconciliation.Updated) != 1 || summary.Reconciliation.Updated[0].State != "Rework" {
		t.Fatalf("reconciliation updated = %#v, want Rework", summary.Reconciliation.Updated)
	}
	close(release)
}

func TestRuntimeReconcileStopsNonActiveRunWithoutCleanupOrRetry(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Prompt")
	release := make(chan struct{})
	trackerClient := &fakeTrackerClient{candidates: []tracker.Issue{
		runtimeIssue("TOO-1", "Todo"),
	}}
	workspace := &fakeWorkspacePreparer{root: t.TempDir(), createdNow: true}
	hookRunner := &fakeHookRunner{}
	runner := newFakeAgentRunner(release)

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: workspace,
		Hooks:     hookRunner,
		Runner:    runner,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	requireStarted(t, runner.started, "TOO-1")
	trackerClient.setRefreshed([]tracker.Issue{runtimeIssue("TOO-1", "Backlog")})

	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick(reconcile inactive) error = %v", err)
	}
	waitUntil(t, func() bool {
		return runner.callCount() == 1 && runtime.RunningIssueCount() == 0
	})
	if runtime.RetryIssueCount() != 0 {
		t.Fatalf("RetryIssueCount() = %d, want 0 after reconciliation stop", runtime.RetryIssueCount())
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner calls = %d, want no same-tick stale-candidate redispatch", runner.callCount())
	}
	if workspace.removeCallCount() != 0 {
		t.Fatalf("workspace remove calls = %d, want 0 for non-active state", workspace.removeCallCount())
	}
	if hookRunner.hasCall(hooks.BeforeRemove) {
		t.Fatal("before_remove hook ran for non-active state")
	}
	if len(summary.Reconciliation.Stopped) != 1 || summary.Reconciliation.Stopped[0].Reason != "inactive_state" {
		t.Fatalf("Stopped = %#v, want inactive_state", summary.Reconciliation.Stopped)
	}
}

func TestRuntimeReconcileStopsTerminalRunAndCleansWorkspace(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Prompt")
	release := make(chan struct{})
	trackerClient := &fakeTrackerClient{candidates: []tracker.Issue{
		runtimeIssue("TOO-1", "Todo"),
	}}
	workspace := &fakeWorkspacePreparer{
		root:            t.TempDir(),
		createdNow:      true,
		cleanupExisting: map[string]bool{"TOO-1": true},
		cleanupRealDir:  map[string]bool{"TOO-1": true},
	}
	hookRunner := &fakeHookRunner{}
	runner := newFakeAgentRunner(release)

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: workspace,
		Hooks:     hookRunner,
		Runner:    runner,
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if _, err := runtime.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	requireStarted(t, runner.started, "TOO-1")
	trackerClient.setRefreshed([]tracker.Issue{runtimeIssue("TOO-1", "Done")})

	summary, err := runtime.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick(reconcile terminal) error = %v", err)
	}
	waitUntil(t, func() bool {
		return runtime.RunningIssueCount() == 0
	})
	if runtime.RetryIssueCount() != 0 {
		t.Fatalf("RetryIssueCount() = %d, want 0 after terminal reconciliation", runtime.RetryIssueCount())
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner calls = %d, want no same-tick stale-candidate redispatch", runner.callCount())
	}
	if !hookRunner.hasCall(hooks.BeforeRemove) {
		t.Fatal("before_remove hook did not run for terminal cleanup")
	}
	if workspace.removeCallCount() != 1 {
		t.Fatalf("workspace remove calls = %d, want 1", workspace.removeCallCount())
	}
	if len(summary.Reconciliation.Stopped) != 1 || summary.Reconciliation.Stopped[0].Reason != "terminal_state" ||
		!summary.Reconciliation.Stopped[0].Cleanup.Removed {
		t.Fatalf("Stopped = %#v, want terminal cleanup removal", summary.Reconciliation.Stopped)
	}
}

func TestRuntimeRunPerformsStartupTerminalWorkspaceCleanup(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 20, 1, "Prompt")
	ctx, cancel := context.WithCancel(context.Background())
	trackerClient := &fakeTrackerClient{
		terminalIssues: []tracker.Issue{runtimeIssue("TOO-9", "Done")},
		onFetch: func(int) {
			cancel()
		},
	}
	workspace := &fakeWorkspacePreparer{
		root:            t.TempDir(),
		createdNow:      true,
		cleanupExisting: map[string]bool{"TOO-9": true},
		cleanupRealDir:  map[string]bool{"TOO-9": true},
	}
	hookRunner := &fakeHookRunner{}

	runtime, err := NewRuntimeWithDependencies(workflowPath, Dependencies{
		Tracker:   trackerClient,
		Workspace: workspace,
		Hooks:     hookRunner,
		Runner:    newFakeAgentRunner(nil),
	})
	if err != nil {
		t.Fatalf("NewRuntimeWithDependencies() error = %v", err)
	}

	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !hookRunner.hasCall(hooks.BeforeRemove) {
		t.Fatal("before_remove hook did not run during startup cleanup")
	}
	if workspace.removeCallCount() != 1 {
		t.Fatalf("workspace remove calls = %d, want 1", workspace.removeCallCount())
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

func writeRuntimeWorkflowWithRetryBackoff(
	t *testing.T,
	intervalMS int,
	maxAgents int,
	maxRetryBackoffMS int,
	prompt string,
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
  max_retry_backoff_ms: {max_retry_backoff_ms}
---

{prompt}
`, "\n")
	content = strings.ReplaceAll(content, "{interval_ms}", strconv.Itoa(intervalMS))
	content = strings.ReplaceAll(content, "{max_agents}", strconv.Itoa(maxAgents))
	content = strings.ReplaceAll(content, "{max_retry_backoff_ms}", strconv.Itoa(maxRetryBackoffMS))
	content = strings.ReplaceAll(content, "{prompt}", prompt)

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
	return workflowPath
}

type fakeTrackerClient struct {
	mu                  sync.Mutex
	candidates          []tracker.Issue
	terminalIssues      []tracker.Issue
	refreshed           []tracker.Issue
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
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tracker.Issue(nil), f.terminalIssues...), nil
}

func (f *fakeTrackerClient) FetchIssueStatesByIDs(context.Context, []string) ([]tracker.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tracker.Issue(nil), f.refreshed...), nil
}

func (f *fakeTrackerClient) fetchCandidateCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetchCandidateCalls
}

func (f *fakeTrackerClient) setCandidates(candidates []tracker.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.candidates = append([]tracker.Issue(nil), candidates...)
}

func (f *fakeTrackerClient) setRefreshed(issues []tracker.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshed = append([]tracker.Issue(nil), issues...)
}

type fakeWorkspacePreparer struct {
	mu              sync.Mutex
	root            string
	createdNow      bool
	requests        []workspace.PrepareRequest
	cleanupRequests []workspace.CleanupRequest
	removedTargets  []workspace.CleanupTarget
	cleanupExisting map[string]bool
	cleanupRealDir  map[string]bool
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

func (f *fakeWorkspacePreparer) CleanupTarget(req workspace.CleanupRequest) (workspace.CleanupTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cleanupRequests = append(f.cleanupRequests, req)
	key, err := workspace.SanitizeIdentifier(req.IssueIdentifier)
	if err != nil {
		return workspace.CleanupTarget{}, err
	}
	exists := f.cleanupExisting != nil && f.cleanupExisting[req.IssueIdentifier]
	realDir := exists
	if f.cleanupRealDir != nil {
		realDir = f.cleanupRealDir[req.IssueIdentifier]
	}
	return workspace.CleanupTarget{
		Workspace: workspace.Workspace{
			Path:       filepath.Join(f.root, key),
			Key:        key,
			IssueID:    req.IssueID,
			IssueKey:   req.IssueIdentifier,
			CreatedNow: false,
		},
		Exists:          exists,
		IsRealDirectory: realDir,
	}, nil
}

func (f *fakeWorkspacePreparer) Remove(target workspace.CleanupTarget) (workspace.CleanupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedTargets = append(f.removedTargets, target)
	return workspace.CleanupResult{Target: target, Removed: target.Exists}, nil
}

func (f *fakeWorkspacePreparer) removeCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.removedTargets)
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

func (f *fakeHookRunner) hasCall(want hooks.Name) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range f.calls {
		if name == want {
			return true
		}
	}
	return false
}

type fakeAgentRunner struct {
	mu        sync.Mutex
	requests  []agent.RunRequest
	started   chan agent.RunRequest
	release   <-chan struct{}
	runErrors []error
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
	callIndex := len(f.requests) - 1
	f.mu.Unlock()

	f.started <- req
	if f.release != nil {
		select {
		case <-ctx.Done():
			return agent.RunResult{}, ctx.Err()
		case <-f.release:
		}
	}
	result := agent.RunResult{SessionID: "session-" + req.IssueKey}
	f.mu.Lock()
	var runErr error
	if callIndex < len(f.runErrors) {
		runErr = f.runErrors[callIndex]
	}
	f.mu.Unlock()
	return result, runErr
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

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(duration time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(duration)
}

func onlyRetryEntry(t *testing.T, runtime *Runtime) runstate.Retry {
	t.Helper()

	entries := runtime.RetryEntries()
	if len(entries) != 1 {
		t.Fatalf("RetryEntries() = %#v, want one entry", entries)
	}
	return entries[0]
}

func issueKeys(dispatched []DispatchSummary) string {
	keys := make([]string, 0, len(dispatched))
	for _, item := range dispatched {
		keys = append(keys, item.IssueKey)
	}
	return strings.Join(keys, ",")
}
