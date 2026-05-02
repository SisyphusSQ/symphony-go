package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	output, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("expected root help to succeed: %v", err)
	}

	for _, want := range []string{
		"Usage:",
		"symphony [command]",
		"validate",
		"run",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("root help missing %q:\n%s", want, output)
		}
	}
}

func TestRunHelp(t *testing.T) {
	output, err := executeCommand(t, "run", "--help")
	if err != nil {
		t.Fatalf("expected run help to succeed: %v", err)
	}

	for _, want := range []string{
		"Usage:",
		"symphony run [workflow]",
		"--workflow string",
		"--port int",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("run help missing %q:\n%s", want, output)
		}
	}
}

func TestValidateUsesDefaultWorkflowPath(t *testing.T) {
	t.Chdir(t.TempDir())
	writeWorkflow(t, "WORKFLOW.md")

	output, err := executeCommand(t, "validate")
	if err != nil {
		t.Fatalf("expected validate with default workflow to succeed: %v", err)
	}

	if !strings.Contains(output, `workflow "./WORKFLOW.md" passed startup validation`) {
		t.Fatalf("unexpected validate output:\n%s", output)
	}
}

func TestValidateAcceptsExplicitWorkflowPath(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "custom-WORKFLOW.md")
	writeWorkflow(t, workflowPath)

	tests := []struct {
		name string
		args []string
	}{
		{name: "positional", args: []string{"validate", workflowPath}},
		{name: "flag", args: []string{"validate", "--workflow", workflowPath}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("expected validate to succeed: %v", err)
			}
			if !strings.Contains(output, `workflow "`+workflowPath+`" passed startup validation`) {
				t.Fatalf("unexpected validate output:\n%s", output)
			}
		})
	}
}

func TestValidateRejectsInvalidWorkflowArguments(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	writeWorkflow(t, workflowPath)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "flag and positional",
			args:    []string{"validate", workflowPath, "--workflow", workflowPath},
			wantErr: "provide the workflow path either as an argument or with --workflow, not both",
		},
		{
			name:    "multiple positionals",
			args:    []string{"validate", workflowPath, workflowPath},
			wantErr: "accepts at most 1 arg(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeCommand(t, tt.args...)
			if err == nil {
				t.Fatal("expected validate to fail")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestValidateRejectsMissingDefaultWorkflow(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := executeCommand(t, "validate")
	if err == nil {
		t.Fatal("expected validate with missing default workflow to fail")
	}
	for _, want := range []string{"missing_workflow_file", "./WORKFLOW.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %q", want, err.Error())
		}
	}
}

func TestValidateRejectsDirectoryWorkflowPath(t *testing.T) {
	workflowDir := t.TempDir()

	_, err := executeCommand(t, "validate", workflowDir)
	if err == nil {
		t.Fatal("expected validate with directory workflow to fail")
	}
	for _, want := range []string{"workflow_path_is_directory", workflowDir} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %q", want, err.Error())
		}
	}
}

func TestRunPerformsStartupValidationWithoutStartingRuntime(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	writeWorkflow(t, workflowPath)

	output, err := executeCommand(t, "run", "--port", "1234", "--instance", "dev", workflowPath)
	if err != nil {
		t.Fatalf("expected run startup validation to succeed: %v", err)
	}

	for _, want := range []string{
		`workflow "` + workflowPath + `" passed startup validation; server.port override 1234; instance "dev"`,
		"orchestrator runtime is not implemented in this slice; no workers started",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("run output missing %q:\n%s", want, output)
		}
	}
}

func TestRunSurfacesStartupValidationFailure(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "missing-WORKFLOW.md")

	_, err := executeCommand(t, "run", workflowPath)
	if err == nil {
		t.Fatal("expected run with missing workflow to fail")
	}
	for _, want := range []string{"startup failed", "missing_workflow_file", workflowPath} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %q", want, err.Error())
		}
	}
}

func executeCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String() + stderr.String(), err
}

func writeWorkflow(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("---\ntracker:\n  kind: linear\n---\nPrompt\n"), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
}
