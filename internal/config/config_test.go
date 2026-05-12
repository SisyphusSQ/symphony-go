package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/workflow"
)

func TestFromWorkflowAppliesSpecDefaults(t *testing.T) {
	cfg := mustConfig(t, workflow.Definition{
		Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
		Config:         minimalRawConfig("literal-token", "symphony-go"),
		PromptTemplate: "Run the issue.",
	})

	if cfg.Tracker.Kind != TrackerKindLinear {
		t.Fatalf("Tracker.Kind = %q, want %q", cfg.Tracker.Kind, TrackerKindLinear)
	}
	if cfg.Tracker.Endpoint != DefaultLinearEndpoint {
		t.Fatalf("Tracker.Endpoint = %q, want %q", cfg.Tracker.Endpoint, DefaultLinearEndpoint)
	}
	if cfg.Tracker.APIKey != "literal-token" {
		t.Fatalf("Tracker.APIKey = %q, want literal-token", cfg.Tracker.APIKey)
	}
	if cfg.Tracker.ProjectSlug != "symphony-go" {
		t.Fatalf("Tracker.ProjectSlug = %q, want symphony-go", cfg.Tracker.ProjectSlug)
	}
	if !reflect.DeepEqual(cfg.Tracker.ActiveStates, DefaultActiveStates) {
		t.Fatalf("ActiveStates = %#v, want %#v", cfg.Tracker.ActiveStates, DefaultActiveStates)
	}
	if !reflect.DeepEqual(cfg.Tracker.TerminalStates, DefaultTerminalStates) {
		t.Fatalf("TerminalStates = %#v, want %#v", cfg.Tracker.TerminalStates, DefaultTerminalStates)
	}
	if cfg.Polling.Interval != DefaultPollingInterval {
		t.Fatalf("Polling.Interval = %s, want %s", cfg.Polling.Interval, DefaultPollingInterval)
	}
	if cfg.Server.Port != DefaultServerPort {
		t.Fatalf("Server.Port = %d, want %d", cfg.Server.Port, DefaultServerPort)
	}
	if cfg.StateStore.Path != "" {
		t.Fatalf("StateStore.Path = %q, want empty default", cfg.StateStore.Path)
	}
	if cfg.StateStore.LeaseTimeout != DefaultStateStoreLease {
		t.Fatalf("StateStore.LeaseTimeout = %s, want %s", cfg.StateStore.LeaseTimeout, DefaultStateStoreLease)
	}
	if cfg.Hooks.Timeout != DefaultHookTimeout {
		t.Fatalf("Hooks.Timeout = %s, want %s", cfg.Hooks.Timeout, DefaultHookTimeout)
	}
	if cfg.Agent.MaxConcurrentAgents != DefaultMaxConcurrentAgents {
		t.Fatalf("MaxConcurrentAgents = %d, want %d", cfg.Agent.MaxConcurrentAgents, DefaultMaxConcurrentAgents)
	}
	if cfg.Agent.MaxTurns != DefaultMaxTurns {
		t.Fatalf("MaxTurns = %d, want %d", cfg.Agent.MaxTurns, DefaultMaxTurns)
	}
	if cfg.Agent.MaxRunDuration != DefaultMaxRunDuration {
		t.Fatalf("MaxRunDuration = %s, want %s", cfg.Agent.MaxRunDuration, DefaultMaxRunDuration)
	}
	if cfg.Agent.MaxTotalTokens != DefaultMaxTotalTokens {
		t.Fatalf("MaxTotalTokens = %d, want %d", cfg.Agent.MaxTotalTokens, DefaultMaxTotalTokens)
	}
	if cfg.Agent.MaxRetryBackoff != DefaultMaxRetryBackoff {
		t.Fatalf("MaxRetryBackoff = %s, want %s", cfg.Agent.MaxRetryBackoff, DefaultMaxRetryBackoff)
	}
	if len(cfg.Agent.MaxConcurrentAgentsByState) != 0 {
		t.Fatalf("MaxConcurrentAgentsByState = %#v, want empty", cfg.Agent.MaxConcurrentAgentsByState)
	}
	if cfg.Codex.Command != DefaultCodexCommand {
		t.Fatalf("Codex.Command = %q, want %q", cfg.Codex.Command, DefaultCodexCommand)
	}
	if cfg.Codex.ApprovalPolicy != DefaultCodexApprovalPolicy {
		t.Fatalf("ApprovalPolicy = %q, want %q", cfg.Codex.ApprovalPolicy, DefaultCodexApprovalPolicy)
	}
	if cfg.Codex.ThreadSandbox != DefaultCodexThreadSandbox {
		t.Fatalf("ThreadSandbox = %q, want %q", cfg.Codex.ThreadSandbox, DefaultCodexThreadSandbox)
	}
	if got := cfg.Codex.TurnSandboxPolicy["type"]; got != "workspaceWrite" {
		t.Fatalf("TurnSandboxPolicy[type] = %v, want workspaceWrite", got)
	}
	if cfg.Codex.TurnTimeout != DefaultCodexTurnTimeout {
		t.Fatalf("Codex.TurnTimeout = %s, want %s", cfg.Codex.TurnTimeout, DefaultCodexTurnTimeout)
	}
	if cfg.Codex.ReadTimeout != DefaultCodexReadTimeout {
		t.Fatalf("Codex.ReadTimeout = %s, want %s", cfg.Codex.ReadTimeout, DefaultCodexReadTimeout)
	}
	if cfg.Codex.StallTimeout != DefaultCodexStallTimeout {
		t.Fatalf("Codex.StallTimeout = %s, want %s", cfg.Codex.StallTimeout, DefaultCodexStallTimeout)
	}
	if cfg.PromptBody != "Run the issue." {
		t.Fatalf("PromptBody = %q, want Run the issue.", cfg.PromptBody)
	}
	if cfg.WorkflowRef == "" {
		t.Fatal("WorkflowRef should preserve the workflow path")
	}
}

func TestFromWorkflowResolvesEnvAndNormalizesPaths(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "raw", "..", "workspaces")
	raw := minimalRawConfig("$LINEAR_API_KEY", "symphony-go")
	raw["workspace"] = map[string]any{
		"root": "$SYMPHONY_WORKSPACE_ROOT",
	}
	raw["state_store"] = map[string]any{
		"path":             "$SYMPHONY_STATE_DB",
		"instance_id":      "dev-instance",
		"lease_timeout_ms": 90000,
	}
	raw["hooks"] = map[string]any{
		"after_create": `git clone "$SOURCE_REPO_URL" .`,
		"timeout_ms":   120000,
	}
	raw["agent"] = map[string]any{
		"max_concurrent_agents":       2,
		"max_turns":                   8,
		"max_run_duration_ms":         120000,
		"max_total_tokens":            1000,
		"max_cost_usd":                2.5,
		"cost_per_million_tokens_usd": 10,
		"max_retry_backoff_ms":        45000,
		"max_concurrent_agents_by_state": map[string]any{
			"Rework":  3,
			"Merging": 2,
			"Todo":    0,
			"Bad":     "x",
		},
	}
	raw["codex"] = map[string]any{
		"command":             `codex app-server --token "$CODEX_TOKEN"`,
		"approval_policy":     "on-request",
		"thread_sandbox":      "workspace-write",
		"turn_sandbox_policy": map[string]any{"type": "workspaceWrite"},
		"turn_timeout_ms":     1000,
		"read_timeout_ms":     2000,
		"stall_timeout_ms":    0,
	}

	cfg := mustConfig(
		t,
		workflow.Definition{
			Path:           filepath.Join(tempDir, "repo", "WORKFLOW.md"),
			Config:         raw,
			PromptTemplate: "Prompt",
		},
		WithEnv(mapEnv(map[string]string{
			"LINEAR_API_KEY":          "resolved-token",
			"SYMPHONY_WORKSPACE_ROOT": workspaceRoot,
			"SYMPHONY_STATE_DB":       filepath.Join(tempDir, "state", "symphony.sqlite"),
		})),
	)

	if cfg.Tracker.APIKey != "resolved-token" {
		t.Fatalf("Tracker.APIKey = %q, want resolved-token", cfg.Tracker.APIKey)
	}
	if cfg.Workspace.Root != filepath.Clean(workspaceRoot) {
		t.Fatalf("Workspace.Root = %q, want %q", cfg.Workspace.Root, filepath.Clean(workspaceRoot))
	}
	if cfg.StateStore.Path != filepath.Join(tempDir, "state", "symphony.sqlite") {
		t.Fatalf("StateStore.Path = %q", cfg.StateStore.Path)
	}
	if cfg.StateStore.InstanceID != "dev-instance" || cfg.StateStore.LeaseTimeout != 90*time.Second {
		t.Fatalf("StateStore = %#v, want configured instance and lease", cfg.StateStore)
	}
	if cfg.Hooks.AfterCreate != `git clone "$SOURCE_REPO_URL" .` {
		t.Fatalf("hook script was unexpectedly expanded: %q", cfg.Hooks.AfterCreate)
	}
	if cfg.Hooks.Timeout != 120*time.Second {
		t.Fatalf("Hooks.Timeout = %s, want 2m0s", cfg.Hooks.Timeout)
	}
	if cfg.Agent.MaxConcurrentAgents != 2 || cfg.Agent.MaxTurns != 8 {
		t.Fatalf("Agent limits = %#v", cfg.Agent)
	}
	if cfg.Agent.MaxRunDuration != 120*time.Second ||
		cfg.Agent.MaxTotalTokens != 1000 ||
		cfg.Agent.MaxCostUSD != 2.5 ||
		cfg.Agent.CostPerMillionTokensUSD != 10 {
		t.Fatalf("Agent guardrails = %#v", cfg.Agent)
	}
	if cfg.Agent.MaxRetryBackoff != 45*time.Second {
		t.Fatalf("MaxRetryBackoff = %s, want 45s", cfg.Agent.MaxRetryBackoff)
	}
	wantByState := map[string]int{"rework": 3, "merging": 2}
	if !reflect.DeepEqual(cfg.Agent.MaxConcurrentAgentsByState, wantByState) {
		t.Fatalf("MaxConcurrentAgentsByState = %#v, want %#v", cfg.Agent.MaxConcurrentAgentsByState, wantByState)
	}
	if cfg.Codex.Command != `codex app-server --token "$CODEX_TOKEN"` {
		t.Fatalf("Codex.Command was unexpectedly expanded: %q", cfg.Codex.Command)
	}
	if cfg.Codex.ApprovalPolicy != "on-request" {
		t.Fatalf("ApprovalPolicy = %q, want on-request", cfg.Codex.ApprovalPolicy)
	}
	if cfg.Codex.ThreadSandbox != "workspace-write" {
		t.Fatalf("ThreadSandbox = %q, want workspace-write", cfg.Codex.ThreadSandbox)
	}
	if got := cfg.Codex.TurnSandboxPolicy["type"]; got != "workspaceWrite" {
		t.Fatalf("TurnSandboxPolicy[type] = %v, want workspaceWrite", got)
	}
	if cfg.Codex.TurnTimeout != time.Second || cfg.Codex.ReadTimeout != 2*time.Second {
		t.Fatalf("Codex timeouts = %#v", cfg.Codex)
	}
	if cfg.Codex.StallTimeout != 0 {
		t.Fatalf("StallTimeout = %s, want disabled zero", cfg.Codex.StallTimeout)
	}
}

func TestFromWorkflowAllowsDisabledTokenGuardrail(t *testing.T) {
	raw := minimalRawConfig("literal-token", "symphony-go")
	raw["agent"] = map[string]any{
		"max_total_tokens": 0,
	}

	cfg := mustConfig(t, workflow.Definition{
		Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
		Config:         raw,
		PromptTemplate: "Prompt",
	})

	if cfg.Agent.MaxTotalTokens != 0 {
		t.Fatalf("MaxTotalTokens = %d, want disabled zero", cfg.Agent.MaxTotalTokens)
	}
}

func TestFromWorkflowParsesIssueFilterExtension(t *testing.T) {
	raw := minimalRawConfig("literal-token", "symphony-go")
	raw["tracker"].(map[string]any)["issue_filter"] = map[string]any{
		"require_labels":                   []any{"repo:api", "security"},
		"reject_labels":                    []any{"repo:web", "cross-repo"},
		"require_any_labels":               []any{"low-risk", "migration"},
		"require_exactly_one_label_prefix": "repo:",
	}

	cfg := mustConfig(t, workflow.Definition{
		Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
		Config:         raw,
		PromptTemplate: "Prompt",
	})

	want := IssueFilter{
		RequireLabels:                []string{"repo:api", "security"},
		RejectLabels:                 []string{"repo:web", "cross-repo"},
		RequireAnyLabels:             []string{"low-risk", "migration"},
		RequireExactlyOneLabelPrefix: "repo:",
	}
	if !reflect.DeepEqual(cfg.Tracker.IssueFilter, want) {
		t.Fatalf("IssueFilter = %#v, want %#v", cfg.Tracker.IssueFilter, want)
	}

	clone := cfg.Clone()
	clone.Tracker.IssueFilter.RequireLabels[0] = "repo:web"
	if cfg.Tracker.IssueFilter.RequireLabels[0] != "repo:api" {
		t.Fatalf("Clone() shared IssueFilter.RequireLabels backing array")
	}
}

func TestFromWorkflowPreservesLiteralAPIKeyWithDollar(t *testing.T) {
	cfg := mustConfig(t, workflow.Definition{
		Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
		Config:         minimalRawConfig("literal$token", "symphony-go"),
		PromptTemplate: "Prompt",
	})

	if cfg.Tracker.APIKey != "literal$token" {
		t.Fatalf("Tracker.APIKey = %q, want literal$token", cfg.Tracker.APIKey)
	}
}

func TestFromWorkflowKeepsSafeDefaultsForBlankCodexSafetyFields(t *testing.T) {
	raw := minimalRawConfig("literal-token", "symphony-go")
	raw["codex"] = map[string]any{
		"approval_policy":     " ",
		"thread_sandbox":      "",
		"turn_sandbox_policy": map[string]any{},
	}

	cfg := mustConfig(t, workflow.Definition{
		Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
		Config:         raw,
		PromptTemplate: "Prompt",
	})

	if cfg.Codex.ApprovalPolicy != DefaultCodexApprovalPolicy {
		t.Fatalf("ApprovalPolicy = %q, want %q", cfg.Codex.ApprovalPolicy, DefaultCodexApprovalPolicy)
	}
	if cfg.Codex.ThreadSandbox != DefaultCodexThreadSandbox {
		t.Fatalf("ThreadSandbox = %q, want %q", cfg.Codex.ThreadSandbox, DefaultCodexThreadSandbox)
	}
	if got := cfg.Codex.TurnSandboxPolicy["type"]; got != "workspaceWrite" {
		t.Fatalf("TurnSandboxPolicy[type] = %v, want workspaceWrite", got)
	}
}

func TestFromWorkflowAllowsUnsafeCodexWithExplicitOption(t *testing.T) {
	raw := minimalRawConfig("literal-token", "symphony-go")
	raw["codex"] = map[string]any{
		"command":             "codex app-server",
		"approval_policy":     "never",
		"thread_sandbox":      "danger-full-access",
		"turn_sandbox_policy": map[string]any{"type": "dangerFullAccess", "shell_environment_policy": map[string]any{"inherit": "all"}},
	}

	cfg := mustConfig(
		t,
		workflow.Definition{
			Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
			Config:         raw,
			PromptTemplate: "Prompt",
		},
		WithAllowUnsafeCodex(),
	)

	if cfg.Codex.ApprovalPolicy != "never" {
		t.Fatalf("ApprovalPolicy = %q, want never", cfg.Codex.ApprovalPolicy)
	}
	if cfg.Codex.ThreadSandbox != "danger-full-access" {
		t.Fatalf("ThreadSandbox = %q, want danger-full-access", cfg.Codex.ThreadSandbox)
	}
	if got := cfg.Codex.TurnSandboxPolicy["type"]; got != "dangerFullAccess" {
		t.Fatalf("TurnSandboxPolicy[type] = %v, want dangerFullAccess", got)
	}
}

func TestFromWorkflowNormalizesRelativeAndHomeWorkspaceRoots(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, "repo", "nested")
	workflowPath := filepath.Join(workflowDir, "WORKFLOW.md")

	tests := []struct {
		name string
		root string
		want string
	}{
		{
			name: "relative to workflow directory",
			root: "workspaces/../ws",
			want: filepath.Join(workflowDir, "ws"),
		},
		{
			name: "home expansion",
			root: "~/symphony",
			want: filepath.Join(tempDir, "home", "symphony"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := minimalRawConfig("literal-token", "symphony-go")
			raw["workspace"] = map[string]any{"root": tt.root}

			cfg := mustConfig(
				t,
				workflow.Definition{
					Path:           workflowPath,
					Config:         raw,
					PromptTemplate: "Prompt",
				},
				WithHomeDir(filepath.Join(tempDir, "home")),
			)

			if cfg.Workspace.Root != filepath.Clean(tt.want) {
				t.Fatalf("Workspace.Root = %q, want %q", cfg.Workspace.Root, filepath.Clean(tt.want))
			}
		})
	}
}

func TestFromWorkflowRejectsInvalidConfig(t *testing.T) {
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":            "github",
			"api_key":         "$LINEAR_API_KEY",
			"active_states":   []any{},
			"terminal_states": []any{},
			"issue_filter": map[string]any{
				"require_labels":                   []any{"repo:api", 1},
				"require_exactly_one_label_prefix": 42,
			},
		},
		"polling": map[string]any{
			"interval_ms": 0,
		},
		"server": map[string]any{
			"port": 70000,
		},
		"state_store": map[string]any{
			"path":             42,
			"lease_timeout_ms": 0,
		},
		"workspace": map[string]any{
			"root": "/",
		},
		"hooks": map[string]any{
			"timeout_ms": -1,
		},
		"agent": map[string]any{
			"max_concurrent_agents":       0,
			"max_turns":                   -1,
			"max_run_duration_ms":         0,
			"max_total_tokens":            -1,
			"max_cost_usd":                -1.0,
			"cost_per_million_tokens_usd": -1.0,
			"max_retry_backoff_ms":        0,
		},
		"codex": map[string]any{
			"command":             "   ",
			"approval_policy":     "never",
			"thread_sandbox":      "danger-full-access",
			"turn_sandbox_policy": map[string]any{"type": "dangerFullAccess", "shell_environment_policy": map[string]any{"inherit": "all"}},
			"turn_timeout_ms":     0,
			"read_timeout_ms":     -1,
		},
	}

	_, err := FromWorkflow(
		workflow.Definition{
			Path:           filepath.Join(t.TempDir(), "WORKFLOW.md"),
			Config:         raw,
			PromptTemplate: "Prompt",
		},
		WithEnv(mapEnv(map[string]string{"LINEAR_API_KEY": ""})),
	)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}

	for _, want := range []string{
		"tracker.kind",
		"tracker.api_key",
		"tracker.active_states",
		"tracker.terminal_states",
		"tracker.issue_filter.require_labels[1]",
		"tracker.issue_filter.require_exactly_one_label_prefix",
		"polling.interval_ms",
		"server.port",
		"state_store.path",
		"state_store.lease_timeout_ms",
		"workspace.root",
		"hooks.timeout_ms",
		"agent.max_concurrent_agents",
		"agent.max_turns",
		"agent.max_run_duration_ms",
		"agent.max_total_tokens",
		"agent.max_cost_usd",
		"agent.cost_per_million_tokens_usd",
		"agent.max_retry_backoff_ms",
		"codex.command",
		"codex.approval_policy",
		"codex.thread_sandbox",
		"codex.turn_sandbox_policy.type",
		"codex.turn_sandbox_policy.shell_environment_policy.inherit",
		"codex.turn_timeout_ms",
		"codex.read_timeout_ms",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error %q to contain %q", err.Error(), want)
		}
	}
}

func TestLoadIntegratesWorkflowLoaderWithTypedConfig(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "env-token")
	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	content := `---
tracker:
  kind: linear
  api_key: "$LINEAR_API_KEY"
  project_slug: "symphony-go"
polling:
  interval_ms: 5000
workspace:
  root: ".symphony-workspaces"
---

Prompt body
`
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	cfg, err := Load(workflowPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Tracker.APIKey != "env-token" {
		t.Fatalf("Tracker.APIKey = %q, want env-token", cfg.Tracker.APIKey)
	}
	if cfg.Polling.Interval != 5*time.Second {
		t.Fatalf("Polling.Interval = %s, want 5s", cfg.Polling.Interval)
	}
	if cfg.Workspace.Root != filepath.Join(filepath.Dir(workflowPath), ".symphony-workspaces") {
		t.Fatalf("Workspace.Root = %q", cfg.Workspace.Root)
	}
	if cfg.PromptBody != "Prompt body" {
		t.Fatalf("PromptBody = %q, want Prompt body", cfg.PromptBody)
	}
}

func minimalRawConfig(apiKey string, projectSlug string) map[string]any {
	return map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"api_key":      apiKey,
			"project_slug": projectSlug,
		},
	}
}

func mustConfig(t *testing.T, def workflow.Definition, opts ...Option) Config {
	t.Helper()

	cfg, err := FromWorkflow(def, opts...)
	if err != nil {
		t.Fatalf("FromWorkflow() returned error: %v", err)
	}
	return cfg
}

func mapEnv(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
