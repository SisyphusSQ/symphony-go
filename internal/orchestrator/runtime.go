package orchestrator

import "github.com/SisyphusSQ/symphony-go/internal/config"

// Runtime is the reload-aware surface owned by the future orchestrator loop.
// State mutation is intentionally serialized by the caller until the full event
// loop lands.
type Runtime struct {
	status       Status
	reloader     *config.Reloader
	activeIssues map[string]struct{}
}

// NewRuntime loads the initial workflow config and prepares the runtime state.
func NewRuntime(workflowPath string, opts ...config.Option) (*Runtime, error) {
	reloader, err := config.NewReloader(workflowPath, opts...)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		status:       StatusRunning,
		reloader:     reloader,
		activeIssues: map[string]struct{}{},
	}, nil
}

// Status returns the operator-visible runtime lifecycle state.
func (r *Runtime) Status() Status {
	return r.status
}

// MarkActive records an active issue without coupling reload to dispatch state.
func (r *Runtime) MarkActive(issueID string) {
	if issueID == "" {
		return
	}
	r.activeIssues[issueID] = struct{}{}
}

// ActiveIssueCount returns the number of active issue entries retained by the runtime.
func (r *Runtime) ActiveIssueCount() int {
	return len(r.activeIssues)
}

// ReloadWorkflowIfChanged updates the effective config for future dispatch
// while preserving active issue state.
func (r *Runtime) ReloadWorkflowIfChanged() config.ReloadResult {
	return r.reloader.ReloadIfChanged()
}

// FutureDispatchConfig returns the last known good config used by subsequent
// dispatch decisions, hook executions, and agent launches.
func (r *Runtime) FutureDispatchConfig() config.Config {
	return r.reloader.Current()
}
