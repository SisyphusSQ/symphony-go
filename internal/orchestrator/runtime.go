package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/agent"
	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/hooks"
	"github.com/SisyphusSQ/symphony-go/internal/policy"
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
	"github.com/SisyphusSQ/symphony-go/internal/workspace"
)

var ErrMissingDependency = errors.New("missing_orchestrator_dependency")

const (
	continuationRetryDelay = time.Second
	failureRetryBaseDelay  = 10 * time.Second
)

// WorkspaceManager is the workspace surface consumed by dispatch and cleanup.
type WorkspaceManager interface {
	Prepare(workspace.PrepareRequest) (workspace.Workspace, error)
	CleanupTarget(workspace.CleanupRequest) (workspace.CleanupTarget, error)
	Remove(workspace.CleanupTarget) (workspace.CleanupResult, error)
}

// HookRunner is the lifecycle-hook surface consumed by dispatch.
type HookRunner interface {
	RunAfterCreate(ctx context.Context, cwd string, createdNow bool) (hooks.Result, error)
	Run(ctx context.Context, name hooks.Name, cwd string) (hooks.Result, error)
}

// Dependencies are the side-effecting collaborators needed by the dispatch loop.
type Dependencies struct {
	Tracker   tracker.Client
	Workspace WorkspaceManager
	Hooks     HookRunner
	Runner    agent.Runner
	Clock     func() time.Time
}

// Runtime is the reload-aware surface and the single owner of mutable
// orchestrator state.
type Runtime struct {
	mu       sync.Mutex
	status   Status
	reloader *config.Reloader
	deps     Dependencies
	state    runtimeState
}

// NewRuntime loads the initial workflow config and prepares the runtime state.
func NewRuntime(workflowPath string, opts ...config.Option) (*Runtime, error) {
	return NewRuntimeWithDependencies(workflowPath, Dependencies{}, opts...)
}

// NewRuntimeWithDependencies loads workflow config and wires the runtime
// collaborators used by Tick and Run.
func NewRuntimeWithDependencies(
	workflowPath string,
	deps Dependencies,
	opts ...config.Option,
) (*Runtime, error) {
	reloader, err := config.NewReloader(workflowPath, opts...)
	if err != nil {
		return nil, err
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &Runtime{
		status:   StatusRunning,
		reloader: reloader,
		deps:     deps,
		state:    newRuntimeState(),
	}, nil
}

// Status returns the operator-visible runtime lifecycle state.
func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// MarkActive records an active issue without coupling reload to dispatch state.
func (r *Runtime) MarkActive(issueID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.markActive(issueID)
}

// ActiveIssueCount returns the number of active issue entries retained by the runtime.
func (r *Runtime) ActiveIssueCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.activeIssueCount()
}

// RunningIssueCount returns the number of currently running issue attempts.
func (r *Runtime) RunningIssueCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.runningIssueCount()
}

// RetryIssueCount returns the number of currently queued retry entries.
func (r *Runtime) RetryIssueCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.retryIssueCount()
}

// RetryEntries returns a snapshot of queued retry entries.
func (r *Runtime) RetryEntries() []runstate.Retry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.retryEntries()
}

// RunningRecords returns a snapshot of currently running issue records.
func (r *Runtime) RunningRecords() []RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.runningRecords()
}

// ReloadWorkflowIfChanged updates the effective config for future dispatch
// while preserving active issue state.
func (r *Runtime) ReloadWorkflowIfChanged() config.ReloadResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reloader.ReloadIfChanged()
}

// FutureDispatchConfig returns the last known good config used by subsequent
// dispatch decisions, hook executions, and agent launches.
func (r *Runtime) FutureDispatchConfig() config.Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reloader.Current()
}

// DispatchReady reports whether the runtime has all side-effecting
// collaborators required to start polling and dispatching.
func (r *Runtime) DispatchReady() error {
	if r.deps.Tracker == nil {
		return fmt.Errorf("%w: tracker", ErrMissingDependency)
	}
	if r.deps.Workspace == nil {
		return fmt.Errorf("%w: workspace", ErrMissingDependency)
	}
	if r.deps.Hooks == nil {
		return fmt.Errorf("%w: hooks", ErrMissingDependency)
	}
	if r.deps.Runner == nil {
		return fmt.Errorf("%w: runner", ErrMissingDependency)
	}
	return nil
}

// TickSummary records one orchestrator polling tick.
type TickSummary struct {
	Reload         config.ReloadResult
	Reconciliation ReconcileSummary
	Retries        RetryTickSummary
	Candidates     int
	Dispatched     []DispatchSummary
	Skipped        []SkipSummary
	TrackerErr     error
	DispatchErr    error
}

// DispatchSummary records one issue attempt that was admitted by the tick.
type DispatchSummary struct {
	IssueID  string
	IssueKey string
	State    string
	Attempt  int
}

// SkipSummary records a candidate that the tick did not dispatch.
type SkipSummary struct {
	IssueID  string
	IssueKey string
	State    string
	Reason   string
}

// ReconcileSummary records active-run reconciliation decisions.
type ReconcileSummary struct {
	Checked    int
	Updated    []DispatchSummary
	Stopped    []StopSummary
	TrackerErr error
}

// StopSummary records one running issue stopped by reconciliation.
type StopSummary struct {
	IssueID  string
	IssueKey string
	State    string
	Reason   string
	Cleanup  CleanupSummary
}

// RetryTickSummary records due retry handling for one tick.
type RetryTickSummary struct {
	Due        int
	Dispatched []DispatchSummary
	Requeued   []runstate.Retry
	Released   []SkipSummary
}

// CleanupSummary records one terminal workspace cleanup attempt.
type CleanupSummary struct {
	IssueID     string
	IssueKey    string
	Path        string
	Existed     bool
	Removed     bool
	HookErr     error
	CleanupErr  error
	SkippedHook bool
}

// StartupCleanupSummary records terminal workspace cleanup during service startup.
type StartupCleanupSummary struct {
	Issues     int
	Cleanups   []CleanupSummary
	TrackerErr error
}

// RunOnce executes one polling tick.
func (r *Runtime) RunOnce(ctx context.Context) (TickSummary, error) {
	return r.Tick(ctx)
}

// Run starts the orchestrator loop. It performs an immediate tick and then
// repeats at the current effective polling interval.
func (r *Runtime) Run(ctx context.Context) error {
	r.CleanupTerminalWorkspaces(ctx)

	if _, err := r.Tick(ctx); err != nil {
		if stop, stopErr := r.shouldStopAfterTickError(ctx, err); stop {
			r.setStatus(StatusStopped)
			return stopErr
		}
	}

	for {
		interval := r.FutureDispatchConfig().Polling.Interval
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			r.setStatus(StatusStopped)
			return nil
		case <-timer.C:
			if _, err := r.Tick(ctx); err != nil {
				if stop, stopErr := r.shouldStopAfterTickError(ctx, err); stop {
					r.setStatus(StatusStopped)
					return stopErr
				}
			}
		}
	}
}

// Tick reloads workflow config if needed, fetches candidates, applies dispatch
// policy, and launches eligible issue attempts up to the configured slot limits.
func (r *Runtime) Tick(ctx context.Context) (TickSummary, error) {
	summary := TickSummary{
		Reload: r.ReloadWorkflowIfChanged(),
	}
	if err := ctx.Err(); err != nil {
		summary.DispatchErr = err
		return summary, err
	}

	cfg := r.FutureDispatchConfig()
	if err := r.DispatchReady(); err != nil {
		summary.DispatchErr = err
		return summary, err
	}
	summary.Reconciliation = r.reconcileRunning(ctx, cfg)
	stoppedThisTick := stoppedIssueIDs(summary.Reconciliation.Stopped)

	now := r.deps.Clock()
	r.mu.Lock()
	dueRetries := r.state.dueRetries(now)
	r.mu.Unlock()
	summary.Retries.Due = len(dueRetries)

	issues, err := r.deps.Tracker.FetchCandidateIssues(ctx)
	if err != nil {
		summary.TrackerErr = err
		if len(dueRetries) > 0 {
			summary.Retries.Requeued = r.requeueRetriesAfterFetchError(cfg, dueRetries, now)
		}
		return summary, err
	}
	summary.Candidates = len(issues)

	r.handleDueRetries(ctx, cfg, dueRetries, issues, &summary)

	r.mu.Lock()
	runtimeSnapshot := r.state.policyRuntimeState()
	r.mu.Unlock()

	decisions := policy.EvaluateCandidates(cfg.Tracker, issues, runtimeSnapshot)
	for _, decision := range decisions {
		issue := decision.Issue
		if _, ok := stoppedThisTick[issue.ID]; ok {
			summary.Skipped = append(summary.Skipped, SkipSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Reason:   "stopped_by_reconciliation",
			})
			continue
		}
		if !decision.Eligibility.Allowed {
			summary.Skipped = append(summary.Skipped, skipFromPolicy(issue, decision.Eligibility))
			continue
		}

		if skip, runCtx, ok := r.tryStart(ctx, issue, cfg, 0); !ok {
			summary.Skipped = append(summary.Skipped, skip)
			continue
		} else {
			summary.Dispatched = append(summary.Dispatched, DispatchSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
			})
			go r.runIssue(runCtx, cfg, issue, 0)
		}
	}

	return summary, nil
}

func (r *Runtime) tryStart(
	ctx context.Context,
	issue tracker.Issue,
	cfg config.Config,
	attempt int,
) (SkipSummary, context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.state.running[issue.ID]; ok {
		return SkipSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Reason:   string(policy.ReasonAlreadyRunning),
		}, nil, false
	}
	if _, ok := r.state.retries[issue.ID]; ok {
		return SkipSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Reason:   string(policy.ReasonAlreadyClaimed),
		}, nil, false
	}
	if r.state.runningIssueCount() >= cfg.Agent.MaxConcurrentAgents {
		return SkipSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Reason:   "global_concurrency_limit",
		}, nil, false
	}
	stateLimit := cfg.Agent.MaxConcurrentAgents
	if limit, ok := cfg.Agent.MaxConcurrentAgentsByState[normalizeState(issue.State)]; ok {
		stateLimit = limit
	}
	if r.state.runningByState(issue.State) >= stateLimit {
		return SkipSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Reason:   "state_concurrency_limit",
		}, nil, false
	}

	runCtx, cancel := context.WithCancel(ctx)
	r.state.start(issue, "", attempt, r.deps.Clock(), cancel)
	return SkipSummary{}, runCtx, true
}

func (r *Runtime) runIssue(ctx context.Context, cfg config.Config, issue tracker.Issue, attempt int) {
	var sessionID string
	normalExit := false
	exitErr := "worker exited without result"
	defer func() {
		r.completeRun(issue.ID, sessionID, normalExit, exitErr, cfg)
	}()

	ws, err := r.deps.Workspace.Prepare(workspace.PrepareRequest{
		IssueID:         issue.ID,
		IssueIdentifier: issue.Identifier,
		WorkflowPath:    cfg.WorkflowRef,
	})
	if err != nil {
		exitErr = err.Error()
		return
	}
	r.mu.Lock()
	r.state.updateWorkspace(issue.ID, ws.Path)
	r.mu.Unlock()

	if _, err := r.deps.Hooks.RunAfterCreate(ctx, ws.Path, ws.CreatedNow); err != nil {
		exitErr = err.Error()
		return
	}
	if _, err := r.deps.Hooks.Run(ctx, hooks.BeforeRun, ws.Path); err != nil {
		exitErr = err.Error()
		return
	}

	result, runErr := r.deps.Runner.Run(ctx, agent.RunRequest{
		Issue:          issue,
		Attempt:        agent.AttemptFromNumber(attempt),
		IssueID:        issue.ID,
		IssueKey:       issue.Identifier,
		WorkspacePath:  ws.Path,
		Prompt:         cfg.PromptBody,
		PromptTemplate: cfg.PromptBody,
		MaxTurns:       cfg.Agent.MaxTurns,
		Tracker:        cfg.Tracker,
		Codex:          cfg.Codex,
	})
	sessionID = result.SessionID
	_, _ = r.deps.Hooks.Run(ctx, hooks.AfterRun, ws.Path)
	if runErr != nil {
		exitErr = runErr.Error()
		return
	}
	normalExit = true
	exitErr = ""
}

func (r *Runtime) completeRun(
	issueID string,
	sessionID string,
	normalExit bool,
	exitErr string,
	cfg config.Config,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sessionID != "" {
		r.state.updateSession(issueID, sessionID)
	}
	record, ok := r.state.finish(issueID)
	if !ok {
		return
	}

	now := r.deps.Clock()
	if normalExit {
		r.state.completed[issueID] = struct{}{}
		r.state.scheduleRetry(runstate.Retry{
			IssueID:  issueID,
			IssueKey: record.IssueKey,
			Attempt:  1,
			DueAt:    now.Add(continuationRetryDelay),
		})
		return
	}

	attempt := record.Attempt + 1
	if attempt < 1 {
		attempt = 1
	}
	r.state.scheduleRetry(runstate.Retry{
		IssueID:  issueID,
		IssueKey: record.IssueKey,
		Attempt:  attempt,
		DueAt:    now.Add(failureRetryDelay(attempt, cfg.Agent.MaxRetryBackoff)),
		Error:    exitErr,
	})
}

func (r *Runtime) reconcileRunning(ctx context.Context, cfg config.Config) ReconcileSummary {
	r.mu.Lock()
	records := r.state.runningRecords()
	r.mu.Unlock()

	summary := ReconcileSummary{Checked: len(records)}
	if len(records) == 0 {
		return summary
	}

	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.IssueID)
	}
	issues, err := r.deps.Tracker.FetchIssueStatesByIDs(ctx, ids)
	if err != nil {
		summary.TrackerErr = err
		return summary
	}

	byID := make(map[string]tracker.Issue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}

	for _, record := range records {
		issue, ok := byID[record.IssueID]
		if !ok {
			continue
		}
		if issue.Identifier == "" {
			issue.Identifier = record.IssueKey
		}

		switch {
		case stateIn(cfg.Tracker.TerminalStates, issue.State):
			stopped, ok := r.stopRunning(record.IssueID)
			if !ok {
				continue
			}
			if stopped.IssueKey != "" && issue.Identifier == "" {
				issue.Identifier = stopped.IssueKey
			}
			cleanup := r.cleanupIssueWorkspace(ctx, cfg, issue)
			summary.Stopped = append(summary.Stopped, StopSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Reason:   "terminal_state",
				Cleanup:  cleanup,
			})
		case stateIn(cfg.Tracker.ActiveStates, issue.State):
			r.mu.Lock()
			r.state.updateIssue(issue)
			r.mu.Unlock()
			summary.Updated = append(summary.Updated, DispatchSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Attempt:  record.Attempt,
			})
		default:
			if _, ok := r.stopRunning(record.IssueID); !ok {
				continue
			}
			summary.Stopped = append(summary.Stopped, StopSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Reason:   "inactive_state",
			})
		}
	}
	return summary
}

func (r *Runtime) stopRunning(issueID string) (RunRecord, bool) {
	r.mu.Lock()
	record, ok := r.state.stop(issueID)
	r.mu.Unlock()
	if ok && record.cancel != nil {
		record.cancel()
	}
	return record, ok
}

// CleanupTerminalWorkspaces removes workspaces for currently terminal tracker issues.
func (r *Runtime) CleanupTerminalWorkspaces(ctx context.Context) StartupCleanupSummary {
	cfg := r.FutureDispatchConfig()
	summary := StartupCleanupSummary{}
	if r.deps.Tracker == nil {
		summary.TrackerErr = fmt.Errorf("%w: tracker", ErrMissingDependency)
		return summary
	}

	issues, err := r.deps.Tracker.FetchIssuesByStates(ctx, cfg.Tracker.TerminalStates)
	if err != nil {
		summary.TrackerErr = err
		return summary
	}
	summary.Issues = len(issues)
	for _, issue := range issues {
		summary.Cleanups = append(summary.Cleanups, r.cleanupIssueWorkspace(ctx, cfg, issue))
	}
	return summary
}

func (r *Runtime) cleanupIssueWorkspace(
	ctx context.Context,
	cfg config.Config,
	issue tracker.Issue,
) CleanupSummary {
	summary := CleanupSummary{
		IssueID:  issue.ID,
		IssueKey: issue.Identifier,
	}
	if r.deps.Workspace == nil {
		summary.CleanupErr = fmt.Errorf("%w: workspace", ErrMissingDependency)
		return summary
	}
	if r.deps.Hooks == nil {
		summary.CleanupErr = fmt.Errorf("%w: hooks", ErrMissingDependency)
		return summary
	}

	target, err := r.deps.Workspace.CleanupTarget(workspace.CleanupRequest{
		IssueID:         issue.ID,
		IssueIdentifier: issue.Identifier,
		WorkflowPath:    cfg.WorkflowRef,
	})
	if err != nil {
		summary.CleanupErr = err
		return summary
	}
	summary.Path = target.Path
	summary.Existed = target.Exists

	if target.Exists && target.IsRealDirectory {
		if _, err := r.deps.Hooks.Run(ctx, hooks.BeforeRemove, target.Path); err != nil {
			summary.HookErr = err
		}
	} else {
		summary.SkippedHook = true
	}

	result, err := r.deps.Workspace.Remove(target)
	summary.Removed = result.Removed
	summary.CleanupErr = err
	return summary
}

func (r *Runtime) requeueRetriesAfterFetchError(
	cfg config.Config,
	entries []runstate.Retry,
	now time.Time,
) []runstate.Retry {
	requeued := make([]runstate.Retry, 0, len(entries))
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range entries {
		entry.Attempt++
		entry.Error = "retry poll failed"
		entry.DueAt = now.Add(failureRetryDelay(entry.Attempt, cfg.Agent.MaxRetryBackoff))
		r.state.requeueRetry(entry)
		requeued = append(requeued, entry)
	}
	return requeued
}

func (r *Runtime) handleDueRetries(
	ctx context.Context,
	cfg config.Config,
	entries []runstate.Retry,
	candidates []tracker.Issue,
	summary *TickSummary,
) {
	if len(entries) == 0 {
		return
	}
	byID := make(map[string]tracker.Issue, len(candidates))
	for _, issue := range candidates {
		byID[issue.ID] = issue
	}

	for _, entry := range entries {
		issue, ok := byID[entry.IssueID]
		if !ok {
			summary.Retries.Released = append(summary.Retries.Released, SkipSummary{
				IssueID:  entry.IssueID,
				IssueKey: entry.IssueKey,
				Reason:   "retry_issue_not_candidate",
			})
			continue
		}

		r.mu.Lock()
		runtimeSnapshot := r.state.policyRuntimeState()
		r.mu.Unlock()
		eligibility := policy.CheckEligibility(cfg.Tracker, issue, runtimeSnapshot)
		if !eligibility.Allowed {
			summary.Retries.Released = append(summary.Retries.Released, skipFromPolicy(issue, eligibility))
			continue
		}

		skip, runCtx, started := r.tryStart(ctx, issue, cfg, entry.Attempt)
		if !started {
			if skip.Reason == "global_concurrency_limit" || skip.Reason == "state_concurrency_limit" {
				requeued := r.requeueRetryForSlots(cfg, issue, entry)
				summary.Retries.Requeued = append(summary.Retries.Requeued, requeued)
				continue
			}
			summary.Retries.Released = append(summary.Retries.Released, skip)
			continue
		}

		dispatch := DispatchSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Attempt:  entry.Attempt,
		}
		summary.Retries.Dispatched = append(summary.Retries.Dispatched, dispatch)
		summary.Dispatched = append(summary.Dispatched, dispatch)
		go r.runIssue(runCtx, cfg, issue, entry.Attempt)
	}
}

func (r *Runtime) requeueRetryForSlots(
	cfg config.Config,
	issue tracker.Issue,
	entry runstate.Retry,
) runstate.Retry {
	entry.IssueKey = issue.Identifier
	entry.Attempt++
	entry.Error = "no available orchestrator slots"
	entry.DueAt = r.deps.Clock().Add(failureRetryDelay(entry.Attempt, cfg.Agent.MaxRetryBackoff))

	r.mu.Lock()
	r.state.requeueRetry(entry)
	r.mu.Unlock()
	return entry
}

func failureRetryDelay(attempt int, capDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if capDelay <= 0 {
		capDelay = config.DefaultMaxRetryBackoff
	}

	delay := failureRetryBaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= capDelay {
			return capDelay
		}
		if delay > capDelay/2 {
			return capDelay
		}
		delay *= 2
	}
	if delay > capDelay {
		return capDelay
	}
	return delay
}

func stateIn(states []string, state string) bool {
	normalized := normalizeState(state)
	if normalized == "" {
		return false
	}
	for _, candidate := range states {
		if normalizeState(candidate) == normalized {
			return true
		}
	}
	return false
}

func stoppedIssueIDs(stopped []StopSummary) map[string]struct{} {
	result := make(map[string]struct{}, len(stopped))
	for _, item := range stopped {
		if item.IssueID != "" {
			result[item.IssueID] = struct{}{}
		}
	}
	return result
}

func (r *Runtime) setStatus(status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

func (r *Runtime) shouldStopAfterTickError(ctx context.Context, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return true, nil
	}
	if errors.Is(err, ErrMissingDependency) {
		return true, err
	}
	return false, nil
}

func skipFromPolicy(issue tracker.Issue, eligibility policy.Eligibility) SkipSummary {
	return SkipSummary{
		IssueID:  issue.ID,
		IssueKey: issue.Identifier,
		State:    issue.State,
		Reason:   string(eligibility.Reason),
	}
}
