package agent

import "context"

// Runner executes one issue attempt in an already prepared workspace.
type Runner interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type RunRequest struct {
	IssueID       string
	IssueKey      string
	WorkspacePath string
	Prompt        string
}

type RunResult struct {
	SessionID string
	Summary   string
}
