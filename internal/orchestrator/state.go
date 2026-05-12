package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/policy"
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
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
	RunID         string
	IssueID       string
	IssueKey      string
	State         string
	WorkspacePath string
	SessionID     string
	Attempt       int
	StartedAt     time.Time
	cancel        context.CancelFunc
}

type runtimeState struct {
	activeIssues map[string]struct{}
	running      map[string]RunRecord
	retries      map[string]runstate.Retry
	suppressions map[string]runstate.Suppression
}

func newRuntimeState() runtimeState {
	return runtimeState{
		activeIssues: map[string]struct{}{},
		running:      map[string]RunRecord{},
		retries:      map[string]runstate.Retry{},
		suppressions: map[string]runstate.Suppression{},
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

func (s *runtimeState) retryIssueCount() int {
	return len(s.retries)
}

func (s *runtimeState) suppressionIssueCount() int {
	return len(s.suppressions)
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
	claimed := make(map[string]struct{}, len(s.retries))
	for issueID := range s.retries {
		claimed[issueID] = struct{}{}
	}
	suppressed := make(map[string]struct{}, len(s.suppressions))
	for issueID := range s.suppressions {
		suppressed[issueID] = struct{}{}
	}
	return policy.RuntimeState{
		RunningIssueIDs:    running,
		ClaimedIssueIDs:    claimed,
		SuppressedIssueIDs: suppressed,
	}
}

func (s *runtimeState) start(
	issue tracker.Issue,
	runID string,
	workspacePath string,
	attempt int,
	now time.Time,
	cancel context.CancelFunc,
) RunRecord {
	record := RunRecord{
		RunID:         runID,
		IssueID:       issue.ID,
		IssueKey:      issue.Identifier,
		State:         issue.State,
		WorkspacePath: workspacePath,
		Attempt:       attempt,
		StartedAt:     now,
		cancel:        cancel,
	}
	s.activeIssues[issue.ID] = struct{}{}
	s.running[issue.ID] = record
	delete(s.retries, issue.ID)
	delete(s.suppressions, issue.ID)
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

func (s *runtimeState) updateWorkspace(issueID string, workspacePath string) {
	record, ok := s.running[issueID]
	if !ok {
		return
	}
	record.WorkspacePath = workspacePath
	s.running[issueID] = record
}

func (s *runtimeState) updateIssue(issue tracker.Issue) {
	record, ok := s.running[issue.ID]
	if !ok {
		return
	}
	if issue.Identifier != "" {
		record.IssueKey = issue.Identifier
	}
	record.State = issue.State
	s.running[issue.ID] = record
}

func (s *runtimeState) finish(issueID string) (RunRecord, bool) {
	record, ok := s.running[issueID]
	if !ok {
		return RunRecord{}, false
	}
	delete(s.running, issueID)
	return record, true
}

func (s *runtimeState) stop(issueID string) (RunRecord, bool) {
	return s.finish(issueID)
}

func (s *runtimeState) scheduleRetry(entry runstate.Retry) {
	if entry.IssueID == "" {
		return
	}
	if entry.Attempt < 1 {
		entry.Attempt = 1
	}
	s.retries[entry.IssueID] = entry
	delete(s.suppressions, entry.IssueID)
}

func (s *runtimeState) requeueRetry(entry runstate.Retry) {
	s.scheduleRetry(entry)
}

func (s *runtimeState) dueRetries(now time.Time) []runstate.Retry {
	due := make([]runstate.Retry, 0)
	for issueID, entry := range s.retries {
		if entry.DueAt.After(now) {
			continue
		}
		due = append(due, entry)
		delete(s.retries, issueID)
	}
	return due
}

func (s *runtimeState) retryEntries() []runstate.Retry {
	entries := make([]runstate.Retry, 0, len(s.retries))
	for _, entry := range s.retries {
		entries = append(entries, entry)
	}
	return entries
}

func (s *runtimeState) runningRecords() []RunRecord {
	records := make([]RunRecord, 0, len(s.running))
	for _, record := range s.running {
		records = append(records, record)
	}
	return records
}

func (s *runtimeState) suppress(record RunRecord, reason string, now time.Time) runstate.Suppression {
	if record.IssueID == "" {
		return runstate.Suppression{}
	}
	existing := s.suppressions[record.IssueID]
	createdAt := existing.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	suppression := runstate.Suppression{
		IssueID:   record.IssueID,
		IssueKey:  record.IssueKey,
		State:     record.State,
		RunID:     record.RunID,
		Reason:    reason,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	s.suppressions[record.IssueID] = suppression
	delete(s.retries, record.IssueID)
	return suppression
}

func (s *runtimeState) addSuppression(suppression runstate.Suppression) {
	if suppression.IssueID == "" {
		return
	}
	s.suppressions[suppression.IssueID] = suppression
}

func (s *runtimeState) clearSuppression(issueID string) (runstate.Suppression, bool) {
	suppression, ok := s.suppressions[issueID]
	if ok {
		delete(s.suppressions, issueID)
	}
	return suppression, ok
}

func (s *runtimeState) suppressionByTarget(target string) (runstate.Suppression, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return runstate.Suppression{}, false
	}
	for _, suppression := range s.suppressions {
		if suppression.IssueID == target || suppression.IssueKey == target {
			return suppression, true
		}
	}
	return runstate.Suppression{}, false
}

func (s *runtimeState) suppressionEntries() []runstate.Suppression {
	entries := make([]runstate.Suppression, 0, len(s.suppressions))
	for _, suppression := range s.suppressions {
		entries = append(entries, suppression)
	}
	return entries
}

func normalizeState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}
