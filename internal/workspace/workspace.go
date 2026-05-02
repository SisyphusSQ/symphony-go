package workspace

// Workspace identifies a filesystem directory assigned to one issue.
type Workspace struct {
	Path         string
	Key          string
	CreatedNow   bool
	IssueID      string
	IssueKey     string
	WorkflowPath string
}
