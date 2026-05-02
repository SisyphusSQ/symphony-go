package hooks

import "time"

// Hook describes a configured lifecycle command.
type Hook struct {
	Name    string
	Command string
	Timeout time.Duration
}
