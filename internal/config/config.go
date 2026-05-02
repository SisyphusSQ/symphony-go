package config

import "time"

// Config is the future typed view consumed by the orchestrator.
type Config struct {
	Tracker     Tracker
	Polling     Polling
	Server      Server
	Workspace   Workspace
	Hooks       Hooks
	Agent       Agent
	Codex       Codex
	PromptBody  string
	WorkflowRef string
}

type Tracker struct {
	Kind           string
	APIKey         string
	ProjectSlug    string
	ActiveStates   []string
	TerminalStates []string
}

type Polling struct {
	Interval time.Duration
}

type Server struct {
	Port int
}

type Workspace struct {
	Root string
}

type Hooks struct {
	AfterCreate  string
	BeforeRun    string
	AfterRun     string
	BeforeRemove string
	Timeout      time.Duration
}

type Agent struct {
	MaxConcurrentAgents        int
	MaxTurns                   int
	MaxConcurrentAgentsByState map[string]int
}

type Codex struct {
	Command           string
	ThreadSandbox     string
	TurnTimeout       time.Duration
	ReadTimeout       time.Duration
	StallTimeout      time.Duration
	TurnSandboxPolicy map[string]any
}
