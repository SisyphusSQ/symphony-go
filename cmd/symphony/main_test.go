package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SisyphusSQ/symphony-go/internal/config"
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

	output, err := executeCommand(t, "run", "--port", "0", "--instance", "dev", workflowPath)
	if err != nil {
		t.Fatalf("expected run startup validation to succeed: %v", err)
	}

	for _, want := range []string{
		`workflow "` + workflowPath + `" passed startup validation; server.port 0 from cli; instance "dev"`,
		"operator HTTP server listening on http://127.0.0.1:",
		"orchestrator runtime loaded; dispatch dependencies are not configured in this CLI slice",
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

func TestResolveServerOptionsHonorsPortPrecedence(t *testing.T) {
	cfg := configForServerTest(4567)

	tests := []struct {
		name                   string
		workflowPortConfigured bool
		cliPort                int
		cliPortSet             bool
		wantEnabled            bool
		wantPort               int
		wantSource             string
	}{
		{
			name:                   "disabled without cli or workflow server port",
			workflowPortConfigured: false,
			wantEnabled:            false,
		},
		{
			name:                   "workflow port enables server",
			workflowPortConfigured: true,
			wantEnabled:            true,
			wantPort:               4567,
			wantSource:             "workflow",
		},
		{
			name:                   "cli port overrides workflow port",
			workflowPortConfigured: true,
			cliPort:                1234,
			cliPortSet:             true,
			wantEnabled:            true,
			wantPort:               1234,
			wantSource:             "cli",
		},
		{
			name:                   "cli ephemeral port overrides workflow port",
			workflowPortConfigured: true,
			cliPort:                0,
			cliPortSet:             true,
			wantEnabled:            true,
			wantPort:               0,
			wantSource:             "cli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveServerOptions(
				cfg,
				tt.workflowPortConfigured,
				tt.cliPort,
				tt.cliPortSet,
				"dev",
			)
			if err != nil {
				t.Fatalf("resolveServerOptions() error = %v", err)
			}
			if got.Enabled != tt.wantEnabled || got.Port != tt.wantPort || got.Source != tt.wantSource ||
				got.BindHost != "127.0.0.1" || got.Instance != "dev" {
				t.Fatalf("server options = %#v", got)
			}
		})
	}
}

func TestResolveServerOptionsRejectsInvalidCLIPort(t *testing.T) {
	if _, err := resolveServerOptions(configForServerTest(0), false, -1, true, ""); err == nil {
		t.Fatal("expected negative CLI port to fail")
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

func configForServerTest(port int) config.Config {
	return config.Config{Server: config.Server{Port: port}}
}

func writeWorkflow(t *testing.T, path string) {
	t.Helper()

	content := strings.Join([]string{
		"---",
		"tracker:",
		"  kind: linear",
		"  api_key: literal-token",
		"  project_slug: symphony-go",
		"---",
		"Prompt",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
}
