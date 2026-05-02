package orchestrator

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
)

func TestRuntimeReloadPreservesActiveStateAndUpdatesFutureDispatchConfig(t *testing.T) {
	workflowPath := writeRuntimeWorkflow(t, 5000, 2, "Old prompt")

	runtime, err := NewRuntime(workflowPath)
	if err != nil {
		t.Fatalf("NewRuntime() returned error: %v", err)
	}
	runtime.MarkActive("issue-1")

	before := runtime.FutureDispatchConfig()
	if before.PromptBody != "Old prompt" {
		t.Fatalf("initial PromptBody = %q, want Old prompt", before.PromptBody)
	}

	writeRuntimeWorkflowAt(t, workflowPath, 11000, 5, "New prompt")

	result := runtime.ReloadWorkflowIfChanged()
	if result.Status != config.ReloadApplied {
		t.Fatalf("Status = %q, want %q; err=%v", result.Status, config.ReloadApplied, result.Err)
	}
	if runtime.ActiveIssueCount() != 1 {
		t.Fatalf("ActiveIssueCount() = %d, want 1", runtime.ActiveIssueCount())
	}

	future := runtime.FutureDispatchConfig()
	if future.Polling.Interval != 11*time.Second {
		t.Fatalf("future Polling.Interval = %s, want 11s", future.Polling.Interval)
	}
	if future.Agent.MaxConcurrentAgents != 5 {
		t.Fatalf("future MaxConcurrentAgents = %d, want 5", future.Agent.MaxConcurrentAgents)
	}
	if future.PromptBody != "New prompt" {
		t.Fatalf("future PromptBody = %q, want New prompt", future.PromptBody)
	}
}

func writeRuntimeWorkflow(t *testing.T, intervalMS int, maxAgents int, prompt string) string {
	t.Helper()

	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	writeRuntimeWorkflowAt(t, workflowPath, intervalMS, maxAgents, prompt)
	return workflowPath
}

func writeRuntimeWorkflowAt(t *testing.T, path string, intervalMS int, maxAgents int, prompt string) {
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
---

{prompt}
`, "\n")
	content = strings.ReplaceAll(content, "{interval_ms}", strconv.Itoa(intervalMS))
	content = strings.ReplaceAll(content, "{max_agents}", strconv.Itoa(maxAgents))
	content = strings.ReplaceAll(content, "{prompt}", prompt)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
}
