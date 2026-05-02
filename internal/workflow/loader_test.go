package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadParsesFrontMatterAndPromptBody(t *testing.T) {
	workflowPath := writeWorkflow(t, `---
tracker:
  kind: linear
  active_states:
    - Todo
    - In Progress
polling:
  interval_ms: 5000
---

# Mission

Run the issue.
`)

	definition, err := Load(workflowPath)
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if definition.Path != workflowPath {
		t.Fatalf("Path = %q, want %q", definition.Path, workflowPath)
	}
	if got := definition.Config["tracker"].(map[string]any)["kind"]; got != "linear" {
		t.Fatalf("tracker.kind = %v, want linear", got)
	}
	if got := definition.Config["polling"].(map[string]any)["interval_ms"]; got != 5000 {
		t.Fatalf("polling.interval_ms = %v, want 5000", got)
	}
	if want := "# Mission\n\nRun the issue."; definition.PromptTemplate != want {
		t.Fatalf("PromptTemplate = %q, want %q", definition.PromptTemplate, want)
	}
}

func TestLoadRejectsMissingFrontMatter(t *testing.T) {
	workflowPath := writeWorkflow(t, "# Prompt\n")

	err := loadError(t, workflowPath)
	assertWorkflowError(t, err, ErrMissingFrontMatter, workflowPath, "front matter")
}

func TestLoadRejectsInvalidFrontMatterYAML(t *testing.T) {
	workflowPath := writeWorkflow(t, `---
tracker: [
---

Prompt
`)

	err := loadError(t, workflowPath)
	assertWorkflowError(t, err, ErrInvalidFrontMatterYAML, workflowPath, "front matter")
}

func TestLoadRejectsNonMapFrontMatter(t *testing.T) {
	workflowPath := writeWorkflow(t, `---
- tracker
---

Prompt
`)

	err := loadError(t, workflowPath)
	assertWorkflowError(t, err, ErrNonMapFrontMatter, workflowPath, "map/object")
}

func TestLoadRejectsEmptyPromptBody(t *testing.T) {
	workflowPath := writeWorkflow(t, `---
tracker:
  kind: linear
---
`+strings.Repeat(" ", 3)+"\n")

	err := loadError(t, workflowPath)
	assertWorkflowError(t, err, ErrEmptyPromptBody, workflowPath, "prompt body")
}

func TestLoadRejectsUnterminatedFrontMatter(t *testing.T) {
	workflowPath := writeWorkflow(t, `---
tracker:
  kind: linear
`)

	err := loadError(t, workflowPath)
	assertWorkflowError(t, err, ErrUnterminatedFrontMatter, workflowPath, "closing delimiter")
}

func loadError(t *testing.T, workflowPath string) error {
	t.Helper()

	_, err := Load(workflowPath)
	if err == nil {
		t.Fatal("expected Load() to fail")
	}
	return err
}

func assertWorkflowError(t *testing.T, err error, want error, workflowPath string, detail string) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
	for _, part := range []string{workflowPath, detail} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("expected error %q to contain %q", err.Error(), part)
		}
	}
}

func writeWorkflow(t *testing.T, content string) string {
	t.Helper()

	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
	return workflowPath
}
