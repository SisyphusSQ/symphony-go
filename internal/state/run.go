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
