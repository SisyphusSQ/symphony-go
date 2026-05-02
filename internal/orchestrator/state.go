package orchestrator

// Status is an operator-visible orchestrator lifecycle state.
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusPaused   Status = "paused"
	StatusDraining Status = "draining"
	StatusStopped  Status = "stopped"
)
