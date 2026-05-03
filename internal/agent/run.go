package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

var (
	ErrInvalidRunRequest = errors.New("invalid_agent_run_request")
	ErrMissingTurnClient = errors.New("missing_agent_turn_client")
)

// Runner executes one issue attempt in an already prepared workspace.
type Runner interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

// TurnClient executes one rendered coding-agent turn.
type TurnClient interface {
	RunTurn(ctx context.Context, req TurnRequest) (TurnResult, error)
}

// IssueRunner renders one issue attempt and drives rendered turns through a
// TurnClient.
type IssueRunner struct {
	client TurnClient
}

// NewRunner creates an orchestrator-facing agent runner.
func NewRunner(client TurnClient) *IssueRunner {
	return &IssueRunner{client: client}
}

type RunRequest struct {
	Issue          tracker.Issue
	Attempt        *int
	IssueID        string
	IssueKey       string
	WorkspacePath  string
	Prompt         string
	PromptTemplate string
	MaxTurns       int
	Codex          config.Codex
}

type TurnRequest struct {
	Issue         tracker.Issue
	Attempt       *int
	IssueID       string
	IssueKey      string
	WorkspacePath string
	Prompt        string
	TurnNumber    int
	MaxTurns      int
	Codex         config.Codex
}

type RunResult struct {
	SessionID string
	Summary   string
	Metadata  RunMetadata
}

type TurnResult struct {
	SessionID string
	ThreadID  string
	TurnID    string
	Status    string
	Summary   string
	Continue  bool
	Usage     TokenUsage
}

type RunMetadata struct {
	IssueID         string
	IssueIdentifier string
	WorkspacePath   string
	Attempt         *int
	TurnCount       int
	MaxTurns        int
	SessionID       string
	ThreadID        string
	TurnID          string
	Status          string
	Summary         string
	Usage           TokenUsage
}

type TokenUsage struct {
	InputTokens           int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
	CachedInputTokens     int64
}

// AttemptFromNumber returns nil for the first run and a stable pointer for
// retry or continuation attempts.
func AttemptFromNumber(attempt int) *int {
	if attempt <= 0 {
		return nil
	}
	value := attempt
	return &value
}

// Run implements Runner.
func (runner *IssueRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if runner == nil || runner.client == nil {
		return RunResult{}, ErrMissingTurnClient
	}

	normalized := normalizeRunRequest(req)
	if err := validateRunRequest(normalized); err != nil {
		return RunResult{}, err
	}

	prompt, err := RenderPrompt(normalized.PromptTemplate, normalized.Issue, normalized.Attempt)
	if err != nil {
		return RunResult{}, err
	}

	metadata := RunMetadata{
		IssueID:         normalized.Issue.ID,
		IssueIdentifier: normalized.Issue.Identifier,
		WorkspacePath:   normalized.WorkspacePath,
		Attempt:         cloneAttempt(normalized.Attempt),
		MaxTurns:        normalized.MaxTurns,
	}

	var result RunResult
	for turnNumber := 1; turnNumber <= normalized.MaxTurns; turnNumber++ {
		turnResult, turnErr := runner.client.RunTurn(ctx, TurnRequest{
			Issue:         normalized.Issue,
			Attempt:       cloneAttempt(normalized.Attempt),
			IssueID:       normalized.Issue.ID,
			IssueKey:      normalized.Issue.Identifier,
			WorkspacePath: normalized.WorkspacePath,
			Prompt:        prompt,
			TurnNumber:    turnNumber,
			MaxTurns:      normalized.MaxTurns,
			Codex:         normalized.Codex,
		})

		metadata.TurnCount = turnNumber
		metadata.SessionID = firstNonEmpty(turnResult.SessionID, sessionID(turnResult.ThreadID, turnResult.TurnID))
		metadata.ThreadID = turnResult.ThreadID
		metadata.TurnID = turnResult.TurnID
		metadata.Status = turnResult.Status
		metadata.Summary = firstNonEmpty(turnResult.Summary, turnResult.Status)
		metadata.Usage = turnResult.Usage

		result = RunResult{
			SessionID: metadata.SessionID,
			Summary:   metadata.Summary,
			Metadata:  metadata,
		}
		if turnErr != nil {
			return result, turnErr
		}
		if !turnResult.Continue || turnNumber == normalized.MaxTurns {
			return result, nil
		}
		prompt = continuationPrompt(normalized.Issue, turnNumber+1, normalized.MaxTurns)
	}

	return result, nil
}

func normalizeRunRequest(req RunRequest) RunRequest {
	normalized := req
	if normalized.Issue.ID == "" {
		normalized.Issue.ID = normalized.IssueID
	}
	if normalized.Issue.Identifier == "" {
		normalized.Issue.Identifier = normalized.IssueKey
	}
	if normalized.IssueID == "" {
		normalized.IssueID = normalized.Issue.ID
	}
	if normalized.IssueKey == "" {
		normalized.IssueKey = normalized.Issue.Identifier
	}
	if normalized.PromptTemplate == "" {
		normalized.PromptTemplate = normalized.Prompt
	}
	if normalized.MaxTurns == 0 {
		normalized.MaxTurns = config.DefaultMaxTurns
	}
	return normalized
}

func validateRunRequest(req RunRequest) error {
	if strings.TrimSpace(req.WorkspacePath) == "" {
		return fmt.Errorf("%w: workspace path is required", ErrInvalidRunRequest)
	}
	if !filepath.IsAbs(req.WorkspacePath) {
		return fmt.Errorf("%w: workspace path must be absolute", ErrInvalidRunRequest)
	}
	if strings.TrimSpace(req.PromptTemplate) == "" {
		return fmt.Errorf("%w: prompt template is required", ErrInvalidRunRequest)
	}
	if req.MaxTurns <= 0 {
		return fmt.Errorf("%w: max turns must be positive", ErrInvalidRunRequest)
	}
	return nil
}

func continuationPrompt(issue tracker.Issue, turnNumber int, maxTurns int) string {
	issueKey := firstNonEmpty(issue.Identifier, issue.ID, "the current issue")
	return fmt.Sprintf(
		"Continue working on Linear issue `%s` from the existing thread context. "+
			"This is continuation turn %d of %d in the same worker session. "+
			"Do not resend the original task prompt; continue the current issue scope, "+
			"verify progress, and write back when complete.",
		issueKey,
		turnNumber,
		maxTurns,
	)
}

func cloneAttempt(attempt *int) *int {
	if attempt == nil {
		return nil
	}
	value := *attempt
	return &value
}

func sessionID(threadID string, turnID string) string {
	if threadID == "" || turnID == "" {
		return ""
	}
	return threadID + "-" + turnID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
