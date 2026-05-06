package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/agent"
	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/hooks"
	"github.com/SisyphusSQ/symphony-go/internal/observability"
	"github.com/SisyphusSQ/symphony-go/internal/policy"
	"github.com/SisyphusSQ/symphony-go/internal/safety"
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
	Tracker    tracker.Client
	Workspace  WorkspaceManager
	Hooks      HookRunner
	Runner     agent.Runner
	Logger     observability.Logger
	StateStore runstate.Store
	Clock      func() time.Time
}

// Runtime is the reload-aware surface and the single owner of mutable
// orchestrator state.
type Runtime struct {
	mu        sync.Mutex
	status    Status
	reloader  *config.Reloader
	deps      Dependencies
	state     runtimeState
	recovered bool
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
	if deps.Logger == nil {
		deps.Logger = observability.DiscardLogger()
	}
	cfg := reloader.Current()
	if deps.StateStore == nil && cfg.StateStore.Path != "" {
		store, err := runstate.OpenSQLiteStore(
			cfg.StateStore.Path,
			runstate.WithInstanceID(cfg.StateStore.InstanceID),
		)
		if err != nil {
			return nil, err
		}
		deps.StateStore = store
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

// StateQueryStore returns the optional durable state read model for operator APIs.
func (r *Runtime) StateQueryStore() runstate.QueryStore {
	r.mu.Lock()
	defer r.mu.Unlock()
	queryStore, _ := r.deps.StateStore.(runstate.QueryStore)
	return queryStore
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

// Snapshot returns a read-only operator view of active runs, queued retries,
// and the current orchestrator lifecycle state.
func (r *Runtime) Snapshot() observability.Snapshot {
	now := r.deps.Clock()
	r.mu.Lock()
	defer r.mu.Unlock()

	activeRuns := make([]observability.RunSnapshot, 0, len(r.state.running))
	for _, record := range r.state.running {
		activeRuns = append(activeRuns, observability.RunSnapshot{
			RunID:           record.RunID,
			IssueID:         record.IssueID,
			IssueIdentifier: record.IssueKey,
			SessionID:       record.SessionID,
			State:           record.State,
			RunStatus:       observability.RunStatusRunning,
			Attempt:         record.Attempt,
			WorkspacePath:   record.WorkspacePath,
			StartedAt:       record.StartedAt,
			SecondsRunning:  now.Sub(record.StartedAt).Seconds(),
		})
	}
	sort.Slice(activeRuns, func(i, j int) bool {
		return snapshotRunKey(activeRuns[i]) < snapshotRunKey(activeRuns[j])
	})

	retryQueue := make([]observability.RetrySnapshot, 0, len(r.state.retries))
	for _, entry := range r.state.retries {
		retryQueue = append(retryQueue, observability.RetrySnapshot{
			RunID:           entry.RunID,
			IssueID:         entry.IssueID,
			IssueIdentifier: entry.IssueKey,
			RetryState:      observability.RetryStateForError(entry.Error),
			Attempt:         entry.Attempt,
			DueAt:           entry.DueAt,
			Error:           entry.Error,
		})
	}
	sort.Slice(retryQueue, func(i, j int) bool {
		return snapshotRetryKey(retryQueue[i]) < snapshotRetryKey(retryQueue[j])
	})

	return observability.Snapshot{
		GeneratedAt:    now,
		LifecycleState: string(r.status),
		ActiveRuns:     activeRuns,
		RetryQueue:     retryQueue,
	}
}

// RecoverState seeds in-memory retry state from durable startup recovery once.
func (r *Runtime) RecoverState(ctx context.Context) StartupRecoverySummary {
	r.mu.Lock()
	if r.recovered || r.deps.StateStore == nil {
		r.recovered = true
		r.mu.Unlock()
		return StartupRecoverySummary{}
	}
	store := r.deps.StateStore
	now := r.deps.Clock()
	r.mu.Unlock()

	snapshot, err := store.RecoverStartup(ctx, now)
	if err != nil {
		return StartupRecoverySummary{Err: err}
	}

	r.mu.Lock()
	for _, retry := range snapshot.Retries {
		r.state.scheduleRetry(retry)
	}
	r.recovered = true
	r.mu.Unlock()

	return StartupRecoverySummary{
		InterruptedRuns:  len(snapshot.InterruptedRuns),
		RecoveredRetries: len(snapshot.Retries),
	}
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
	Recovery       StartupRecoverySummary
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

// StartupRecoverySummary records durable state restored during startup.
type StartupRecoverySummary struct {
	InterruptedRuns  int
	RecoveredRetries int
	Err              error
}

// RunOnce executes one polling tick.
func (r *Runtime) RunOnce(ctx context.Context) (TickSummary, error) {
	return r.Tick(ctx)
}

// Run starts the orchestrator loop. It performs an immediate tick and then
// repeats at the current effective polling interval.
func (r *Runtime) Run(ctx context.Context) error {
	if recovery := r.RecoverState(ctx); recovery.Err != nil {
		r.setStatus(StatusStopped)
		return recovery.Err
	}
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
		r.emitEvent(ctx, observability.Event{
			Level:     observability.LevelWarn,
			Type:      observability.EventOrchestratorRunFailed,
			Message:   "action=tick run_status=failed",
			RunStatus: observability.RunStatusFailed,
			Error:     err.Error(),
		})
		return summary, err
	}

	cfg := r.FutureDispatchConfig()
	summary.Recovery = r.RecoverState(ctx)
	if summary.Recovery.Err != nil {
		summary.DispatchErr = summary.Recovery.Err
		r.emitEvent(ctx, observability.Event{
			Level:     observability.LevelError,
			Type:      observability.EventOrchestratorRunFailed,
			Message:   "action=startup_recovery run_status=failed",
			RunStatus: observability.RunStatusFailed,
			Error:     summary.Recovery.Err.Error(),
		})
		return summary, summary.Recovery.Err
	}
	if err := r.DispatchReady(); err != nil {
		summary.DispatchErr = err
		r.emitEvent(ctx, observability.Event{
			Level:     observability.LevelError,
			Type:      observability.EventOrchestratorMissingDeps,
			Message:   "action=dispatch_ready run_status=failed",
			RunStatus: observability.RunStatusFailed,
			Error:     err.Error(),
		})
		return summary, err
	}
	summary.Reconciliation = r.reconcileRunning(ctx, cfg)
	stoppedThisTick := stoppedIssueIDs(summary.Reconciliation.Stopped)
	if status, paused := r.controlDispatchPaused(); paused {
		r.emitEvent(ctx, observability.Event{
			Level:   observability.LevelInfo,
			Type:    observability.EventOrchestratorDispatchSkipped,
			Message: "action=dispatch skipped=true",
			Fields: map[string]any{
				"reason":          "operator_control",
				"lifecycle_state": string(status),
			},
		})
		return summary, nil
	}

	now := r.deps.Clock()
	r.mu.Lock()
	dueRetries := r.state.dueRetries(now)
	r.mu.Unlock()
	summary.Retries.Due = len(dueRetries)

	issues, err := r.deps.Tracker.FetchCandidateIssues(ctx)
	if err != nil {
		summary.TrackerErr = err
		r.emitEvent(ctx, observability.Event{
			Level:   observability.LevelError,
			Type:    observability.EventTrackerCandidateFetchFailed,
			Message: "action=fetch_candidates completed=false",
			Error:   err.Error(),
		})
		if len(dueRetries) > 0 {
			summary.Retries.Requeued = r.requeueRetriesAfterFetchError(ctx, cfg, dueRetries, now)
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
			r.emitEvent(ctx, runEvent(issue, observability.EventOrchestratorRunDispatched, observability.RunStatusRunning, 0, "action=dispatch run_status=running"))
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
	now := r.deps.Clock()
	runID := runstate.NewID("run")
	if r.deps.StateStore != nil {
		err := r.deps.StateStore.ClaimRun(ctx, runstate.Run{
			ID:          runID,
			IssueID:     issue.ID,
			IssueKey:    issue.Identifier,
			Status:      runstate.RunStatusRunning,
			Attempt:     attempt,
			WorkflowRef: cfg.WorkflowRef,
			StartedAt:   now,
		}, now.Add(cfg.StateStore.LeaseTimeout))
		if errors.Is(err, runstate.ErrRunClaimed) {
			cancel()
			return SkipSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Reason:   string(policy.ReasonAlreadyClaimed),
			}, nil, false
		}
		if err != nil {
			cancel()
			return SkipSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Reason:   "state_store_claim_failed",
			}, nil, false
		}
	}
	r.state.start(issue, runID, "", attempt, now, cancel)
	return SkipSummary{}, runCtx, true
}

func (r *Runtime) runIssue(ctx context.Context, cfg config.Config, issue tracker.Issue, attempt int) {
	var sessionID string
	var metadata agent.RunMetadata
	normalExit := false
	retryable := true
	exitErr := "worker exited without result"
	defer func() {
		r.completeRun(ctx, issue.ID, sessionID, metadata, normalExit, retryable, exitErr, cfg)
	}()

	r.emitEvent(ctx, runEvent(issue, observability.EventOrchestratorRunStarted, observability.RunStatusRunning, attempt, "action=run_start run_status=running"))

	ws, err := r.deps.Workspace.Prepare(workspace.PrepareRequest{
		IssueID:         issue.ID,
		IssueIdentifier: issue.Identifier,
		WorkflowPath:    cfg.WorkflowRef,
	})
	if err != nil {
		exitErr = err.Error()
		r.emitEvent(ctx, runEventWithError(
			issue,
			observability.EventWorkspacePrepareFailed,
			observability.RunStatusFailed,
			attempt,
			"action=workspace_prepare run_status=failed",
			err,
		))
		return
	}
	r.mu.Lock()
	r.state.updateWorkspace(issue.ID, ws.Path)
	record := r.state.running[issue.ID]
	r.mu.Unlock()
	if err := r.persistRunUpdate(ctx, record); err != nil {
		exitErr = err.Error()
		r.emitEvent(ctx, runEventWithError(
			issue,
			observability.EventOrchestratorRunFailed,
			observability.RunStatusFailed,
			attempt,
			"action=state_store_update run_status=failed",
			err,
		))
		return
	}
	r.emitEvent(ctx, runEventWithFields(
		issue,
		observability.EventWorkspacePrepared,
		observability.RunStatusRunning,
		attempt,
		"action=workspace_prepare completed=true",
		map[string]any{"workspace_path": ws.Path, "created_now": ws.CreatedNow},
	))

	if _, err := r.deps.Hooks.RunAfterCreate(ctx, ws.Path, ws.CreatedNow); err != nil {
		exitErr = err.Error()
		r.emitEvent(ctx, hookEvent(issue, hooks.AfterCreate, attempt, err))
		return
	}
	if _, err := r.deps.Hooks.Run(ctx, hooks.BeforeRun, ws.Path); err != nil {
		exitErr = err.Error()
		r.emitEvent(ctx, hookEvent(issue, hooks.BeforeRun, attempt, err))
		return
	}

	result, runErr := r.deps.Runner.Run(ctx, agent.RunRequest{
		Issue:                   issue,
		Attempt:                 agent.AttemptFromNumber(attempt),
		IssueID:                 issue.ID,
		IssueKey:                issue.Identifier,
		WorkspacePath:           ws.Path,
		Prompt:                  cfg.PromptBody,
		PromptTemplate:          cfg.PromptBody,
		MaxTurns:                cfg.Agent.MaxTurns,
		MaxRunDuration:          cfg.Agent.MaxRunDuration,
		MaxTotalTokens:          cfg.Agent.MaxTotalTokens,
		MaxCostUSD:              cfg.Agent.MaxCostUSD,
		CostPerMillionTokensUSD: cfg.Agent.CostPerMillionTokensUSD,
		Tracker:                 cfg.Tracker,
		Codex:                   cfg.Codex,
	})
	sessionID = result.SessionID
	metadata = result.Metadata
	if sessionID == "" {
		sessionID = metadata.SessionID
	}
	if metadata.SessionID == "" {
		metadata.SessionID = sessionID
	}
	if _, err := r.deps.Hooks.Run(ctx, hooks.AfterRun, ws.Path); err != nil {
		r.emitEvent(ctx, hookEvent(issue, hooks.AfterRun, attempt, err))
	}
	if runErr != nil {
		exitErr = runErr.Error()
		if errors.Is(runErr, agent.ErrGuardrailExceeded) {
			retryable = false
			event := sessionRunEvent(
				issue,
				sessionID,
				observability.EventGuardrailExceeded,
				observability.RunStatusStopped,
				attempt,
				"action=guardrail_exceeded run_status=stopped",
				runErr,
			)
			event.Level = observability.LevelWarn
			event.Fields = guardrailFields(metadata.Guardrail)
			r.emitEvent(ctx, event)
			return
		}
		r.emitEvent(ctx, sessionRunEvent(
			issue,
			sessionID,
			observability.EventAgentRunFailed,
			observability.RunStatusFailed,
			attempt,
			"action=agent_run run_status=failed",
			runErr,
		))
		return
	}
	r.emitEvent(ctx, sessionRunEvent(
		issue,
		sessionID,
		observability.EventAgentRunCompleted,
		observability.RunStatusCompleted,
		attempt,
		"action=agent_run run_status=completed",
		nil,
	))
	normalExit = true
	exitErr = ""
}

func (r *Runtime) completeRun(
	ctx context.Context,
	issueID string,
	sessionID string,
	metadata agent.RunMetadata,
	normalExit bool,
	retryable bool,
	exitErr string,
	cfg config.Config,
) {
	var event observability.Event

	r.mu.Lock()
	if sessionID != "" {
		r.state.updateSession(issueID, sessionID)
	}
	record, ok := r.state.finish(issueID)
	if !ok {
		r.mu.Unlock()
		return
	}
	if sessionID == "" {
		sessionID = record.SessionID
	}
	issue := tracker.Issue{ID: record.IssueID, Identifier: record.IssueKey, State: record.State}

	now := r.deps.Clock()
	if normalExit {
		r.state.completed[issueID] = struct{}{}
		entry := runstate.Retry{
			RunID:    record.RunID,
			IssueID:  issueID,
			IssueKey: record.IssueKey,
			Attempt:  1,
			DueAt:    now.Add(continuationRetryDelay),
		}
		r.state.scheduleRetry(entry)
		dueAt := entry.DueAt
		event = sessionRunEvent(
			issue,
			sessionID,
			observability.EventRetryScheduled,
			observability.RunStatusCompleted,
			entry.Attempt,
			"action=retry_schedule retry_state=continuation run_status=completed",
			nil,
		)
		event.RetryState = observability.RetryStateContinuation
		event.RetryAttempt = entry.Attempt
		event.RetryDueAt = &dueAt
		r.mu.Unlock()
		r.persistSession(ctx, record, sessionID, metadata, now)
		r.persistAgentEvents(ctx, cfg, record, sessionID, metadata.Events, now)
		r.persistRunCompletion(ctx, record, runstate.RunStatusCompleted, now, "")
		r.persistRetry(ctx, entry)
		r.emitEvent(ctx, event)
		return
	}

	if !retryable {
		event = sessionRunEvent(
			issue,
			sessionID,
			observability.EventGuardrailExceeded,
			observability.RunStatusStopped,
			record.Attempt,
			"action=guardrail_stop run_status=stopped",
			nil,
		)
		event.Level = observability.LevelWarn
		event.Error = exitErr
		event.Fields = guardrailFields(metadata.Guardrail)
		r.mu.Unlock()
		r.persistSession(ctx, record, sessionID, metadata, now)
		r.persistAgentEvents(ctx, cfg, record, sessionID, metadata.Events, now)
		r.persistRunCompletion(ctx, record, runstate.RunStatusStopped, now, exitErr)
		r.emitEvent(ctx, event)
		return
	}

	attempt := record.Attempt + 1
	if attempt < 1 {
		attempt = 1
	}
	delay := failureRetryDelay(attempt, cfg.Agent.MaxRetryBackoff)
	entry := runstate.Retry{
		RunID:     record.RunID,
		IssueID:   issueID,
		IssueKey:  record.IssueKey,
		Attempt:   attempt,
		DueAt:     now.Add(delay),
		BackoffMS: int(delay / time.Millisecond),
		Error:     exitErr,
	}
	r.state.scheduleRetry(entry)
	dueAt := entry.DueAt
	event = sessionRunEvent(
		issue,
		sessionID,
		observability.EventRetryScheduled,
		observability.RunStatusFailed,
		entry.Attempt,
		"action=retry_schedule retry_state=failure run_status=failed",
		nil,
	)
	event.RetryState = observability.RetryStateFailure
	event.RetryAttempt = entry.Attempt
	event.RetryDueAt = &dueAt
	event.Error = exitErr
	if metadata.TurnCount > 0 {
		event.Fields = map[string]any{
			"turn_count":          metadata.TurnCount,
			"input_tokens":        metadata.Usage.InputTokens,
			"output_tokens":       metadata.Usage.OutputTokens,
			"total_tokens":        metadata.Usage.TotalTokens,
			"workspace_path":      metadata.WorkspacePath,
			"issue_identifier":    metadata.IssueIdentifier,
			"metadata_session_id": metadata.SessionID,
		}
	}
	r.mu.Unlock()
	r.persistSession(ctx, record, sessionID, metadata, now)
	r.persistAgentEvents(ctx, cfg, record, sessionID, metadata.Events, now)
	r.persistRunCompletion(ctx, record, runstate.RunStatusFailed, now, exitErr)
	r.persistRetry(ctx, entry)
	r.emitEvent(ctx, event)
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
		r.emitEvent(ctx, observability.Event{
			Level:   observability.LevelError,
			Type:    observability.EventTrackerStateRefreshFailed,
			Message: "action=refresh_issue_states completed=false",
			Error:   err.Error(),
		})
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
			r.persistRunCompletion(ctx, stopped, runstate.RunStatusStopped, r.deps.Clock(), "terminal_state")
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
			event := runEvent(issue, observability.EventOrchestratorRunStopped, observability.RunStatusStopped, stopped.Attempt, "action=reconcile_stop run_status=stopped")
			event.SessionID = stopped.SessionID
			event.Fields = map[string]any{"reason": "terminal_state"}
			r.emitEvent(ctx, event)
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
			stopped, ok := r.stopRunning(record.IssueID)
			if !ok {
				continue
			}
			r.persistRunCompletion(ctx, stopped, runstate.RunStatusStopped, r.deps.Clock(), "inactive_state")
			summary.Stopped = append(summary.Stopped, StopSummary{
				IssueID:  issue.ID,
				IssueKey: issue.Identifier,
				State:    issue.State,
				Reason:   "inactive_state",
			})
			event := runEvent(issue, observability.EventOrchestratorRunStopped, observability.RunStatusStopped, stopped.Attempt, "action=reconcile_stop run_status=stopped")
			event.SessionID = stopped.SessionID
			event.Fields = map[string]any{"reason": "inactive_state"}
			r.emitEvent(ctx, event)
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

func (r *Runtime) persistRunUpdate(ctx context.Context, record RunRecord) error {
	if r.deps.StateStore == nil || record.RunID == "" {
		return nil
	}
	return r.deps.StateStore.UpdateRun(ctx, runstate.Run{
		ID:            record.RunID,
		IssueID:       record.IssueID,
		IssueKey:      record.IssueKey,
		Attempt:       record.Attempt,
		WorkspacePath: record.WorkspacePath,
		SessionID:     record.SessionID,
		UpdatedAt:     r.deps.Clock(),
	})
}

func (r *Runtime) persistRunCompletion(
	ctx context.Context,
	record RunRecord,
	status runstate.RunStatus,
	at time.Time,
	errText string,
) {
	if r.deps.StateStore == nil || record.RunID == "" {
		return
	}
	_ = r.persistRunUpdate(ctx, record)
	_ = r.deps.StateStore.CompleteRun(ctx, record.RunID, status, at, errText)
}

func (r *Runtime) persistRetry(ctx context.Context, entry runstate.Retry) {
	if r.deps.StateStore == nil {
		return
	}
	_ = r.deps.StateStore.UpsertRetry(ctx, entry)
}

func (r *Runtime) deleteRetry(ctx context.Context, issueID string) {
	if r.deps.StateStore == nil {
		return
	}
	_ = r.deps.StateStore.DeleteRetry(ctx, issueID)
}

func (r *Runtime) persistSession(
	ctx context.Context,
	record RunRecord,
	sessionID string,
	metadata agent.RunMetadata,
	now time.Time,
) {
	if r.deps.StateStore == nil || record.RunID == "" || sessionID == "" {
		return
	}
	_ = r.deps.StateStore.RecordSession(ctx, runstate.Session{
		ID:              sessionID,
		RunID:           record.RunID,
		IssueID:         record.IssueID,
		IssueKey:        record.IssueKey,
		ThreadID:        metadata.ThreadID,
		TurnID:          metadata.TurnID,
		Status:          metadata.Status,
		Summary:         metadata.Summary,
		WorkspacePath:   firstNonEmpty(metadata.WorkspacePath, record.WorkspacePath),
		InputTokens:     metadata.Usage.InputTokens,
		OutputTokens:    metadata.Usage.OutputTokens,
		ReasoningTokens: metadata.Usage.ReasoningOutputTokens,
		TotalTokens:     metadata.Usage.TotalTokens,
		CachedTokens:    metadata.Usage.CachedInputTokens,
		TurnCount:       metadata.TurnCount,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
}

func (r *Runtime) persistAgentEvents(
	ctx context.Context,
	cfg config.Config,
	record RunRecord,
	sessionID string,
	events []agent.Event,
	now time.Time,
) {
	if r.deps.StateStore == nil || record.RunID == "" || len(events) == 0 {
		return
	}
	redactor := safety.NewRedactor(cfg)
	for _, event := range events {
		if event.Kind == "" {
			continue
		}
		createdAt := event.Timestamp
		if createdAt.IsZero() {
			createdAt = now
		}
		payload := map[string]any{
			"kind":    event.Kind,
			"method":  event.Method,
			"message": event.Message,
			"thread":  event.ThreadID,
			"turn":    event.TurnID,
		}
		if event.Payload != "" {
			payload["payload"] = json.RawMessage(redactor.JSON(event.Payload))
		}
		payloadJSON, err := json.Marshal(redactor.Any(payload))
		if err != nil {
			continue
		}
		_ = r.deps.StateStore.RecordEvent(ctx, runstate.Event{
			ID:          runstate.NewID("event"),
			RunID:       record.RunID,
			IssueID:     record.IssueID,
			IssueKey:    record.IssueKey,
			SessionID:   sessionID,
			Type:        "agent." + event.Kind,
			PayloadJSON: string(payloadJSON),
			CreatedAt:   createdAt,
		})
	}
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
		r.emitEvent(ctx, observability.Event{
			Level:   observability.LevelError,
			Type:    observability.EventTrackerTerminalFetchFailed,
			Message: "action=fetch_terminal_issues completed=false",
			Error:   err.Error(),
		})
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
	eventType := observability.EventWorkspaceCleanup
	level := observability.LevelInfo
	message := "action=workspace_cleanup completed=true"
	errorText := ""
	if err != nil {
		eventType = observability.EventWorkspaceCleanupFailed
		level = observability.LevelError
		message = "action=workspace_cleanup completed=false"
		errorText = err.Error()
	}
	r.emitEvent(ctx, observability.Event{
		Level:           level,
		Type:            eventType,
		Message:         message,
		IssueID:         issue.ID,
		IssueIdentifier: issue.Identifier,
		Error:           errorText,
		Fields: map[string]any{
			"workspace_path": target.Path,
			"existed":        target.Exists,
			"removed":        result.Removed,
			"hook_error":     errorString(summary.HookErr),
			"skipped_hook":   summary.SkippedHook,
		},
	})
	return summary
}

func (r *Runtime) requeueRetriesAfterFetchError(
	ctx context.Context,
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
		delay := failureRetryDelay(entry.Attempt, cfg.Agent.MaxRetryBackoff)
		entry.DueAt = now.Add(delay)
		entry.BackoffMS = int(delay / time.Millisecond)
		r.state.requeueRetry(entry)
		r.persistRetry(ctx, entry)
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
			r.deleteRetry(ctx, entry.IssueID)
			r.emitEvent(ctx, retryEventFromEntry(
				entry,
				observability.EventRetryReleased,
				observability.RetryStateReleased,
				"action=retry_release retry_state=released reason=retry_issue_not_candidate",
			))
			continue
		}

		r.mu.Lock()
		runtimeSnapshot := r.state.policyRuntimeState()
		r.mu.Unlock()
		eligibility := policy.CheckEligibility(cfg.Tracker, issue, runtimeSnapshot)
		if !eligibility.Allowed {
			summary.Retries.Released = append(summary.Retries.Released, skipFromPolicy(issue, eligibility))
			r.deleteRetry(ctx, entry.IssueID)
			event := retryEventForIssue(
				issue,
				entry,
				observability.EventRetryReleased,
				observability.RetryStateReleased,
				"action=retry_release retry_state=released",
			)
			event.Fields = map[string]any{"reason": string(eligibility.Reason)}
			r.emitEvent(ctx, event)
			continue
		}

		skip, runCtx, started := r.tryStart(ctx, issue, cfg, entry.Attempt)
		if !started {
			if skip.Reason == "global_concurrency_limit" || skip.Reason == "state_concurrency_limit" {
				requeued := r.requeueRetryForSlots(ctx, cfg, issue, entry)
				summary.Retries.Requeued = append(summary.Retries.Requeued, requeued)
				event := retryEventForIssue(
					issue,
					requeued,
					observability.EventRetryRequeued,
					observability.RetryStateRequeued,
					"action=retry_requeue retry_state=requeued",
				)
				event.Fields = map[string]any{"reason": skip.Reason}
				r.emitEvent(ctx, event)
				continue
			}
			summary.Retries.Released = append(summary.Retries.Released, skip)
			r.deleteRetry(ctx, entry.IssueID)
			event := retryEventForIssue(
				issue,
				entry,
				observability.EventRetryReleased,
				observability.RetryStateReleased,
				"action=retry_release retry_state=released",
			)
			event.Fields = map[string]any{"reason": skip.Reason}
			r.emitEvent(ctx, event)
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
		r.emitEvent(ctx, retryEventForIssue(
			issue,
			entry,
			observability.EventRetryDispatched,
			observability.RetryStateForError(entry.Error),
			"action=retry_dispatch run_status=running",
		))
		go r.runIssue(runCtx, cfg, issue, entry.Attempt)
	}
}

func (r *Runtime) requeueRetryForSlots(
	ctx context.Context,
	cfg config.Config,
	issue tracker.Issue,
	entry runstate.Retry,
) runstate.Retry {
	entry.IssueKey = issue.Identifier
	entry.Attempt++
	entry.Error = "no available orchestrator slots"
	delay := failureRetryDelay(entry.Attempt, cfg.Agent.MaxRetryBackoff)
	entry.DueAt = r.deps.Clock().Add(delay)
	entry.BackoffMS = int(delay / time.Millisecond)

	r.mu.Lock()
	r.state.requeueRetry(entry)
	r.mu.Unlock()
	r.persistRetry(ctx, entry)
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

func (r *Runtime) emitEvent(ctx context.Context, event observability.Event) {
	if r == nil || r.deps.Logger == nil {
		return
	}
	if event.Time.IsZero() && r.deps.Clock != nil {
		event.Time = r.deps.Clock()
	}
	event = safety.ConfigEvent(r.FutureDispatchConfig(), event)
	_ = r.deps.Logger.Log(ctx, event)
	if r.deps.StateStore == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = r.deps.StateStore.RecordEvent(ctx, runstate.Event{
		ID:          runstate.NewID("event"),
		IssueID:     event.IssueID,
		IssueKey:    event.IssueIdentifier,
		SessionID:   event.SessionID,
		Type:        string(event.Type),
		PayloadJSON: string(payload),
		CreatedAt:   event.Time,
	})
}

func runEvent(
	issue tracker.Issue,
	eventType observability.EventType,
	runStatus string,
	attempt int,
	message string,
) observability.Event {
	return observability.Event{
		Type:            eventType,
		Message:         message,
		IssueID:         issue.ID,
		IssueIdentifier: issue.Identifier,
		RunStatus:       runStatus,
		RetryAttempt:    attempt,
	}
}

func runEventWithError(
	issue tracker.Issue,
	eventType observability.EventType,
	runStatus string,
	attempt int,
	message string,
	err error,
) observability.Event {
	event := runEvent(issue, eventType, runStatus, attempt, message)
	event.Level = observability.LevelError
	event.Error = errorString(err)
	return event
}

func runEventWithFields(
	issue tracker.Issue,
	eventType observability.EventType,
	runStatus string,
	attempt int,
	message string,
	fields map[string]any,
) observability.Event {
	event := runEvent(issue, eventType, runStatus, attempt, message)
	event.Fields = fields
	return event
}

func sessionRunEvent(
	issue tracker.Issue,
	sessionID string,
	eventType observability.EventType,
	runStatus string,
	attempt int,
	message string,
	err error,
) observability.Event {
	event := runEvent(issue, eventType, runStatus, attempt, message)
	event.SessionID = sessionID
	if err != nil {
		event.Level = observability.LevelError
		event.Error = err.Error()
	}
	return event
}

func hookEvent(issue tracker.Issue, name hooks.Name, attempt int, err error) observability.Event {
	event := runEvent(
		issue,
		observability.EventHookFailed,
		observability.RunStatusFailed,
		attempt,
		"action=hook run_status=failed",
	)
	event.Level = observability.LevelError
	event.Error = errorString(err)
	event.Fields = map[string]any{"hook": name.String()}
	var hookErr *hooks.Error
	if errors.As(err, &hookErr) {
		event.Fields["command"] = hookErr.Result.Command
		event.Fields["stdout"] = hookErr.Result.Stdout
		event.Fields["stderr"] = hookErr.Result.Stderr
		event.Fields["exit_code"] = hookErr.Result.ExitCode
		event.Fields["timed_out"] = hookErr.Result.TimedOut
		event.Fields["duration_ms"] = hookErr.Result.Duration.Milliseconds()
	}
	return event
}

func guardrailFields(decision agent.GuardrailDecision) map[string]any {
	if !decision.Exceeded {
		return map[string]any{}
	}
	return map[string]any{
		"reason": decision.Reason,
		"limit":  decision.Limit,
		"actual": decision.Actual,
	}
}

func retryEventForIssue(
	issue tracker.Issue,
	entry runstate.Retry,
	eventType observability.EventType,
	retryState string,
	message string,
) observability.Event {
	event := retryEventFromEntry(entry, eventType, retryState, message)
	event.IssueID = firstNonEmpty(issue.ID, entry.IssueID)
	event.IssueIdentifier = firstNonEmpty(issue.Identifier, entry.IssueKey)
	return event
}

func retryEventFromEntry(
	entry runstate.Retry,
	eventType observability.EventType,
	retryState string,
	message string,
) observability.Event {
	dueAt := entry.DueAt
	runStatus := observability.RunStatusRunning
	if retryState == observability.RetryStateReleased || retryState == observability.RetryStateRequeued {
		runStatus = observability.RunStatusSkipped
	}
	return observability.Event{
		Type:            eventType,
		Message:         message,
		IssueID:         entry.IssueID,
		IssueIdentifier: entry.IssueKey,
		RunStatus:       runStatus,
		RetryState:      retryState,
		RetryAttempt:    entry.Attempt,
		RetryDueAt:      &dueAt,
		Error:           entry.Error,
	}
}

func snapshotRunKey(run observability.RunSnapshot) string {
	return firstNonEmpty(run.IssueIdentifier, run.IssueID)
}

func snapshotRetryKey(retry observability.RetrySnapshot) string {
	return firstNonEmpty(retry.IssueIdentifier, retry.IssueID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func skipFromPolicy(issue tracker.Issue, eligibility policy.Eligibility) SkipSummary {
	return SkipSummary{
		IssueID:  issue.ID,
		IssueKey: issue.Identifier,
		State:    issue.State,
		Reason:   string(eligibility.Reason),
	}
}
