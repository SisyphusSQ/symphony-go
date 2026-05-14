package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

func TestIssueRunnerRendersStrictPromptWithIssueAndAttempt(t *testing.T) {
	attempt := 2
	workspace := t.TempDir()
	client := &fakeTurnClient{
		results: []TurnResult{{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Status:   "completed",
			Usage:    TokenUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
		}},
	}

	result, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue: tracker.Issue{
			ID:          "issue-1",
			Identifier:  "TOO-127",
			Title:       "Agent runner",
			Description: "Render prompt",
			State:       "Todo",
			Labels:      []string{"runner", "codex"},
			BlockedBy: []tracker.BlockerRef{{
				ID:         "blocker-1",
				Identifier: "TOO-126",
				State:      "Done",
			}},
			Comments: []tracker.IssueComment{
				{ID: "comment-root", Body: "top-level discussion", ThreadRootID: "comment-root"},
				{ID: "comment-reply", Body: "reply discussion", ParentID: "comment-root", ThreadRootID: "comment-root", Depth: 1},
			},
		},
		Attempt:       &attempt,
		WorkspacePath: workspace,
		PromptTemplate: strings.Join([]string{
			"Issue {{ issue.identifier }}",
			"Title {{ issue.title }}",
			"Description {{ issue.description }}",
			"Labels {{ issue.labels }}",
			"Blockers {{ issue.blocked_by }}",
			"Comments {{ issue.comments }}",
			"Attempt {{ attempt }}",
		}, "\n"),
		MaxTurns: 1,
		Codex: config.Codex{
			Command:      "fake codex",
			ReadTimeout:  11 * time.Millisecond,
			TurnTimeout:  12 * time.Millisecond,
			StallTimeout: 13 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	requests := client.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("turn requests = %d, want 1", len(requests))
	}
	prompt := requests[0].Prompt
	for _, want := range []string{
		"Issue TOO-127",
		"Title Agent runner",
		"Description Render prompt",
		`Labels ["runner","codex"]`,
		`"identifier":"TOO-126"`,
		`"parent_id":"comment-root"`,
		`"is_reply":true`,
		"Attempt 2",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, prompt)
		}
	}
	if requests[0].WorkspacePath != workspace {
		t.Fatalf("WorkspacePath = %q, want %q", requests[0].WorkspacePath, workspace)
	}
	if requests[0].Codex.ReadTimeout != 11*time.Millisecond ||
		requests[0].Codex.TurnTimeout != 12*time.Millisecond ||
		requests[0].Codex.StallTimeout != 13*time.Millisecond {
		t.Fatalf("Codex timeouts = %#v", requests[0].Codex)
	}
	if result.SessionID != "thread-1-turn-1" {
		t.Fatalf("SessionID = %q, want derived thread-turn id", result.SessionID)
	}
	if result.Metadata.TurnCount != 1 || result.Metadata.Attempt == nil ||
		*result.Metadata.Attempt != attempt || result.Metadata.Usage.TotalTokens != 30 {
		t.Fatalf("Metadata = %#v", result.Metadata)
	}
}

func TestIssueRunnerFirstAttemptRendersAttemptAsEmpty(t *testing.T) {
	client := &fakeTurnClient{results: []TurnResult{{SessionID: "session-1", Status: "completed"}}}

	_, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-127"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "first={{ attempt }} issue={{ issue.identifier }}",
		MaxTurns:       1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	requests := client.requestsSnapshot()
	if got := requests[0].Prompt; got != "first= issue=TOO-127" {
		t.Fatalf("prompt = %q, want empty attempt rendering", got)
	}
}

func TestIssueRunnerDoesNotReparseRenderedIssueText(t *testing.T) {
	client := &fakeTurnClient{results: []TurnResult{{SessionID: "session-1", Status: "completed"}}}

	_, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue: tracker.Issue{
			ID:          "issue-1",
			Identifier:  "TOO-127",
			Description: "literal {{ not_a_template }} text",
		},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "description={{ issue.description }}",
		MaxTurns:       1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	requests := client.requestsSnapshot()
	if got := requests[0].Prompt; got != "description=literal {{ not_a_template }} text" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestIssueRunnerFailsStrictlyOnMissingVariable(t *testing.T) {
	client := &fakeTurnClient{}

	_, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-127"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "missing {{ issue.not_a_field }}",
		MaxTurns:       1,
	})
	if !errors.Is(err, ErrTemplateRender) {
		t.Fatalf("error = %v, want ErrTemplateRender", err)
	}
	if len(client.requestsSnapshot()) != 0 {
		t.Fatal("turn client was called after render failure")
	}
}

func TestIssueRunnerFailsStrictlyOnUnknownFilter(t *testing.T) {
	client := &fakeTurnClient{}

	_, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-127"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "filtered {{ issue.identifier | upcase }}",
		MaxTurns:       1,
	})
	if !errors.Is(err, ErrTemplateRender) {
		t.Fatalf("error = %v, want ErrTemplateRender", err)
	}
	if len(client.requestsSnapshot()) != 0 {
		t.Fatal("turn client was called after filter failure")
	}
}

func TestIssueRunnerHonorsMaxTurnsAndContinuationPrompt(t *testing.T) {
	client := &fakeTurnClient{results: []TurnResult{
		{SessionID: "session-1", Status: "completed", Continue: true},
		{SessionID: "session-2", Status: "completed", Continue: true},
		{SessionID: "session-3", Status: "completed", Continue: true},
	}}

	result, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-127"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "original {{ issue.identifier }}",
		MaxTurns:       2,
	})
	if !errors.Is(err, ErrGuardrailExceeded) {
		t.Fatalf("Run error = %v, want ErrGuardrailExceeded", err)
	}
	requests := client.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("turn requests = %d, want max_turns 2", len(requests))
	}
	if requests[0].Prompt != "original TOO-127" {
		t.Fatalf("first prompt = %q", requests[0].Prompt)
	}
	if requests[1].Prompt == requests[0].Prompt ||
		!strings.Contains(requests[1].Prompt, "continuation turn 2 of 2") {
		t.Fatalf("continuation prompt = %q", requests[1].Prompt)
	}
	if result.Metadata.TurnCount != 2 || result.SessionID != "session-2" {
		t.Fatalf("result = %#v", result)
	}
	if !result.Metadata.Guardrail.Exceeded || result.Metadata.Guardrail.Reason != "max_turns" {
		t.Fatalf("guardrail = %#v, want max_turns exceeded", result.Metadata.Guardrail)
	}
}

func TestIssueRunnerValidatesWorkspaceBeforeRendering(t *testing.T) {
	client := &fakeTurnClient{}

	_, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-127"},
		WorkspacePath:  "relative/path",
		PromptTemplate: "issue {{ issue.identifier }}",
		MaxTurns:       1,
	})
	if !errors.Is(err, ErrInvalidRunRequest) {
		t.Fatalf("error = %v, want ErrInvalidRunRequest", err)
	}
	if len(client.requestsSnapshot()) != 0 {
		t.Fatal("turn client was called after invalid workspace")
	}
}

func TestIssueRunnerRejectsNegativeMaxTurns(t *testing.T) {
	client := &fakeTurnClient{}

	_, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-127"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "issue {{ issue.identifier }}",
		MaxTurns:       -1,
	})
	if !errors.Is(err, ErrInvalidRunRequest) {
		t.Fatalf("error = %v, want ErrInvalidRunRequest", err)
	}
	if len(client.requestsSnapshot()) != 0 {
		t.Fatal("turn client was called after invalid max turns")
	}
}

func TestIssueRunnerDoesNotStopWhenTokenGuardrailIsDisabled(t *testing.T) {
	client := &fakeTurnClient{results: []TurnResult{{
		SessionID: "session-1",
		Status:    "completed",
		Usage:     TokenUsage{TotalTokens: 5_788_393},
		Events: []Event{{
			Kind:    "token_usage_updated",
			Message: "usage",
		}},
	}}}

	result, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-145"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "issue {{ issue.identifier }}",
		MaxTurns:       1,
		MaxTotalTokens: 0,
	})
	if err != nil {
		t.Fatalf("Run error = %v, want nil with token guardrail disabled", err)
	}
	if result.Metadata.Guardrail.Exceeded {
		t.Fatalf("guardrail = %#v, want disabled", result.Metadata.Guardrail)
	}
	if result.Metadata.Usage.TotalTokens != 5_788_393 {
		t.Fatalf("TotalTokens = %d, want observed usage retained", result.Metadata.Usage.TotalTokens)
	}
}

func TestIssueRunnerStopsWhenTokenGuardrailIsExceeded(t *testing.T) {
	client := &fakeTurnClient{results: []TurnResult{{
		SessionID: "session-1",
		Status:    "completed",
		Usage:     TokenUsage{TotalTokens: 101},
		Events: []Event{{
			Kind:    "token_usage_updated",
			Message: "usage",
		}},
	}}}

	result, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:          tracker.Issue{ID: "issue-1", Identifier: "TOO-134"},
		WorkspacePath:  t.TempDir(),
		PromptTemplate: "issue {{ issue.identifier }}",
		MaxTurns:       2,
		MaxTotalTokens: 100,
	})
	if !errors.Is(err, ErrGuardrailExceeded) {
		t.Fatalf("Run error = %v, want ErrGuardrailExceeded", err)
	}
	if result.Metadata.Guardrail.Reason != "max_total_tokens" ||
		result.Metadata.Guardrail.Limit != "100" ||
		result.Metadata.Guardrail.Actual != "101" {
		t.Fatalf("guardrail = %#v", result.Metadata.Guardrail)
	}
	if len(result.Metadata.Events) != 1 || result.Metadata.Events[0].Kind != "token_usage_updated" {
		t.Fatalf("metadata events = %#v, want propagated token event", result.Metadata.Events)
	}
}

func TestIssueRunnerStopsWhenEstimatedCostGuardrailIsExceeded(t *testing.T) {
	client := &fakeTurnClient{results: []TurnResult{{
		SessionID: "session-1",
		Status:    "completed",
		Usage:     TokenUsage{TotalTokens: 2_000_000},
	}}}

	result, err := NewRunner(client).Run(context.Background(), RunRequest{
		Issue:                   tracker.Issue{ID: "issue-1", Identifier: "TOO-134"},
		WorkspacePath:           t.TempDir(),
		PromptTemplate:          "issue {{ issue.identifier }}",
		MaxTurns:                1,
		MaxCostUSD:              0.10,
		CostPerMillionTokensUSD: 0.20,
		MaxTotalTokens:          10_000_000,
	})
	if !errors.Is(err, ErrGuardrailExceeded) {
		t.Fatalf("Run error = %v, want ErrGuardrailExceeded", err)
	}
	if result.Metadata.Guardrail.Reason != "max_cost_usd" {
		t.Fatalf("guardrail = %#v, want max_cost_usd", result.Metadata.Guardrail)
	}
}

type fakeTurnClient struct {
	mu       sync.Mutex
	requests []TurnRequest
	results  []TurnResult
	errs     []error
}

func (client *fakeTurnClient) RunTurn(_ context.Context, req TurnRequest) (TurnResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	client.requests = append(client.requests, req)
	index := len(client.requests) - 1
	var result TurnResult
	if index < len(client.results) {
		result = client.results[index]
	}
	var err error
	if index < len(client.errs) {
		err = client.errs[index]
	}
	return result, err
}

func (client *fakeTurnClient) requestsSnapshot() []TurnRequest {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]TurnRequest(nil), client.requests...)
}
