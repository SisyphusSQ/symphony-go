package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
)

const (
	AfterCreate  Name = "after_create"
	BeforeRun    Name = "before_run"
	AfterRun     Name = "after_run"
	BeforeRemove Name = "before_remove"
)

var (
	ErrInvalidCWD  = errors.New("invalid_hook_cwd")
	ErrHookFailed  = errors.New("hook_failed")
	ErrHookTimeout = errors.New("hook_timeout")
	ErrUnknownHook = errors.New("unknown_hook")
)

// Name identifies one configured lifecycle hook.
type Name string

// String returns the workflow config key for name.
func (name Name) String() string {
	return string(name)
}

// Hook describes a configured lifecycle command.
type Hook struct {
	Name    Name
	Command string
	Timeout time.Duration
}

// Result captures one hook execution outcome for orchestrator logs.
type Result struct {
	Name     Name
	Command  string
	CWD      string
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
	Skipped  bool
}

// Success reports whether the hook completed without failure. Skipped hooks
// are successful because there was no configured command to run.
func (result Result) Success() bool {
	return result.Skipped || (!result.TimedOut && result.ExitCode == 0)
}

// Error wraps a failed hook result while preserving a comparable cause.
type Error struct {
	Result Result
	Err    error
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	if err.Err == nil {
		return fmt.Sprintf("hook %q failed", err.Result.Name)
	}
	if err.Result.Name == "" {
		return err.Err.Error()
	}
	return fmt.Sprintf("hook %q failed: %v", err.Result.Name, err.Err)
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// Runner executes configured lifecycle hooks in a workspace directory.
type Runner struct {
	hooks   map[Name]string
	timeout time.Duration
}

// NewRunner creates a lifecycle hook runner from typed workflow config.
func NewRunner(cfg config.Hooks) *Runner {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = config.DefaultHookTimeout
	}

	return &Runner{
		hooks: map[Name]string{
			AfterCreate:  cfg.AfterCreate,
			BeforeRun:    cfg.BeforeRun,
			AfterRun:     cfg.AfterRun,
			BeforeRemove: cfg.BeforeRemove,
		},
		timeout: timeout,
	}
}

// Timeout returns the effective timeout applied to every configured hook.
func (runner *Runner) Timeout() time.Duration {
	if runner == nil || runner.timeout <= 0 {
		return config.DefaultHookTimeout
	}
	return runner.timeout
}

// RunAfterCreate runs after_create only for newly created workspaces.
func (runner *Runner) RunAfterCreate(ctx context.Context, cwd string, createdNow bool) (Result, error) {
	if !createdNow {
		command := ""
		if runner != nil {
			command = runner.hooks[AfterCreate]
		}
		return skippedResult(AfterCreate, command, cwd), nil
	}
	return runner.Run(ctx, AfterCreate, cwd)
}

// Run executes name in cwd using the host POSIX shell.
func (runner *Runner) Run(ctx context.Context, name Name, cwd string) (Result, error) {
	command, ok := runner.command(name)
	result := Result{
		Name:     name,
		Command:  command,
		CWD:      cwd,
		ExitCode: -1,
	}
	if !ok {
		return result, fmt.Errorf("%w: %q", ErrUnknownHook, name)
	}
	if strings.TrimSpace(command) == "" {
		return skippedResult(name, command, cwd), nil
	}

	normalizedCWD, err := normalizeCWD(cwd)
	if err != nil {
		result.CWD = normalizedCWD
		return result, &Error{
			Result: result,
			Err:    fmt.Errorf("%w: %w", ErrInvalidCWD, err),
		}
	}
	result.CWD = normalizedCWD

	timeout := runner.Timeout()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-lc", command)
	cmd.Dir = normalizedCWD

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if err == nil {
		return result, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		return result, &Error{
			Result: result,
			Err:    fmt.Errorf("%w after %s", ErrHookTimeout, timeout),
		}
	}

	return result, &Error{
		Result: result,
		Err:    fmt.Errorf("%w: %w", ErrHookFailed, err),
	}
}

func (runner *Runner) command(name Name) (string, bool) {
	if runner == nil {
		return "", false
	}
	command, ok := runner.hooks[name]
	return command, ok
}

func skippedResult(name Name, command string, cwd string) Result {
	normalized := cwd
	if strings.TrimSpace(cwd) != "" {
		absolute, err := filepath.Abs(cwd)
		if err == nil {
			normalized = filepath.Clean(absolute)
		}
	}
	return Result{
		Name:     name,
		Command:  command,
		CWD:      normalized,
		ExitCode: 0,
		Skipped:  true,
	}
}

func normalizeCWD(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", errors.New("cwd must not be empty")
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return cwd, fmt.Errorf("normalize cwd %q: %w", cwd, err)
	}
	absolute = filepath.Clean(absolute)

	info, err := os.Stat(absolute)
	if err != nil {
		return absolute, fmt.Errorf("stat cwd %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return absolute, fmt.Errorf("cwd %q is not a directory", absolute)
	}
	return absolute, nil
}
