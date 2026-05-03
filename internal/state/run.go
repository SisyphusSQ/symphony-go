package state

import "time"

// Run records one issue execution attempt.
type Run struct {
	ID         string
	IssueID    string
	IssueKey   string
	Attempt    int
	StartedAt  time.Time
	FinishedAt *time.Time
}

// Retry records one in-memory retry queue entry.
type Retry struct {
	IssueID  string
	IssueKey string
	Attempt  int
	DueAt    time.Time
	Error    string
}
