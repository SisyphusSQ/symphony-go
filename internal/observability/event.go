package observability

import "time"

// Event is the structured log unit shared by orchestration packages.
type Event struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]any
}
