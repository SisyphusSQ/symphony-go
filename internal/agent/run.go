package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

var (
	ErrInvalidRunRequest = errors.New("invalid_agent_run_request")
	ErrMissingTurnClient = errors.New("missing_agent_turn_client")
	ErrGuardrailExceeded = errors.New("guardrail_exceeded")
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
	Issue                   tracker.Issue
	Attempt                 *int
	IssueID                 string
	IssueKey                string
	WorkspacePath           string
	Prompt                  string
	PromptTemplate          string
	MaxTurns                int
	MaxRunDuration          time.Duration
	MaxTotalTokens          int64
	MaxCostUSD              float64
	CostPerMillionTokensUSD float64
	Tracker                 config.Tracker
	Codex                   config.Codex
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
	Tracker       config.Tracker
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
	Events    []Event
}

type Event struct {
	Kind      string
	Method    string
	Timestamp time.Time
	ThreadID  string
	TurnID    string
	Message   string
	Payload   string
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
	Events          []Event
	Guardrail       GuardrailDecision
}

type TokenUsage struct {
	InputTokens           int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
	CachedInputTokens     int64
}

type GuardrailDecision struct {
	Exceeded bool
	Reason   string
	Limit    string
	Actual   string
}

type GuardrailError struct {
	Decision GuardrailDecision
}

func (err *GuardrailError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: reason=%s limit=%s actual=%s",
		ErrGuardrailExceeded,
		err.Decision.Reason,
		err.Decision.Limit,
		err.Decision.Actual,
	)
}

func (err *GuardrailError) Is(target error) bool {
	return target == ErrGuardrailExceeded
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

	startedAt := time.Now()
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
			Tracker:       normalized.Tracker,
			Codex:         normalized.Codex,
		})

		metadata.TurnCount = turnNumber
		metadata.SessionID = firstNonEmpty(turnResult.SessionID, sessionID(turnResult.ThreadID, turnResult.TurnID))
		metadata.ThreadID = turnResult.ThreadID
		metadata.TurnID = turnResult.TurnID
		metadata.Status = turnResult.Status
		metadata.Summary = firstNonEmpty(turnResult.Summary, turnResult.Status)
		metadata.Usage = turnResult.Usage
		metadata.Events = append(metadata.Events, cloneEvents(turnResult.Events)...)

		result = RunResult{
			SessionID: metadata.SessionID,
			Summary:   metadata.Summary,
			Metadata:  metadata,
		}
		if turnErr != nil {
			return result, turnErr
		}
		if decision := evaluateGuardrails(normalized, metadata, startedAt, turnResult.Continue); decision.Exceeded {
			metadata.Guardrail = decision
			result.Metadata = metadata
			return result, &GuardrailError{Decision: decision}
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
	if normalized.MaxRunDuration == 0 {
		normalized.MaxRunDuration = config.DefaultMaxRunDuration
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

func evaluateGuardrails(
	req RunRequest,
	metadata RunMetadata,
	startedAt time.Time,
	wantsContinuation bool,
) GuardrailDecision {
	if wantsContinuation && metadata.TurnCount >= req.MaxTurns {
		return GuardrailDecision{
			Exceeded: true,
			Reason:   "max_turns",
			Limit:    fmt.Sprintf("%d", req.MaxTurns),
			Actual:   fmt.Sprintf("%d", metadata.TurnCount),
		}
	}
	if req.MaxRunDuration > 0 {
		elapsed := time.Since(startedAt)
		if elapsed > req.MaxRunDuration {
			return GuardrailDecision{
				Exceeded: true,
				Reason:   "max_run_duration",
				Limit:    req.MaxRunDuration.String(),
				Actual:   elapsed.String(),
			}
		}
	}
	if req.MaxTotalTokens > 0 && metadata.Usage.TotalTokens > req.MaxTotalTokens {
		return GuardrailDecision{
			Exceeded: true,
			Reason:   "max_total_tokens",
			Limit:    fmt.Sprintf("%d", req.MaxTotalTokens),
			Actual:   fmt.Sprintf("%d", metadata.Usage.TotalTokens),
		}
	}
	if req.MaxCostUSD > 0 && req.CostPerMillionTokensUSD > 0 {
		actual := float64(metadata.Usage.TotalTokens) / 1_000_000 * req.CostPerMillionTokensUSD
		if actual > req.MaxCostUSD {
			return GuardrailDecision{
				Exceeded: true,
				Reason:   "max_cost_usd",
				Limit:    fmt.Sprintf("%.6f", req.MaxCostUSD),
				Actual:   fmt.Sprintf("%.6f", actual),
			}
		}
	}
	return GuardrailDecision{}
}

func cloneEvents(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	cloned := make([]Event, len(events))
	copy(cloned, events)
	return cloned
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
