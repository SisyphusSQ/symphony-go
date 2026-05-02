package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "default", path: "", want: DefaultPath},
		{name: "explicit", path: "/tmp/custom-WORKFLOW.md", want: "/tmp/custom-WORKFLOW.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolvePath(tt.path); got != tt.want {
				t.Fatalf("ResolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestRequireReadableAcceptsReadableFile(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	if err := os.WriteFile(workflowPath, []byte("Prompt\n"), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}

	if err := RequireReadable(workflowPath); err != nil {
		t.Fatalf("RequireReadable() returned unexpected error: %v", err)
	}
}

func TestRequireReadableRejectsMissingFile(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "missing-WORKFLOW.md")

	err := RequireReadable(workflowPath)
	if err == nil {
		t.Fatal("expected missing workflow file to fail")
	}
	if !errors.Is(err, ErrMissingWorkflowFile) {
		t.Fatalf("expected ErrMissingWorkflowFile, got %v", err)
	}
	if !strings.Contains(err.Error(), workflowPath) {
		t.Fatalf("expected error to include path %q, got %q", workflowPath, err.Error())
	}
}

func TestRequireReadableRejectsDirectory(t *testing.T) {
	workflowPath := t.TempDir()

	err := RequireReadable(workflowPath)
	if err == nil {
		t.Fatal("expected directory workflow path to fail")
	}
	if !errors.Is(err, ErrWorkflowPathIsDirectory) {
		t.Fatalf("expected ErrWorkflowPathIsDirectory, got %v", err)
	}
	if !strings.Contains(err.Error(), workflowPath) {
		t.Fatalf("expected error to include path %q, got %q", workflowPath, err.Error())
	}
}
