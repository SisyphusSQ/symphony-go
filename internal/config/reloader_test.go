package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReloaderAppliesChangedWorkflow(t *testing.T) {
	workflowPath := writeReloadWorkflow(t, 5000, 2, "Old prompt")

	reloader, err := NewReloader(workflowPath)
	if err != nil {
		t.Fatalf("NewReloader() returned error: %v", err)
	}
	if got := reloader.Current().PromptBody; got != "Old prompt" {
		t.Fatalf("initial PromptBody = %q, want Old prompt", got)
	}

	writeReloadWorkflowAt(t, workflowPath, 7500, 4, "New prompt")

	result := reloader.ReloadIfChanged()
	if result.Status != ReloadApplied {
		t.Fatalf("Status = %q, want %q; err=%v", result.Status, ReloadApplied, result.Err)
	}
	if !result.Changed {
		t.Fatal("Changed = false, want true")
	}
	if result.Config.Polling.Interval != 7500*time.Millisecond {
		t.Fatalf("Polling.Interval = %s, want 7.5s", result.Config.Polling.Interval)
	}
	if result.Config.Agent.MaxConcurrentAgents != 4 {
		t.Fatalf("MaxConcurrentAgents = %d, want 4", result.Config.Agent.MaxConcurrentAgents)
	}
	if result.Config.PromptBody != "New prompt" {
		t.Fatalf("PromptBody = %q, want New prompt", result.Config.PromptBody)
	}
	if !strings.Contains(result.OperatorMessage(), "workflow_reload_applied") {
		t.Fatalf("OperatorMessage() = %q", result.OperatorMessage())
	}

	unchanged := reloader.ReloadIfChanged()
	if unchanged.Status != ReloadUnchanged || unchanged.Changed {
		t.Fatalf("second reload = (%q, %v), want unchanged false", unchanged.Status, unchanged.Changed)
	}
}

func TestReloaderKeepsLastKnownGoodOnInvalidReload(t *testing.T) {
	workflowPath := writeReloadWorkflow(t, 5000, 2, "Stable prompt")

	reloader, err := NewReloader(workflowPath)
	if err != nil {
		t.Fatalf("NewReloader() returned error: %v", err)
	}

	if err := os.WriteFile(workflowPath, []byte(`---
tracker:
  kind: linear
  api_key: literal-token
  project_slug: symphony-go
polling:
  interval_ms: 0
---

Broken prompt
`), 0o644); err != nil {
		t.Fatalf("write invalid workflow: %v", err)
	}

	result := reloader.ReloadIfChanged()
	if result.Status != ReloadInvalid {
		t.Fatalf("Status = %q, want %q", result.Status, ReloadInvalid)
	}
	if !errors.Is(result.Err, ErrReloadInvalid) {
		t.Fatalf("Err = %v, want ErrReloadInvalid", result.Err)
	}
	if result.Config.Polling.Interval != 5*time.Second {
		t.Fatalf("Polling.Interval = %s, want last known good 5s", result.Config.Polling.Interval)
	}
	if result.Config.PromptBody != "Stable prompt" {
		t.Fatalf("PromptBody = %q, want last known good Stable prompt", result.Config.PromptBody)
	}
	if message := result.OperatorMessage(); !strings.Contains(message, "keeping_last_known_good=true") {
		t.Fatalf("OperatorMessage() = %q, want last-known-good marker", message)
	}

	unchangedInvalid := reloader.ReloadIfChanged()
	if unchangedInvalid.Status != ReloadUnchanged {
		t.Fatalf("unchanged invalid content Status = %q, want %q", unchangedInvalid.Status, ReloadUnchanged)
	}

	writeReloadWorkflowAt(t, workflowPath, 9000, 3, "Recovered prompt")
	recovered := reloader.ReloadIfChanged()
	if recovered.Status != ReloadApplied {
		t.Fatalf("recovered Status = %q, want %q; err=%v", recovered.Status, ReloadApplied, recovered.Err)
	}
	if recovered.Config.PromptBody != "Recovered prompt" {
		t.Fatalf("recovered PromptBody = %q, want Recovered prompt", recovered.Config.PromptBody)
	}
}

func TestReloaderCurrentReturnsClone(t *testing.T) {
	workflowPath := writeReloadWorkflow(t, 5000, 2, "Prompt")

	reloader, err := NewReloader(workflowPath)
	if err != nil {
		t.Fatalf("NewReloader() returned error: %v", err)
	}

	cfg := reloader.Current()
	cfg.Tracker.ActiveStates[0] = "Mutated"
	cfg.Agent.MaxConcurrentAgentsByState["todo"] = 99
	cfg.Codex.TurnSandboxPolicy["type"] = "mutated"

	fresh := reloader.Current()
	if fresh.Tracker.ActiveStates[0] == "Mutated" {
		t.Fatal("Current() returned mutable ActiveStates slice")
	}
	if fresh.Agent.MaxConcurrentAgentsByState["todo"] == 99 {
		t.Fatal("Current() returned mutable MaxConcurrentAgentsByState map")
	}
	if fresh.Codex.TurnSandboxPolicy["type"] == "mutated" {
		t.Fatal("Current() returned mutable TurnSandboxPolicy map")
	}
}

func writeReloadWorkflow(t *testing.T, intervalMS int, maxAgents int, prompt string) string {
	t.Helper()

	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	writeReloadWorkflowAt(t, workflowPath, intervalMS, maxAgents, prompt)
	return workflowPath
}

func writeReloadWorkflowAt(t *testing.T, path string, intervalMS int, maxAgents int, prompt string) {
	t.Helper()

	content := strings.TrimLeft(`
---
tracker:
  kind: linear
  api_key: literal-token
  project_slug: symphony-go
polling:
  interval_ms: {interval_ms}
agent:
  max_concurrent_agents: {max_agents}
  max_concurrent_agents_by_state:
    Todo: 1
codex:
  turn_sandbox_policy:
    type: workspaceWrite
---

{prompt}
`, "\n")
	content = strings.ReplaceAll(content, "{interval_ms}", intString(intervalMS))
	content = strings.ReplaceAll(content, "{max_agents}", intString(maxAgents))
	content = strings.ReplaceAll(content, "{prompt}", prompt)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
}

func intString(value int) string {
	return strconv.Itoa(value)
}
