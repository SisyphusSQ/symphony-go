package orchestrator

import (
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/policy"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

// Status is an operator-visible orchestrator lifecycle state.
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusPaused   Status = "paused"
	StatusDraining Status = "draining"
	StatusStopped  Status = "stopped"
)

// RunRecord is the runtime-owned mutable view of one active issue dispatch.
type RunRecord struct {
	IssueID       string
	IssueKey      string
	State         string
	WorkspacePath string
	SessionID     string
	StartedAt     time.Time
}

type runtimeState struct {
	activeIssues map[string]struct{}
	running      map[string]RunRecord
}

func newRuntimeState() runtimeState {
	return runtimeState{
		activeIssues: map[string]struct{}{},
		running:      map[string]RunRecord{},
	}
}

func (s *runtimeState) markActive(issueID string) {
	if issueID == "" {
		return
	}
	s.activeIssues[issueID] = struct{}{}
}

func (s *runtimeState) activeIssueCount() int {
	return len(s.activeIssues)
}

func (s *runtimeState) runningIssueCount() int {
	return len(s.running)
}

func (s *runtimeState) runningByState(state string) int {
	normalized := normalizeState(state)
	count := 0
	for _, record := range s.running {
		if normalizeState(record.State) == normalized {
			count++
		}
	}
	return count
}

func (s *runtimeState) policyRuntimeState() policy.RuntimeState {
	running := make(map[string]struct{}, len(s.running))
	for issueID := range s.running {
		running[issueID] = struct{}{}
	}
	return policy.RuntimeState{
		RunningIssueIDs: running,
		ClaimedIssueIDs: map[string]struct{}{},
	}
}

func (s *runtimeState) start(issue tracker.Issue, workspacePath string, now time.Time) RunRecord {
	record := RunRecord{
		IssueID:       issue.ID,
		IssueKey:      issue.Identifier,
		State:         issue.State,
		WorkspacePath: workspacePath,
		StartedAt:     now,
	}
	s.activeIssues[issue.ID] = struct{}{}
	s.running[issue.ID] = record
	return record
}

func (s *runtimeState) updateSession(issueID string, sessionID string) {
	record, ok := s.running[issueID]
	if !ok {
		return
	}
	record.SessionID = sessionID
	s.running[issueID] = record
}

func (s *runtimeState) finish(issueID string) {
	delete(s.running, issueID)
}

func normalizeState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}
