package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/workspace"
)

func TestRunnerUsesConfigTimeoutAndDefault(t *testing.T) {
	if got := NewRunner(config.Hooks{}).Timeout(); got != config.DefaultHookTimeout {
		t.Fatalf("Timeout() = %s, want %s", got, config.DefaultHookTimeout)
	}

	custom := 123 * time.Millisecond
	if got := NewRunner(config.Hooks{Timeout: custom}).Timeout(); got != custom {
		t.Fatalf("Timeout() = %s, want %s", got, custom)
	}
}

func TestRunnerRunsHookInWorkspaceCWDAndCapturesOutput(t *testing.T) {
	cwd := t.TempDir()
	runner := NewRunner(config.Hooks{
		BeforeRun: `pwd; printf "stdout-body"; printf "stderr-body" >&2`,
		Timeout:   time.Second,
	})

	result, err := runner.Run(context.Background(), BeforeRun, cwd)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Success() {
		t.Fatalf("Success() = false for result %#v", result)
	}
	if result.CWD != cwd {
		t.Fatalf("CWD = %q, want %q", result.CWD, cwd)
	}

	stdoutLines := strings.SplitN(result.Stdout, "\n", 2)
	if stdoutLines[0] != cwd {
		t.Fatalf("hook pwd stdout = %q, want %q in full stdout %q", stdoutLines[0], cwd, result.Stdout)
	}
	if !strings.Contains(result.Stdout, "stdout-body") {
		t.Fatalf("Stdout = %q, want captured stdout body", result.Stdout)
	}
	if result.Stderr != "stderr-body" {
		t.Fatalf("Stderr = %q, want stderr-body", result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestRunnerReturnsFailureResultWithOutput(t *testing.T) {
	runner := NewRunner(config.Hooks{
		BeforeRun: `printf "visible stdout"; printf "visible stderr" >&2; exit 23`,
		Timeout:   time.Second,
	})

	result, err := runner.Run(context.Background(), BeforeRun, t.TempDir())
	if err == nil {
		t.Fatal("Run() error = nil, want failure")
	}
	if !errors.Is(err, ErrHookFailed) {
		t.Fatalf("errors.Is(err, ErrHookFailed) = false for %v", err)
	}

	var hookErr *Error
	if !errors.As(err, &hookErr) {
		t.Fatalf("errors.As(*Error) = false for %T", err)
	}
	if hookErr.Result.ExitCode != 23 || result.ExitCode != 23 {
		t.Fatalf("ExitCode result=%d hookErr=%d, want 23", result.ExitCode, hookErr.Result.ExitCode)
	}
	if result.Stdout != "visible stdout" || result.Stderr != "visible stderr" {
		t.Fatalf("captured output mismatch: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if result.Success() {
		t.Fatal("Success() = true, want false for failed hook")
	}
}

func TestRunnerTimesOutHook(t *testing.T) {
	runner := NewRunner(config.Hooks{
		BeforeRun: `sleep 2`,
		Timeout:   20 * time.Millisecond,
	})

	start := time.Now()
	result, err := runner.Run(context.Background(), BeforeRun, t.TempDir())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Run() error = nil, want timeout")
	}
	if !errors.Is(err, ErrHookTimeout) {
		t.Fatalf("errors.Is(err, ErrHookTimeout) = false for %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("TimedOut = false for result %#v", result)
	}
	if result.Success() {
		t.Fatal("Success() = true, want false for timed-out hook")
	}
	if elapsed > time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
}

func TestRunnerRejectsInvalidCWD(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	runner := NewRunner(config.Hooks{
		BeforeRun: `printf ok`,
		Timeout:   time.Second,
	})

	result, err := runner.Run(context.Background(), BeforeRun, missing)
	if err == nil {
		t.Fatal("Run() error = nil, want invalid cwd")
	}
	if !errors.Is(err, ErrInvalidCWD) {
		t.Fatalf("errors.Is(err, ErrInvalidCWD) = false for %v", err)
	}
	if result.CWD != missing {
		t.Fatalf("CWD = %q, want normalized missing path %q", result.CWD, missing)
	}
}

func TestRunnerSkipsUnconfiguredHook(t *testing.T) {
	result, err := NewRunner(config.Hooks{}).Run(context.Background(), AfterRun, t.TempDir())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Skipped || !result.Success() {
		t.Fatalf("result = %#v, want skipped success", result)
	}
}

func TestRunAfterCreateOnlyRunsForNewWorkspace(t *testing.T) {
	manager, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	runner := NewRunner(config.Hooks{
		AfterCreate: `printf "created\n" >> marker.txt`,
		Timeout:     time.Second,
	})

	first, err := manager.Prepare(workspace.PrepareRequest{IssueIdentifier: "TOO-121"})
	if err != nil {
		t.Fatalf("Prepare(first) error = %v", err)
	}
	firstResult, err := runner.RunAfterCreate(context.Background(), first.Path, first.CreatedNow)
	if err != nil {
		t.Fatalf("RunAfterCreate(first) error = %v", err)
	}
	if firstResult.Skipped {
		t.Fatal("first RunAfterCreate skipped, want execution for new workspace")
	}

	second, err := manager.Prepare(workspace.PrepareRequest{IssueIdentifier: "TOO-121"})
	if err != nil {
		t.Fatalf("Prepare(second) error = %v", err)
	}
	secondResult, err := runner.RunAfterCreate(context.Background(), second.Path, second.CreatedNow)
	if err != nil {
		t.Fatalf("RunAfterCreate(second) error = %v", err)
	}
	if !secondResult.Skipped {
		t.Fatal("second RunAfterCreate did not skip reused workspace")
	}

	data, err := os.ReadFile(filepath.Join(first.Path, "marker.txt"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got := string(data); got != "created\n" {
		t.Fatalf("marker content = %q, want one after_create execution", got)
	}
}
