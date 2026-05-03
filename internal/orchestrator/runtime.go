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
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
	"github.com/SisyphusSQ/symphony-go/internal/workspace"
)

var ErrMissingDependency = errors.New("missing_orchestrator_dependency")

// WorkspacePreparer is the workspace surface consumed by dispatch.
type WorkspacePreparer interface {
	Prepare(workspace.PrepareRequest) (workspace.Workspace, error)
}

// HookRunner is the lifecycle-hook surface consumed by dispatch.
type HookRunner interface {
	RunAfterCreate(ctx context.Context, cwd string, createdNow bool) (hooks.Result, error)
	Run(ctx context.Context, name hooks.Name, cwd string) (hooks.Result, error)
}

// Dependencies are the side-effecting collaborators needed by the dispatch loop.
type Dependencies struct {
	Tracker   tracker.Client
	Workspace WorkspacePreparer
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
	Reload      config.ReloadResult
	Candidates  int
	Dispatched  []DispatchSummary
	Skipped     []SkipSummary
	TrackerErr  error
	DispatchErr error
}

// DispatchSummary records one issue attempt that was admitted by the tick.
type DispatchSummary struct {
	IssueID  string
	IssueKey string
	State    string
}

// SkipSummary records a candidate that the tick did not dispatch.
type SkipSummary struct {
	IssueID  string
	IssueKey string
	State    string
	Reason   string
}

// RunOnce executes one polling tick.
func (r *Runtime) RunOnce(ctx context.Context) (TickSummary, error) {
	return r.Tick(ctx)
}

// Run starts the orchestrator loop. It performs an immediate tick and then
// repeats at the current effective polling interval.
func (r *Runtime) Run(ctx context.Context) error {
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
	if err := r.DispatchReady(); err != nil {
		summary.DispatchErr = err
		return summary, err
	}
	if err := ctx.Err(); err != nil {
		summary.DispatchErr = err
		return summary, err
	}

	cfg := r.FutureDispatchConfig()
	issues, err := r.deps.Tracker.FetchCandidateIssues(ctx)
	if err != nil {
		summary.TrackerErr = err
		return summary, err
	}
	summary.Candidates = len(issues)

	r.mu.Lock()
	runtimeSnapshot := r.state.policyRuntimeState()
	r.mu.Unlock()

	decisions := policy.EvaluateCandidates(cfg.Tracker, issues, runtimeSnapshot)
	for _, decision := range decisions {
		issue := decision.Issue
		if !decision.Eligibility.Allowed {
			summary.Skipped = append(summary.Skipped, skipFromPolicy(issue, decision.Eligibility))
			continue
		}

		if skip, ok := r.tryStart(issue, cfg); !ok {
			summary.Skipped = append(summary.Skipped, skip)
			continue
		}

		summary.Dispatched = append(summary.Dispatched, DispatchSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
		})
		go r.runIssue(ctx, cfg, issue)
	}

	return summary, nil
}

func (r *Runtime) tryStart(issue tracker.Issue, cfg config.Config) (SkipSummary, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.state.running[issue.ID]; ok {
		return SkipSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Reason:   string(policy.ReasonAlreadyRunning),
		}, false
	}
	if r.state.runningIssueCount() >= cfg.Agent.MaxConcurrentAgents {
		return SkipSummary{
			IssueID:  issue.ID,
			IssueKey: issue.Identifier,
			State:    issue.State,
			Reason:   "global_concurrency_limit",
		}, false
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
		}, false
	}

	r.state.start(issue, "", r.deps.Clock())
	return SkipSummary{}, true
}

func (r *Runtime) runIssue(ctx context.Context, cfg config.Config, issue tracker.Issue) {
	var sessionID string
	defer func() {
		r.mu.Lock()
		if sessionID != "" {
			r.state.updateSession(issue.ID, sessionID)
		}
		r.state.finish(issue.ID)
		r.mu.Unlock()
	}()

	ws, err := r.deps.Workspace.Prepare(workspace.PrepareRequest{
		IssueID:         issue.ID,
		IssueIdentifier: issue.Identifier,
		WorkflowPath:    cfg.WorkflowRef,
	})
	if err != nil {
		return
	}
	r.mu.Lock()
	record := r.state.running[issue.ID]
	record.WorkspacePath = ws.Path
	r.state.running[issue.ID] = record
	r.mu.Unlock()

	if _, err := r.deps.Hooks.RunAfterCreate(ctx, ws.Path, ws.CreatedNow); err != nil {
		return
	}
	if _, err := r.deps.Hooks.Run(ctx, hooks.BeforeRun, ws.Path); err != nil {
		return
	}

	result, runErr := r.deps.Runner.Run(ctx, agent.RunRequest{
		IssueID:       issue.ID,
		IssueKey:      issue.Identifier,
		WorkspacePath: ws.Path,
		Prompt:        cfg.PromptBody,
	})
	sessionID = result.SessionID
	if _, err := r.deps.Hooks.Run(ctx, hooks.AfterRun, ws.Path); err != nil {
		return
	}
	if runErr != nil {
		return
	}
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
