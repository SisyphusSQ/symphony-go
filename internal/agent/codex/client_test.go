package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/agent"
	"github.com/SisyphusSQ/symphony-go/internal/config"
)

func TestClientRunSuccess(t *testing.T) {
	workspace := t.TempDir()
	cfg := testCodexConfig(helperCommand(), 2*time.Second)
	cfg.ApprovalPolicy = "never"
	cfg.ThreadSandbox = "workspace-write"
	cfg.TurnSandboxPolicy = map[string]any{"type": "workspaceWrite"}

	client := NewClient(
		WithEnv(
			"SYMPHONY_FAKE_CODEX=1",
			"SYMPHONY_FAKE_CODEX_MODE=success",
			"SYMPHONY_FAKE_EXPECT_ENV=present",
		),
		WithClock(func() time.Time { return time.Unix(1, 0) }),
	)
	result, err := client.Run(context.Background(), RunRequest{
		Config:        cfg,
		WorkspacePath: workspace,
		Prompt:        "handle TOO-126",
		IssueKey:      "TOO-126",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ThreadID != "thread-1" || result.TurnID != "turn-1" {
		t.Fatalf("thread/turn = %q/%q, want thread-1/turn-1", result.ThreadID, result.TurnID)
	}
	if result.SessionID != "thread-1-turn-1" {
		t.Fatalf("SessionID = %q", result.SessionID)
	}
	if result.Status != "completed" {
		t.Fatalf("Status = %q", result.Status)
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 20 || result.Usage.TotalTokens != 30 {
		t.Fatalf("Usage = %#v", result.Usage)
	}
	requireEvent(t, result.Events, EventSessionStarted)
	requireEvent(t, result.Events, EventTokenUsageUpdated)
	requireEvent(t, result.Events, EventRateLimitsUpdated)
	requireEvent(t, result.Events, EventTurnCompleted)
}

func TestClientRunMalformedJSON(t *testing.T) {
	result, err := runFakeMode(t, "malformed", config.Codex{})
	if !IsKind(err, ErrorMalformedJSON) {
		t.Fatalf("error = %v, want kind %s", err, ErrorMalformedJSON)
	}
	requireEvent(t, result.Events, EventError)
}

func TestClientRunReadTimeout(t *testing.T) {
	cfg := config.Codex{ReadTimeout: 25 * time.Millisecond}
	result, err := runFakeMode(t, "read-timeout", cfg)
	if !IsKind(err, ErrorReadTimeout) {
		t.Fatalf("error = %v, want kind %s", err, ErrorReadTimeout)
	}
	requireEvent(t, result.Events, EventTimeout)
}

func TestClientRunStallTimeout(t *testing.T) {
	cfg := config.Codex{
		StallTimeout: 25 * time.Millisecond,
		TurnTimeout:  time.Second,
	}
	result, err := runFakeMode(t, "stall-timeout", cfg)
	if !IsKind(err, ErrorStallTimeout) {
		t.Fatalf("error = %v, want kind %s", err, ErrorStallTimeout)
	}
	requireEvent(t, result.Events, EventTimeout)
}

func TestClientRunTurnTimeout(t *testing.T) {
	cfg := config.Codex{
		StallTimeout: 0,
		TurnTimeout:  25 * time.Millisecond,
	}
	result, err := runFakeMode(t, "stall-timeout", cfg)
	if !IsKind(err, ErrorTurnTimeout) {
		t.Fatalf("error = %v, want kind %s", err, ErrorTurnTimeout)
	}
	requireEvent(t, result.Events, EventTimeout)
}

func TestClientRunProcessError(t *testing.T) {
	result, err := runFakeMode(t, "process-error", config.Codex{})
	if !IsKind(err, ErrorProcessExit) {
		t.Fatalf("error = %v, want kind %s", err, ErrorProcessExit)
	}
	requireEvent(t, result.Events, EventProcessExited)
	var codexErr *Error
	if !errors.As(err, &codexErr) {
		t.Fatalf("error = %T, want *Error", err)
	}
	if codexErr.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", codexErr.ExitCode)
	}
	if !strings.Contains(codexErr.Stderr, "fake process failed") {
		t.Fatalf("Stderr = %q", codexErr.Stderr)
	}
}

func TestClientRunRejectsUnsupportedServerRequest(t *testing.T) {
	result, err := runFakeMode(t, "unsupported-request", config.Codex{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	requireEvent(t, result.Events, EventUnsupportedRequest)
	if result.Status != "completed" {
		t.Fatalf("Status = %q", result.Status)
	}
}

func TestRunnerRunAdaptsClientResult(t *testing.T) {
	workspace := t.TempDir()
	runner := NewRunner(
		testCodexConfig(helperCommand(), 2*time.Second),
		WithEnv(
			"SYMPHONY_FAKE_CODEX=1",
			"SYMPHONY_FAKE_CODEX_MODE=success",
			"SYMPHONY_FAKE_EXPECT_ENV=present",
		),
	)
	result, err := runner.Run(context.Background(), agent.RunRequest{
		IssueID:       "issue-1",
		IssueKey:      "TOO-126",
		WorkspacePath: workspace,
		Prompt:        "handle TOO-126",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.SessionID != "thread-1-turn-1" || result.Summary != "completed" {
		t.Fatalf("RunResult = %#v", result)
	}
}

func runFakeMode(t *testing.T, mode string, cfg config.Codex) (Result, error) {
	t.Helper()
	workspace := t.TempDir()
	cfg = mergeConfig(testCodexConfig(helperCommand(), 2*time.Second), cfg)
	client := NewClient(WithEnv(
		"SYMPHONY_FAKE_CODEX=1",
		"SYMPHONY_FAKE_CODEX_MODE="+mode,
		"SYMPHONY_FAKE_EXPECT_ENV=present",
	))
	return client.Run(context.Background(), RunRequest{
		Config:        cfg,
		WorkspacePath: workspace,
		Prompt:        "handle TOO-126",
		IssueKey:      "TOO-126",
	})
}

func testCodexConfig(command string, timeout time.Duration) config.Codex {
	return config.Codex{
		Command:           command,
		ApprovalPolicy:    "never",
		ThreadSandbox:     "workspace-write",
		TurnSandboxPolicy: map[string]any{"type": "workspaceWrite"},
		ReadTimeout:       timeout,
		TurnTimeout:       timeout,
		StallTimeout:      timeout,
	}
}

func mergeConfig(base config.Codex, override config.Codex) config.Codex {
	merged := base
	if override.Command != "" {
		merged.Command = override.Command
	}
	if override.ApprovalPolicy != "" {
		merged.ApprovalPolicy = override.ApprovalPolicy
	}
	if override.ThreadSandbox != "" {
		merged.ThreadSandbox = override.ThreadSandbox
	}
	if override.TurnSandboxPolicy != nil {
		merged.TurnSandboxPolicy = override.TurnSandboxPolicy
	}
	if override.ReadTimeout != 0 {
		merged.ReadTimeout = override.ReadTimeout
	}
	if override.TurnTimeout != 0 {
		merged.TurnTimeout = override.TurnTimeout
	}
	merged.StallTimeout = override.StallTimeout
	return merged
}

func helperCommand() string {
	return strconv.Quote(os.Args[0]) + " -test.run=TestFakeCodexAppServer --"
}

func requireEvent(t *testing.T, events []Event, kind EventKind) {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind {
			return
		}
	}
	t.Fatalf("missing event %s in %#v", kind, events)
}

func TestFakeCodexAppServer(t *testing.T) {
	if os.Getenv("SYMPHONY_FAKE_CODEX") != "1" {
		return
	}
	if err := runFakeCodexAppServer(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

type fakeRequest struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func runFakeCodexAppServer() error {
	mode := os.Getenv("SYMPHONY_FAKE_CODEX_MODE")
	switch mode {
	case "process-error":
		fmt.Fprintln(os.Stderr, "fake process failed")
		os.Exit(7)
	case "malformed":
		fmt.Println("{not-json")
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	reader := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	encoder := json.NewEncoder(writer)
	write := func(value any) error {
		if err := encoder.Encode(value); err != nil {
			return err
		}
		return writer.Flush()
	}
	read := func(wantMethod string) (fakeRequest, error) {
		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return fakeRequest{}, err
			}
			return fakeRequest{}, fmt.Errorf("stdin closed before %s", wantMethod)
		}
		var request fakeRequest
		if err := json.Unmarshal(reader.Bytes(), &request); err != nil {
			return fakeRequest{}, err
		}
		if request.Method != wantMethod {
			return fakeRequest{}, fmt.Errorf("method = %s, want %s", request.Method, wantMethod)
		}
		return request, nil
	}
	readAny := func() (fakeAnyMessage, error) {
		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return fakeAnyMessage{}, err
			}
			return fakeAnyMessage{}, fmt.Errorf("stdin closed before response")
		}
		var message fakeAnyMessage
		if err := json.Unmarshal(reader.Bytes(), &message); err != nil {
			return fakeAnyMessage{}, err
		}
		return message, nil
	}

	initialize, err := read("initialize")
	if err != nil {
		return err
	}
	if mode == "read-timeout" {
		time.Sleep(time.Second)
		return nil
	}
	if err := write(map[string]any{
		"id": initialize.ID,
		"result": map[string]any{
			"codexHome":      "/tmp/fake-codex-home",
			"platformFamily": "unix",
			"platformOs":     "macos",
			"userAgent":      "fake-codex",
		},
	}); err != nil {
		return err
	}

	threadStart, err := read("thread/start")
	if err != nil {
		return err
	}
	if err := assertThreadStart(threadStart.Params); err != nil {
		return err
	}
	if err := write(map[string]any{
		"id": threadStart.ID,
		"result": map[string]any{
			"thread": map[string]any{"id": "thread-1"},
		},
	}); err != nil {
		return err
	}

	turnStart, err := read("turn/start")
	if err != nil {
		return err
	}
	if err := assertTurnStart(turnStart.Params); err != nil {
		return err
	}
	if err := write(map[string]any{
		"id": turnStart.ID,
		"result": map[string]any{
			"turn": map[string]any{"id": "turn-1", "status": "inProgress"},
		},
	}); err != nil {
		return err
	}
	if mode == "stall-timeout" {
		time.Sleep(time.Second)
		return nil
	}

	if mode == "unsupported-request" {
		if err := write(map[string]any{
			"id":     99,
			"method": "item/tool/call",
			"params": map[string]any{"name": "linear_graphql"},
		}); err != nil {
			return err
		}
		unsupportedResponse, err := readAny()
		if err != nil {
			return err
		}
		if unsupportedResponse.ID != 99 || unsupportedResponse.Error == nil ||
			unsupportedResponse.Error.Code != jsonRPCUnsupported {
			return fmt.Errorf("unsupported response = %#v", unsupportedResponse)
		}
	}

	if err := write(map[string]any{
		"method": "thread/tokenUsage/updated",
		"params": map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"tokenUsage": map[string]any{
				"last":  tokenUsage(1, 2, 3),
				"total": tokenUsage(10, 20, 30),
			},
		},
	}); err != nil {
		return err
	}
	if err := write(map[string]any{
		"method": "account/rateLimits/updated",
		"params": map[string]any{
			"rateLimits": map[string]any{"limitName": "fake-limit"},
		},
	}); err != nil {
		return err
	}
	return write(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{
			"threadId": "thread-1",
			"turn":     map[string]any{"id": "turn-1", "status": "completed"},
		},
	})
}

type fakeAnyMessage struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Error  *responseError `json:"error"`
}

func tokenUsage(input int, output int, total int) map[string]any {
	return map[string]any{
		"cachedInputTokens":     0,
		"inputTokens":           input,
		"outputTokens":          output,
		"reasoningOutputTokens": 0,
		"totalTokens":           total,
	}
}

func assertThreadStart(raw json.RawMessage) error {
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	if !samePath(fmt.Sprint(params["cwd"]), cwd) {
		return fmt.Errorf("thread cwd = %v, want %s", params["cwd"], cwd)
	}
	if os.Getenv("SYMPHONY_FAKE_EXPECT_ENV") != "present" {
		return fmt.Errorf("missing inherited test env")
	}
	if params["approvalPolicy"] != "never" {
		return fmt.Errorf("approvalPolicy = %v", params["approvalPolicy"])
	}
	if params["sandbox"] != "workspace-write" {
		return fmt.Errorf("sandbox = %v", params["sandbox"])
	}
	return nil
}

func assertTurnStart(raw json.RawMessage) error {
	var params struct {
		ThreadID      string           `json:"threadId"`
		CWD           string           `json:"cwd"`
		Approval      string           `json:"approvalPolicy"`
		SandboxPolicy map[string]any   `json:"sandboxPolicy"`
		Input         []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	if params.ThreadID != "thread-1" {
		return fmt.Errorf("threadId = %q", params.ThreadID)
	}
	if !samePath(params.CWD, cwd) {
		return fmt.Errorf("turn cwd = %s, want %s", params.CWD, cwd)
	}
	if params.Approval != "never" {
		return fmt.Errorf("turn approvalPolicy = %q", params.Approval)
	}
	if params.SandboxPolicy["type"] != "workspaceWrite" {
		return fmt.Errorf("sandboxPolicy = %#v", params.SandboxPolicy)
	}
	if len(params.Input) != 1 || params.Input[0]["type"] != "text" || params.Input[0]["text"] != "handle TOO-126" {
		return fmt.Errorf("input = %#v", params.Input)
	}
	return nil
}

func samePath(got string, want string) bool {
	gotEval, gotErr := filepath.EvalSymlinks(got)
	wantEval, wantErr := filepath.EvalSymlinks(want)
	if gotErr == nil && wantErr == nil {
		return gotEval == wantEval
	}
	return got == want
}
