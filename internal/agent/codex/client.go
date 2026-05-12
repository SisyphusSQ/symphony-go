package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tools/lineargraphql"
)

const (
	defaultClientName    = "symphony-go"
	defaultClientVersion = "dev"
	defaultMaxLineBytes  = 10 * 1024 * 1024
	jsonRPCUnsupported   = -32601
)

// ErrorKind classifies Codex app-server client failures for runner and
// orchestrator retry handling.
type ErrorKind string

const (
	ErrorInvalidRequest ErrorKind = "invalid_request"
	ErrorProcessStart   ErrorKind = "process_start"
	ErrorProcessExit    ErrorKind = "process_exit"
	ErrorReadTimeout    ErrorKind = "read_timeout"
	ErrorTurnTimeout    ErrorKind = "turn_timeout"
	ErrorStallTimeout   ErrorKind = "stall_timeout"
	ErrorMalformedJSON  ErrorKind = "malformed_json"
	ErrorResponseError  ErrorKind = "response_error"
	ErrorTurnFailed     ErrorKind = "turn_failed"
	ErrorWriteFailed    ErrorKind = "write_failed"
	ErrorContextDone    ErrorKind = "context_done"
)

// Error preserves the normalized failure kind while retaining the lower-level
// cause and process diagnostics.
type Error struct {
	Kind     ErrorKind
	Phase    string
	Message  string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{string(e.Kind)}
	if e.Phase != "" {
		parts = append(parts, e.Phase)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	} else if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if e.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit_code=%d", e.ExitCode))
	}
	if e.Stderr != "" {
		parts = append(parts, "stderr="+e.Stderr)
	}
	return strings.Join(parts, ": ")
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsKind reports whether err is a Codex client error with the requested kind.
func IsKind(err error, kind ErrorKind) bool {
	var codexErr *Error
	return errors.As(err, &codexErr) && codexErr.Kind == kind
}

// EventKind names normalized app-server events emitted by the client.
type EventKind string

const (
	EventProcessStarted     EventKind = "process_started"
	EventSessionStarted     EventKind = "session_started"
	EventThreadStarted      EventKind = "thread_started"
	EventTurnStarted        EventKind = "turn_started"
	EventTurnCompleted      EventKind = "turn_completed"
	EventTurnFailed         EventKind = "turn_failed"
	EventTokenUsageUpdated  EventKind = "token_usage_updated"
	EventRateLimitsUpdated  EventKind = "rate_limits_updated"
	EventToolCall           EventKind = "tool_call"
	EventCommandApproval    EventKind = "command_approval"
	EventFileChangeApproval EventKind = "file_change_approval"
	EventUnsupportedRequest EventKind = "unsupported_server_request"
	EventTimeout            EventKind = "timeout"
	EventError              EventKind = "error"
	EventProcessExited      EventKind = "process_exited"
	EventOtherMessage       EventKind = "other_message"
)

// Event is the runner-facing normalized stream event.
type Event struct {
	Kind      EventKind
	Method    string
	Timestamp time.Time
	ProcessID int
	ThreadID  string
	TurnID    string
	Message   string
	Usage     *Usage
	Payload   json.RawMessage
}

// Usage tracks Codex token totals.
type Usage struct {
	InputTokens           int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
	CachedInputTokens     int64
}

// Result is returned after a turn reaches a terminal state.
type Result struct {
	ThreadID  string
	TurnID    string
	SessionID string
	Status    string
	Usage     Usage
	RateLimit json.RawMessage
	Events    []Event
}

// RunRequest describes one app-server thread + turn execution.
type RunRequest struct {
	Config        config.Codex
	Tracker       config.Tracker
	WorkspacePath string
	Prompt        string
	IssueKey      string
	OnEvent       func(Event)
}

// Client starts and speaks to a local Codex app-server process over JSONL.
type Client struct {
	clientName    string
	clientVersion string
	env           []string
	now           func() time.Time
	maxLineBytes  int
}

// Option customizes a Client.
type Option func(*Client)

// WithEnv appends environment entries to the inherited process environment.
func WithEnv(env ...string) Option {
	return func(c *Client) {
		c.env = append(c.env, env...)
	}
}

// WithClientInfo overrides the initialize clientInfo payload.
func WithClientInfo(name string, version string) Option {
	return func(c *Client) {
		if strings.TrimSpace(name) != "" {
			c.clientName = strings.TrimSpace(name)
		}
		if strings.TrimSpace(version) != "" {
			c.clientVersion = strings.TrimSpace(version)
		}
	}
}

// WithClock overrides event timestamps for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// NewClient returns a JSONL Codex app-server client.
func NewClient(opts ...Option) *Client {
	client := &Client{
		clientName:    defaultClientName,
		clientVersion: defaultClientVersion,
		now:           time.Now,
		maxLineBytes:  defaultMaxLineBytes,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// Run starts the configured app-server process and runs one thread turn.
func (c *Client) Run(ctx context.Context, req RunRequest) (Result, error) {
	if c == nil {
		c = NewClient()
	}
	req.Config = withCodexDefaults(req.Config)
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}

	procCtx, cancelProc := context.WithCancel(ctx)
	defer cancelProc()

	cmd := exec.CommandContext(procCtx, "bash", "-lc", req.Config.Command)
	cmd.Dir = req.WorkspacePath
	cmd.Env = append(os.Environ(), c.env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, wrapError(ErrorProcessStart, "stdin", err, "")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, wrapError(ErrorProcessStart, "stdout", err, "")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, wrapError(ErrorProcessStart, "stderr", err, "")
	}

	if err := cmd.Start(); err != nil {
		return Result{}, wrapError(ErrorProcessStart, "start", err, "")
	}

	var stderrBuf limitedBuffer
	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	messages := make(chan scannedMessage, 16)
	go scanMessages(stdout, c.maxLineBytes, messages)

	waitCh := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		stderrWG.Wait()
		waitCh <- waitErr
		close(waitCh)
	}()

	linearTool := newLinearGraphQLTool(req.Tracker)
	state := &runState{
		req:        req,
		stdin:      stdin,
		waitCh:     waitCh,
		messages:   messages,
		cancel:     cancelProc,
		stderr:     &stderrBuf,
		now:        c.now,
		processID:  cmd.Process.Pid,
		linearTool: linearTool,
	}
	state.emit(Event{Kind: EventProcessStarted})

	if _, err := state.call(ctx, 1, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    c.clientName,
			"version": c.clientVersion,
		},
		"capabilities": map[string]any{
			"experimentalApi": linearTool != nil,
		},
	}, req.Config.ReadTimeout); err != nil {
		state.recordError(err)
		state.stop()
		return state.result, err
	}

	threadResult, err := state.call(ctx, 2, "thread/start", threadStartParams(req, linearTool), req.Config.ReadTimeout)
	if err != nil {
		state.recordError(err)
		state.stop()
		return state.result, err
	}
	threadID := parseThreadID(threadResult)
	if threadID == "" {
		err := &Error{Kind: ErrorResponseError, Phase: "thread/start", Message: "missing thread.id"}
		state.recordError(err)
		state.stop()
		return state.result, err
	}
	state.result.ThreadID = threadID

	turnResult, err := state.call(ctx, 3, "turn/start", turnStartParams(req, threadID), req.Config.ReadTimeout)
	if err != nil {
		state.recordError(err)
		state.stop()
		return state.result, err
	}
	turnID, turnStatus := parseTurn(turnResult)
	if turnID == "" {
		err := &Error{Kind: ErrorResponseError, Phase: "turn/start", Message: "missing turn.id"}
		state.recordError(err)
		state.stop()
		return state.result, err
	}
	state.result.TurnID = turnID
	state.result.Status = turnStatus
	state.result.SessionID = sessionID(threadID, turnID)
	state.emit(Event{
		Kind:     EventSessionStarted,
		ThreadID: threadID,
		TurnID:   turnID,
		Message:  state.result.SessionID,
	})

	if err := state.streamTurn(ctx); err != nil {
		state.recordError(err)
		state.stop()
		return state.result, err
	}
	state.stop()
	return state.result, nil
}

type runState struct {
	req      RunRequest
	stdin    io.WriteCloser
	waitCh   <-chan error
	messages <-chan scannedMessage
	cancel   context.CancelFunc
	stderr   *limitedBuffer
	now      func() time.Time

	processID  int
	linearTool *lineargraphql.Tool
	result     Result
}

func (s *runState) call(
	ctx context.Context,
	id int,
	method string,
	params map[string]any,
	timeout time.Duration,
) (json.RawMessage, error) {
	if err := s.send(requestMessage{ID: id, Method: method, Params: params}); err != nil {
		return nil, &Error{Kind: ErrorWriteFailed, Phase: method, Err: err}
	}
	for {
		msg, err := s.next(ctx, timeout, method)
		if err != nil {
			return nil, err
		}
		if msg.Method != "" && len(msg.ID) == 0 {
			if err := s.handleNotification(msg); err != nil {
				return nil, err
			}
			continue
		}
		if msg.Method != "" && len(msg.ID) > 0 && !matchesID(msg.ID, id) {
			if err := s.rejectServerRequest(msg); err != nil {
				return nil, err
			}
			continue
		}
		if !matchesID(msg.ID, id) {
			s.emit(Event{Kind: EventOtherMessage, Method: msg.Method, Payload: msg.Raw})
			continue
		}
		if msg.Error != nil {
			return nil, &Error{
				Kind:    ErrorResponseError,
				Phase:   method,
				Message: msg.Error.Message,
			}
		}
		return msg.Result, nil
	}
}

func (s *runState) streamTurn(ctx context.Context) error {
	start := time.Now()
	for {
		timeout, kind := s.nextTurnTimeout(start)
		msg, err := s.next(ctx, timeout, "turn")
		if err != nil {
			if IsKind(err, ErrorReadTimeout) {
				return &Error{Kind: kind, Phase: "turn", Message: err.Error(), Stderr: s.stderr.String()}
			}
			return err
		}
		if msg.Method != "" && len(msg.ID) > 0 {
			if err := s.handleServerRequest(ctx, msg); err != nil {
				return err
			}
			continue
		}
		if msg.Method == "" {
			s.emit(Event{Kind: EventOtherMessage, Payload: msg.Raw})
			continue
		}
		if err := s.handleNotification(msg); err != nil {
			return err
		}
		if msg.Method == "turn/completed" && s.result.Status == "completed" {
			return nil
		}
	}
}

func (s *runState) nextTurnTimeout(start time.Time) (time.Duration, ErrorKind) {
	remaining := s.req.Config.TurnTimeout - time.Since(start)
	if remaining <= 0 {
		return 1, ErrorTurnTimeout
	}
	if s.req.Config.StallTimeout <= 0 || s.req.Config.StallTimeout >= remaining {
		return remaining, ErrorTurnTimeout
	}
	return s.req.Config.StallTimeout, ErrorStallTimeout
}

func (s *runState) send(request requestMessage) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.stdin.Write(data)
	return err
}

func (s *runState) next(ctx context.Context, timeout time.Duration, phase string) (rpcMessage, error) {
	if timeout <= 0 {
		timeout = config.DefaultCodexReadTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case scanned, ok := <-s.messages:
		return s.decodeScanned(phase, scanned, ok)
	default:
	}

	select {
	case <-ctx.Done():
		return rpcMessage{}, &Error{Kind: ErrorContextDone, Phase: phase, Err: ctx.Err(), Stderr: s.stderr.String()}
	case <-timer.C:
		return rpcMessage{}, &Error{Kind: ErrorReadTimeout, Phase: phase, Message: "timeout after " + timeout.String(), Stderr: s.stderr.String()}
	case waitErr := <-s.waitCh:
		return rpcMessage{}, s.processExitError(phase, waitErr)
	case scanned, ok := <-s.messages:
		return s.decodeScanned(phase, scanned, ok)
	}
}

func (s *runState) decodeScanned(phase string, scanned scannedMessage, ok bool) (rpcMessage, error) {
	if !ok {
		select {
		case waitErr := <-s.waitCh:
			return rpcMessage{}, s.processExitError(phase, waitErr)
		case <-time.After(50 * time.Millisecond):
			return rpcMessage{}, s.processExitError(phase, nil)
		}
	}
	if scanned.err != nil {
		return rpcMessage{}, scanned.err
	}
	return scanned.msg, nil
}

func (s *runState) handleNotification(msg rpcMessage) error {
	switch msg.Method {
	case "thread/started":
		threadID := parseThreadID(msg.Params)
		if threadID != "" {
			s.result.ThreadID = threadID
		}
		s.emit(Event{Kind: EventThreadStarted, Method: msg.Method, ThreadID: threadID, Payload: msg.Params})
	case "turn/started":
		threadID, turnID, status := parseTurnNotification(msg.Params)
		if threadID != "" {
			s.result.ThreadID = threadID
		}
		if turnID != "" {
			s.result.TurnID = turnID
		}
		if status != "" {
			s.result.Status = status
		}
		s.emit(Event{Kind: EventTurnStarted, Method: msg.Method, ThreadID: threadID, TurnID: turnID, Payload: msg.Params})
	case "thread/tokenUsage/updated":
		usage, threadID, turnID := parseUsage(msg.Params)
		s.result.Usage = usage
		s.emit(Event{
			Kind:     EventTokenUsageUpdated,
			Method:   msg.Method,
			ThreadID: threadID,
			TurnID:   turnID,
			Usage:    &usage,
			Payload:  msg.Params,
		})
	case "account/rateLimits/updated":
		s.result.RateLimit = append(json.RawMessage(nil), msg.Params...)
		s.emit(Event{Kind: EventRateLimitsUpdated, Method: msg.Method, Payload: msg.Params})
	case "turn/completed":
		threadID, turnID, status := parseTurnNotification(msg.Params)
		if status == "" {
			status = "completed"
		}
		s.result.ThreadID = firstNonEmpty(threadID, s.result.ThreadID)
		s.result.TurnID = firstNonEmpty(turnID, s.result.TurnID)
		s.result.Status = status
		s.emit(Event{Kind: EventTurnCompleted, Method: msg.Method, ThreadID: threadID, TurnID: turnID, Payload: msg.Params})
		if status != "completed" {
			return &Error{Kind: ErrorTurnFailed, Phase: "turn", Message: "turn status " + status, Stderr: s.stderr.String()}
		}
	case "error":
		threadID, turnID, message := parseErrorNotification(msg.Params)
		s.result.ThreadID = firstNonEmpty(threadID, s.result.ThreadID)
		s.result.TurnID = firstNonEmpty(turnID, s.result.TurnID)
		s.result.Status = "failed"
		s.emit(Event{Kind: EventTurnFailed, Method: msg.Method, ThreadID: threadID, TurnID: turnID, Message: message, Payload: msg.Params})
		return &Error{Kind: ErrorTurnFailed, Phase: "turn", Message: message, Stderr: s.stderr.String()}
	default:
		s.emit(Event{Kind: EventOtherMessage, Method: msg.Method, Payload: msg.Params})
	}
	return nil
}

func (s *runState) rejectServerRequest(msg rpcMessage) error {
	s.emit(Event{
		Kind:    EventUnsupportedRequest,
		Method:  msg.Method,
		Message: "unsupported server request",
		Payload: msg.Raw,
	})
	response := errorResponse{
		ID: msg.ID,
		Error: responseError{
			Code:    jsonRPCUnsupported,
			Message: "unsupported server request: " + msg.Method,
		},
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := s.stdin.Write(data); err != nil {
		return &Error{Kind: ErrorWriteFailed, Phase: "unsupported_server_request", Err: err}
	}
	return nil
}

func (s *runState) handleServerRequest(ctx context.Context, msg rpcMessage) error {
	switch msg.Method {
	case "item/tool/call":
		return s.handleDynamicToolCall(ctx, msg)
	case "mcpServer/tool/call":
		return s.handleMCPServerToolCall(ctx, msg)
	case "item/commandExecution/requestApproval":
		return s.handleCommandExecutionApproval(msg)
	case "item/fileChange/requestApproval":
		return s.handleFileChangeApproval(msg)
	case "applyPatchApproval":
		return s.handleLegacyReviewApproval(msg, EventFileChangeApproval)
	case "execCommandApproval":
		return s.handleLegacyReviewApproval(msg, EventCommandApproval)
	}
	return s.rejectServerRequest(msg)
}

func (s *runState) handleCommandExecutionApproval(msg rpcMessage) error {
	var params commandExecutionApprovalParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.emit(Event{
			Kind:    EventUnsupportedRequest,
			Method:  msg.Method,
			Message: "invalid command approval request: " + err.Error(),
			Payload: msg.Raw,
		})
		return s.respondCommandExecutionApproval(msg.ID, "cancel")
	}
	decision := commandExecutionApprovalDecision(params)
	payload, _ := json.Marshal(map[string]any{
		"thread_id": params.ThreadID,
		"turn_id":   params.TurnID,
		"item_id":   params.ItemID,
		"command":   params.Command,
		"cwd":       params.CWD,
		"reason":    params.Reason,
		"decision":  decision,
	})
	s.emit(Event{
		Kind:     EventCommandApproval,
		Method:   msg.Method,
		ThreadID: params.ThreadID,
		TurnID:   params.TurnID,
		Message:  "decision=" + commandExecutionDecisionLabel(decision),
		Payload:  payload,
	})
	return s.respondCommandExecutionApproval(msg.ID, decision)
}

func commandExecutionApprovalDecision(params commandExecutionApprovalParams) any {
	if len(params.ProposedExecpolicyAmendment) > 0 {
		return map[string]any{
			"acceptWithExecpolicyAmendment": map[string]any{
				"execpolicy_amendment": params.ProposedExecpolicyAmendment,
			},
		}
	}
	if len(params.ProposedNetworkPolicyAmendments) > 0 {
		return map[string]any{
			"applyNetworkPolicyAmendment": map[string]any{
				"network_policy_amendment": params.ProposedNetworkPolicyAmendments[0],
			},
		}
	}
	return "accept"
}

func (s *runState) handleFileChangeApproval(msg rpcMessage) error {
	var params fileChangeApprovalParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.emit(Event{
			Kind:    EventUnsupportedRequest,
			Method:  msg.Method,
			Message: "invalid file change approval request: " + err.Error(),
			Payload: msg.Raw,
		})
		return s.respondApprovalDecision(msg.ID, msg.Method, fileChangeApprovalResponse{Decision: "cancel"})
	}
	decision := "accept"
	payload, _ := json.Marshal(map[string]any{
		"thread_id":  params.ThreadID,
		"turn_id":    params.TurnID,
		"item_id":    params.ItemID,
		"grant_root": params.GrantRoot,
		"reason":     params.Reason,
		"decision":   decision,
	})
	s.emit(Event{
		Kind:     EventFileChangeApproval,
		Method:   msg.Method,
		ThreadID: params.ThreadID,
		TurnID:   params.TurnID,
		Message:  "decision=" + decision,
		Payload:  payload,
	})
	return s.respondApprovalDecision(msg.ID, msg.Method, fileChangeApprovalResponse{Decision: decision})
}

func (s *runState) handleLegacyReviewApproval(msg rpcMessage, eventKind EventKind) error {
	s.emit(Event{
		Kind:    eventKind,
		Method:  msg.Method,
		Message: "decision=approved",
		Payload: msg.Params,
	})
	return s.respondApprovalDecision(msg.ID, msg.Method, legacyReviewApprovalResponse{Decision: "approved"})
}

func commandExecutionDecisionLabel(decision any) string {
	if label, ok := decision.(string); ok {
		return label
	}
	if decisionMap, ok := decision.(map[string]any); ok {
		for key := range decisionMap {
			return key
		}
	}
	return "accept"
}

func (s *runState) handleDynamicToolCall(ctx context.Context, msg rpcMessage) error {
	var params dynamicToolCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondDynamicToolFailure(msg.ID, "invalid_tool_call", err.Error())
	}
	if params.Tool != lineargraphql.Name || s.linearTool == nil {
		s.emit(Event{
			Kind:    EventUnsupportedRequest,
			Method:  msg.Method,
			Message: "unsupported dynamic tool: " + params.Tool,
			Payload: msg.Raw,
		})
		return s.respondDynamicToolFailure(msg.ID, "unsupported_tool", "unsupported dynamic tool: "+params.Tool)
	}
	result := s.linearTool.ExecuteJSON(ctx, params.Arguments)
	s.emitToolCall(params, result.Success)
	return s.respondDynamicToolResult(msg.ID, result.Success, result.Text())
}

func (s *runState) handleMCPServerToolCall(ctx context.Context, msg rpcMessage) error {
	var params mcpServerToolCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondMCPServerToolFailure(msg.ID, "invalid_tool_call", err.Error())
	}
	if params.Tool != lineargraphql.Name || s.linearTool == nil {
		s.emit(Event{
			Kind:    EventUnsupportedRequest,
			Method:  msg.Method,
			Message: "unsupported mcp server tool: " + params.Tool,
			Payload: msg.Raw,
		})
		return s.respondMCPServerToolFailure(msg.ID, "unsupported_tool", "unsupported mcp server tool: "+params.Tool)
	}
	result := s.linearTool.ExecuteJSON(ctx, params.Arguments)
	s.emitMCPServerToolCall(params, result.Success)
	return s.respondMCPServerToolResult(msg.ID, !result.Success, result.Text())
}

func (s *runState) emitToolCall(params dynamicToolCallParams, success bool) {
	payload, _ := json.Marshal(map[string]any{
		"tool":    params.Tool,
		"call_id": params.CallID,
		"success": success,
	})
	s.emit(Event{
		Kind:     EventToolCall,
		Method:   "item/tool/call",
		ThreadID: params.ThreadID,
		TurnID:   params.TurnID,
		Message:  "tool=" + params.Tool,
		Payload:  payload,
	})
}

func (s *runState) emitMCPServerToolCall(params mcpServerToolCallParams, success bool) {
	payload, _ := json.Marshal(map[string]any{
		"tool":    params.Tool,
		"server":  params.Server,
		"success": success,
	})
	s.emit(Event{
		Kind:     EventToolCall,
		Method:   "mcpServer/tool/call",
		ThreadID: params.ThreadID,
		TurnID:   params.TurnID,
		Message:  "tool=" + params.Tool,
		Payload:  payload,
	})
}

func (s *runState) respondDynamicToolFailure(id json.RawMessage, code string, message string) error {
	output := map[string]any{
		"success": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	data, err := json.Marshal(output)
	if err != nil {
		return err
	}
	return s.respondDynamicToolResult(id, false, string(data))
}

func (s *runState) respondDynamicToolResult(id json.RawMessage, success bool, text string) error {
	response := successResponse{
		ID: id,
		Result: dynamicToolCallResponse{
			Success: success,
			ContentItems: []dynamicToolCallOutputContentItem{{
				Type: "inputText",
				Text: text,
			}},
		},
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := s.stdin.Write(data); err != nil {
		return &Error{Kind: ErrorWriteFailed, Phase: "item/tool/call", Err: err}
	}
	return nil
}

func (s *runState) respondMCPServerToolFailure(id json.RawMessage, code string, message string) error {
	output := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	data, err := json.Marshal(output)
	if err != nil {
		return err
	}
	return s.respondMCPServerToolResult(id, true, string(data))
}

func (s *runState) respondMCPServerToolResult(id json.RawMessage, isError bool, text string) error {
	response := successResponse{
		ID: id,
		Result: mcpServerToolCallResponse{
			Content: []mcpServerToolCallContent{{
				Type: "text",
				Text: text,
			}},
			IsError: isError,
		},
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := s.stdin.Write(data); err != nil {
		return &Error{Kind: ErrorWriteFailed, Phase: "mcpServer/tool/call", Err: err}
	}
	return nil
}

func (s *runState) respondCommandExecutionApproval(id json.RawMessage, decision any) error {
	return s.respondApprovalDecision(id, "item/commandExecution/requestApproval", commandExecutionApprovalResponse{
		Decision: decision,
	})
}

func (s *runState) respondApprovalDecision(id json.RawMessage, phase string, result any) error {
	response := successResponse{ID: id, Result: result}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := s.stdin.Write(data); err != nil {
		return &Error{Kind: ErrorWriteFailed, Phase: phase, Err: err}
	}
	return nil
}

func (s *runState) emit(event Event) {
	event.Timestamp = s.now().UTC()
	event.ProcessID = s.processID
	s.result.Events = append(s.result.Events, event)
	if s.req.OnEvent != nil {
		s.req.OnEvent(event)
	}
}

func (s *runState) recordError(err error) {
	if err == nil {
		return
	}
	kind := EventError
	var codexErr *Error
	if errors.As(err, &codexErr) {
		switch codexErr.Kind {
		case ErrorReadTimeout, ErrorTurnTimeout, ErrorStallTimeout:
			kind = EventTimeout
		case ErrorProcessExit:
			kind = EventProcessExited
		}
	}
	s.emit(Event{
		Kind:    kind,
		Message: err.Error(),
	})
}

func (s *runState) processExitError(phase string, waitErr error) error {
	exitCode := 0
	if exitErr := (&exec.ExitError{}); errors.As(waitErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	message := "process exited"
	if waitErr != nil {
		message = waitErr.Error()
	}
	return &Error{
		Kind:     ErrorProcessExit,
		Phase:    phase,
		Message:  message,
		Err:      waitErr,
		ExitCode: exitCode,
		Stderr:   s.stderr.String(),
	}
}

func (s *runState) stop() {
	s.cancel()
	_ = s.stdin.Close()
	select {
	case <-s.waitCh:
	case <-time.After(time.Second):
	}
}

type requestMessage struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	ID    json.RawMessage `json:"id"`
	Error responseError   `json:"error"`
}

type successResponse struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result"`
}

type dynamicToolCallParams struct {
	Arguments json.RawMessage `json:"arguments"`
	CallID    string          `json:"callId"`
	Namespace *string         `json:"namespace"`
	ThreadID  string          `json:"threadId"`
	Tool      string          `json:"tool"`
	TurnID    string          `json:"turnId"`
}

type mcpServerToolCallParams struct {
	Server    string          `json:"server"`
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type commandExecutionApprovalParams struct {
	ThreadID                        string            `json:"threadId"`
	TurnID                          string            `json:"turnId"`
	ItemID                          string            `json:"itemId"`
	Command                         *string           `json:"command"`
	CWD                             *string           `json:"cwd"`
	Reason                          *string           `json:"reason"`
	ProposedExecpolicyAmendment     []string          `json:"proposedExecpolicyAmendment"`
	ProposedNetworkPolicyAmendments []json.RawMessage `json:"proposedNetworkPolicyAmendments"`
}

type fileChangeApprovalParams struct {
	ThreadID  string  `json:"threadId"`
	TurnID    string  `json:"turnId"`
	ItemID    string  `json:"itemId"`
	Reason    *string `json:"reason"`
	GrantRoot *string `json:"grantRoot"`
}

type dynamicToolCallOutputContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type dynamicToolCallResponse struct {
	ContentItems []dynamicToolCallOutputContentItem `json:"contentItems"`
	Success      bool                               `json:"success"`
}

type mcpServerToolCallContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpServerToolCallResponse struct {
	Content []mcpServerToolCallContent `json:"content"`
	IsError bool                       `json:"isError"`
}

type commandExecutionApprovalResponse struct {
	Decision any `json:"decision"`
}

type fileChangeApprovalResponse struct {
	Decision string `json:"decision"`
}

type legacyReviewApprovalResponse struct {
	Decision string `json:"decision"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
	Raw    json.RawMessage `json:"-"`
}

type scannedMessage struct {
	msg rpcMessage
	err error
}

func scanMessages(stdout io.Reader, maxLineBytes int, out chan<- scannedMessage) {
	defer close(out)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			out <- scannedMessage{err: &Error{Kind: ErrorMalformedJSON, Phase: "read", Message: err.Error()}}
			return
		}
		msg.Raw = line
		out <- scannedMessage{msg: msg}
	}
	if err := scanner.Err(); err != nil {
		out <- scannedMessage{err: err}
	}
}

func validateRequest(req RunRequest) error {
	if strings.TrimSpace(req.Config.Command) == "" {
		return &Error{Kind: ErrorInvalidRequest, Phase: "config", Message: "codex.command is required"}
	}
	if strings.TrimSpace(req.WorkspacePath) == "" {
		return &Error{Kind: ErrorInvalidRequest, Phase: "workspace", Message: "workspace path is required"}
	}
	if !filepath.IsAbs(req.WorkspacePath) {
		return &Error{Kind: ErrorInvalidRequest, Phase: "workspace", Message: "workspace path must be absolute"}
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return &Error{Kind: ErrorInvalidRequest, Phase: "prompt", Message: "prompt is required"}
	}
	return nil
}

func withCodexDefaults(cfg config.Codex) config.Codex {
	if strings.TrimSpace(cfg.Command) == "" {
		cfg.Command = config.DefaultCodexCommand
	}
	if strings.TrimSpace(cfg.ApprovalPolicy) == "" {
		cfg.ApprovalPolicy = config.DefaultCodexApprovalPolicy
	}
	if strings.TrimSpace(cfg.ThreadSandbox) == "" {
		cfg.ThreadSandbox = config.DefaultCodexThreadSandbox
	}
	if len(cfg.TurnSandboxPolicy) == 0 {
		cfg.TurnSandboxPolicy = map[string]any{"type": "workspaceWrite"}
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = config.DefaultCodexReadTimeout
	}
	if cfg.TurnTimeout <= 0 {
		cfg.TurnTimeout = config.DefaultCodexTurnTimeout
	}
	return cfg
}

func threadStartParams(req RunRequest, linearTool *lineargraphql.Tool) map[string]any {
	params := map[string]any{
		"cwd":         req.WorkspacePath,
		"serviceName": "symphony-go",
		"ephemeral":   true,
	}
	if linearTool != nil {
		params["dynamicTools"] = []map[string]any{lineargraphql.Spec()}
	}
	if req.Config.ApprovalPolicy != "" {
		params["approvalPolicy"] = req.Config.ApprovalPolicy
	}
	if req.Config.ThreadSandbox != "" {
		params["sandbox"] = req.Config.ThreadSandbox
	}
	return params
}

func turnStartParams(req RunRequest, threadID string) map[string]any {
	params := map[string]any{
		"threadId": threadID,
		"cwd":      req.WorkspacePath,
		"input": []map[string]any{{
			"type": "text",
			"text": req.Prompt,
		}},
	}
	if req.Config.ApprovalPolicy != "" {
		params["approvalPolicy"] = req.Config.ApprovalPolicy
	}
	if req.Config.TurnSandboxPolicy != nil {
		params["sandboxPolicy"] = req.Config.TurnSandboxPolicy
	}
	return params
}

func parseThreadID(raw json.RawMessage) string {
	var payload struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	_ = json.Unmarshal(raw, &payload)
	return payload.Thread.ID
}

func parseTurn(raw json.RawMessage) (string, string) {
	var payload struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(raw, &payload)
	return payload.Turn.ID, payload.Turn.Status
}

func parseTurnNotification(raw json.RawMessage) (string, string, string) {
	var payload struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(raw, &payload)
	return payload.ThreadID, payload.Turn.ID, payload.Turn.Status
}

func parseUsage(raw json.RawMessage) (Usage, string, string) {
	var payload struct {
		ThreadID   string `json:"threadId"`
		TurnID     string `json:"turnId"`
		TokenUsage struct {
			Total struct {
				CachedInputTokens     int64 `json:"cachedInputTokens"`
				InputTokens           int64 `json:"inputTokens"`
				OutputTokens          int64 `json:"outputTokens"`
				ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
				TotalTokens           int64 `json:"totalTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	_ = json.Unmarshal(raw, &payload)
	return Usage{
		CachedInputTokens:     payload.TokenUsage.Total.CachedInputTokens,
		InputTokens:           payload.TokenUsage.Total.InputTokens,
		OutputTokens:          payload.TokenUsage.Total.OutputTokens,
		ReasoningOutputTokens: payload.TokenUsage.Total.ReasoningOutputTokens,
		TotalTokens:           payload.TokenUsage.Total.TotalTokens,
	}, payload.ThreadID, payload.TurnID
}

func parseErrorNotification(raw json.RawMessage) (string, string, string) {
	var payload struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Error    struct {
			Message           string  `json:"message"`
			AdditionalDetails *string `json:"additionalDetails"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &payload)
	message := payload.Error.Message
	if payload.Error.AdditionalDetails != nil && *payload.Error.AdditionalDetails != "" {
		message += ": " + *payload.Error.AdditionalDetails
	}
	return payload.ThreadID, payload.TurnID, message
}

func matchesID(raw json.RawMessage, want int) bool {
	if len(raw) == 0 {
		return false
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number == want
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text == fmt.Sprintf("%d", want)
	}
	return false
}

func sessionID(threadID string, turnID string) string {
	return threadID + "-" + turnID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func newLinearGraphQLTool(cfg config.Tracker) *lineargraphql.Tool {
	if !lineargraphql.Available(cfg) {
		return nil
	}
	tool, err := lineargraphql.NewFromTrackerConfig(cfg)
	if err != nil {
		return nil
	}
	return tool
}

func wrapError(kind ErrorKind, phase string, err error, stderr string) error {
	return &Error{Kind: kind, Phase: phase, Err: err, Stderr: stderr}
}

type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 64 * 1024
	remaining := limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
		} else {
			_, _ = b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}
