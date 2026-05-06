package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/orchestrator"
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
		"tui",
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

func TestTUIStatusCommandUsesAPIV1State(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		if gotPath != "/api/v1/state" {
			t.Fatalf("unexpected path: %s", gotPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"generated_at":"2026-05-06T09:00:00Z",
			"lifecycle":{"state":"running"},
			"ready":{"ok":true},
			"counts":{"running":0,"retrying":0},
			"running":[],
			"retrying":[],
			"latest_completed_or_failed":[],
			"tokens":{"total_tokens":0},
			"runtime":{"total_seconds":0},
			"rate_limit":{"latest":null},
			"state_store":{"configured":false}
		}`))
	}))
	defer server.Close()

	output, err := executeCommand(t, "tui", "--endpoint", server.URL, "--width", "100")
	if err != nil {
		t.Fatalf("expected tui status to succeed: %v", err)
	}
	if !strings.Contains(output, "SYMPHONY STATUS") || !strings.Contains(output, "RUNNING") {
		t.Fatalf("unexpected tui output:\n%s", output)
	}
	if gotPath != "/api/v1/state" {
		t.Fatalf("path = %q, want /api/v1/state", gotPath)
	}
}

func TestTUIDetailRunUsesLatestAndEvents(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RequestURI() {
		case "/api/v1/issues/TOO-141/latest":
			_, _ = w.Write([]byte(`{
				"metadata":{"run_id":"run-141","status":"running","attempt":1,"started_at":"2026-05-06T08:55:00Z","runtime_seconds":30},
				"issue":{"id":"issue-141","identifier":"TOO-141"},
				"workspace":{},
				"session":{"id":"session-abcdef1234567890"},
				"token_totals":{"total_tokens":12}
			}`))
		case "/api/v1/runs/run-141/events?limit=200":
			_, _ = w.Write([]byte(`{
				"rows":[{"sequence":1,"id":"event-1","at":"2026-05-06T08:55:01Z","category":"lifecycle","severity":"info","title":"Run started","summary":"run started","issue_id":"issue-141","issue_identifier":"TOO-141","run_id":"run-141","payload":{}}],
				"limit":200
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output, err := executeCommand(t, "tui", "--endpoint", server.URL, "--run", "TOO-141")
	if err != nil {
		t.Fatalf("expected tui detail to succeed: %v", err)
	}
	for _, want := range []string{"SYMPHONY RUN DETAIL", "TOO-141", "Run started - run started"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	wantPaths := []string{
		"/api/v1/issues/TOO-141/latest",
		"/api/v1/runs/run-141/events?limit=200",
	}
	if strings.Join(paths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("paths = %#v, want %#v", paths, wantPaths)
	}
}

func TestTUIRejectsAmbiguousDetailFlags(t *testing.T) {
	_, err := executeCommand(t, "tui", "--run", "TOO-141", "--run-id", "run-141")
	if err == nil {
		t.Fatal("expected tui with --run and --run-id to fail")
	}
	if !strings.Contains(err.Error(), "provide either --run or --run-id") {
		t.Fatalf("unexpected error: %v", err)
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
	withRunRuntimeFactory(t, func(path string, _ config.Config) (*orchestrator.Runtime, error) {
		return orchestrator.NewRuntime(path)
	})

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

func TestNewProductionRuntimeWiresDispatchDependencies(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "WORKFLOW.md")
	workspaceRoot := filepath.Join(dir, "workspaces")
	content := strings.Join([]string{
		"---",
		"tracker:",
		"  kind: linear",
		"  endpoint: http://127.0.0.1:1/graphql",
		"  api_key: literal-token",
		"  project_slug: symphony-go",
		"  active_states:",
		"    - Todo",
		"  terminal_states:",
		"    - Done",
		"workspace:",
		"  root: " + workspaceRoot,
		"codex:",
		"  command: codex app-server",
		"---",
		"Prompt",
		"",
	}, "\n")
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
	cfg, err := config.Load(workflowPath)
	if err != nil {
		t.Fatalf("load workflow config: %v", err)
	}

	runtime, err := newProductionRuntime(workflowPath, cfg)
	if err != nil {
		t.Fatalf("newProductionRuntime() error = %v", err)
	}
	if err := runtime.DispatchReady(); err != nil {
		t.Fatalf("expected production runtime to be dispatch-ready: %v", err)
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

func withRunRuntimeFactory(t *testing.T, factory runRuntimeFactory) {
	t.Helper()

	previous := newRuntimeForRun
	newRuntimeForRun = factory
	t.Cleanup(func() {
		newRuntimeForRun = previous
	})
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
