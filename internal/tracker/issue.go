package tracker

import (
	"context"
	"time"
)

// Client is the read-only tracker surface consumed by the future orchestrator.
type Client interface {
	FetchCandidateIssues(ctx context.Context) ([]Issue, error)
	FetchIssuesByStates(ctx context.Context, stateNames []string) ([]Issue, error)
	FetchIssueStatesByIDs(ctx context.Context, issueIDs []string) ([]Issue, error)
}

// Issue is the normalized tracker record consumed by orchestration and prompts.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	Priority    *int
	State       string
	BranchName  string
	URL         string
	Labels      []string
	BlockedBy   []BlockerRef
	Comments    []IssueComment
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

// IssueComment is the normalized issue discussion context exposed to prompts.
type IssueComment struct {
	ID           string
	Body         string
	ParentID     string
	ThreadRootID string
	Depth        int
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

// BlockerRef captures the small amount of dependency state needed for dispatch.
type BlockerRef struct {
	ID         string
	Identifier string
	State      string
}
