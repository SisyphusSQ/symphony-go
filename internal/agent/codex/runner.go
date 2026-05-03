package codex

import (
	"context"

	"github.com/SisyphusSQ/symphony-go/internal/agent"
	"github.com/SisyphusSQ/symphony-go/internal/config"
)

// Runner adapts the Codex app-server client to the generic agent.Runner
// contract used by the orchestrator.
type Runner struct {
	client *Client
	cfg    config.Codex
}

// NewRunner creates a runner that executes one Codex app-server thread/turn
// for each agent.RunRequest.
func NewRunner(cfg config.Codex, opts ...Option) *Runner {
	return &Runner{
		client: NewClient(opts...),
		cfg:    cfg,
	}
}

// Run implements agent.Runner.
func (r *Runner) Run(ctx context.Context, req agent.RunRequest) (agent.RunResult, error) {
	if r == nil {
		r = NewRunner(config.Codex{})
	}
	client := r.client
	if client == nil {
		client = NewClient()
	}
	result, err := client.Run(ctx, RunRequest{
		Config:        r.cfg,
		WorkspacePath: req.WorkspacePath,
		Prompt:        req.Prompt,
		IssueKey:      req.IssueKey,
	})
	return agent.RunResult{
		SessionID: result.SessionID,
		Summary:   result.Status,
	}, err
}

var _ agent.Runner = (*Runner)(nil)
