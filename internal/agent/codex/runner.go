package codex

import (
	"context"

	"github.com/SisyphusSQ/symphony-go/internal/agent"
	"github.com/SisyphusSQ/symphony-go/internal/config"
)

// Runner adapts the Codex app-server client to the generic agent runner
// contracts used by the orchestrator.
type Runner struct {
	client *Client
	cfg    config.Codex
}

// NewRunner creates a Codex turn client.
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
	return agent.NewRunner(r).Run(ctx, req)
}

// RunTurn implements agent.TurnClient.
func (r *Runner) RunTurn(ctx context.Context, req agent.TurnRequest) (agent.TurnResult, error) {
	if r == nil {
		r = NewRunner(req.Codex)
	}
	client := r.client
	if client == nil {
		client = NewClient()
	}
	cfg := mergeCodexConfig(r.cfg, req.Codex)
	result, err := client.Run(ctx, RunRequest{
		Config:        cfg,
		Tracker:       req.Tracker,
		WorkspacePath: req.WorkspacePath,
		Prompt:        req.Prompt,
		IssueKey:      req.IssueKey,
	})
	return agent.TurnResult{
		SessionID: result.SessionID,
		ThreadID:  result.ThreadID,
		TurnID:    result.TurnID,
		Status:    result.Status,
		Summary:   result.Status,
		Usage: agent.TokenUsage{
			InputTokens:           result.Usage.InputTokens,
			OutputTokens:          result.Usage.OutputTokens,
			ReasoningOutputTokens: result.Usage.ReasoningOutputTokens,
			TotalTokens:           result.Usage.TotalTokens,
			CachedInputTokens:     result.Usage.CachedInputTokens,
		},
		Events: mapEvents(result.Events),
	}, err
}

var _ agent.Runner = (*Runner)(nil)
var _ agent.TurnClient = (*Runner)(nil)

func mergeCodexConfig(base config.Codex, override config.Codex) config.Codex {
	if codexConfigIsZero(override) {
		return base
	}
	merged := base
	if override.Command != "" {
		merged.Command = override.Command
	}
	if override.ApprovalPolicy != "" {
		merged.ApprovalPolicy = override.ApprovalPolicy
	}
	if override.ThreadSandbox != "" {
		merged.ThreadSandbox = override.ThreadSandbox
	}
	if override.TurnSandboxPolicy != nil {
		merged.TurnSandboxPolicy = override.TurnSandboxPolicy
	}
	if override.ReadTimeout != 0 {
		merged.ReadTimeout = override.ReadTimeout
	}
	if override.TurnTimeout != 0 {
		merged.TurnTimeout = override.TurnTimeout
	}
	merged.StallTimeout = override.StallTimeout
	return merged
}

func codexConfigIsZero(cfg config.Codex) bool {
	return cfg.Command == "" &&
		cfg.ApprovalPolicy == "" &&
		cfg.ThreadSandbox == "" &&
		cfg.TurnSandboxPolicy == nil &&
		cfg.ReadTimeout == 0 &&
		cfg.TurnTimeout == 0 &&
		cfg.StallTimeout == 0
}

func mapEvents(events []Event) []agent.Event {
	if len(events) == 0 {
		return nil
	}
	mapped := make([]agent.Event, 0, len(events))
	for _, event := range events {
		mapped = append(mapped, agent.Event{
			Kind:      string(event.Kind),
			Method:    event.Method,
			Timestamp: event.Timestamp,
			ThreadID:  event.ThreadID,
			TurnID:    event.TurnID,
			Message:   event.Message,
			Payload:   string(event.Payload),
		})
	}
	return mapped
}
