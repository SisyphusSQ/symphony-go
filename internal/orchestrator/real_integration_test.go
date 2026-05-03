package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
	"github.com/SisyphusSQ/symphony-go/internal/tracker/linear"
)

func TestRealIntegrationProfile(t *testing.T) {
	if os.Getenv("SYMPHONY_REAL_INTEGRATION") != "1" {
		t.Skip("skipped real integration profile: set SYMPHONY_REAL_INTEGRATION=1 with LINEAR_API_KEY, SYMPHONY_WORKSPACE_ROOT, SOURCE_REPO_URL, and SYMPHONY_REAL_DOGFOOD_ISSUE")
	}

	requireRealIntegrationEnv(t,
		"LINEAR_API_KEY",
		"SYMPHONY_WORKSPACE_ROOT",
		"SOURCE_REPO_URL",
		"SYMPHONY_REAL_DOGFOOD_ISSUE",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workflowPath := filepath.Join(realIntegrationRepoRoot(t), "WORKFLOW.md")
	cfg, err := config.Load(workflowPath)
	if err != nil {
		t.Fatalf("config.Load(WORKFLOW.md) error = %v", err)
	}
	assertRealDogfoodDefaults(t, cfg)

	client, err := linear.NewFromTrackerConfig(
		cfg.Tracker,
		linear.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}),
	)
	if err != nil {
		t.Fatalf("linear.NewFromTrackerConfig() error = %v", err)
	}
	issues, err := client.FetchCandidateIssues(ctx)
	if err != nil {
		t.Fatalf("FetchCandidateIssues() error = %v", err)
	}
	target := strings.TrimSpace(os.Getenv("SYMPHONY_REAL_DOGFOOD_ISSUE"))
	if _, ok := findIssueByIdentifier(issues, target); !ok {
		t.Fatalf(
			"real dogfood issue %q was not returned by project %q active states %v; "+
				"create an isolated low-risk issue in an active state or update SYMPHONY_REAL_DOGFOOD_ISSUE",
			target,
			cfg.Tracker.ProjectSlug,
			cfg.Tracker.ActiveStates,
		)
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex command is required for real dogfood profile: %v", err)
	}
	output, err := exec.CommandContext(ctx, codexPath, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("codex --version failed: %v output=%s", err, strings.TrimSpace(string(output)))
	}

	t.Logf(
		"real dogfood preflight ok: project=%q candidates=%d target=%q codex=%q workspace_root_configured=%t",
		cfg.Tracker.ProjectSlug,
		len(issues),
		target,
		strings.TrimSpace(string(output)),
		cfg.Workspace.Root != "",
	)
}

func TestRealDogfoodWorkflowDefaults(t *testing.T) {
	workflowPath := filepath.Join(realIntegrationRepoRoot(t), "WORKFLOW.md")
	cfg, err := config.Load(workflowPath, config.WithEnv(realIntegrationConfigEnv(map[string]string{
		"LINEAR_API_KEY":          "fake-linear-token",
		"SYMPHONY_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "workspaces"),
	})))
	if err != nil {
		t.Fatalf("config.Load(WORKFLOW.md) error = %v", err)
	}
	assertRealDogfoodDefaults(t, cfg)
}

func TestMissingRealIntegrationEnv(t *testing.T) {
	missing := missingRealIntegrationEnv(
		[]string{"LINEAR_API_KEY", "SOURCE_REPO_URL", "SYMPHONY_REAL_DOGFOOD_ISSUE"},
		realIntegrationTestEnv(map[string]string{
			"LINEAR_API_KEY": "fake-linear-token",
		}),
	)
	want := "SOURCE_REPO_URL,SYMPHONY_REAL_DOGFOOD_ISSUE"
	if strings.Join(missing, ",") != want {
		t.Fatalf("missing env = %v, want %s", missing, want)
	}
}

func requireRealIntegrationEnv(t *testing.T, names ...string) {
	t.Helper()

	missing := missingRealIntegrationEnv(names, os.Getenv)
	if len(missing) > 0 {
		t.Fatalf(
			"real integration profile explicitly enabled but required environment is missing: %s",
			strings.Join(missing, ", "),
		)
	}
}

func missingRealIntegrationEnv(names []string, getenv func(string) string) []string {
	var missing []string
	for _, name := range names {
		if strings.TrimSpace(getenv(name)) == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func realIntegrationTestEnv(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}

func realIntegrationConfigEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func assertRealDogfoodDefaults(t *testing.T, cfg config.Config) {
	t.Helper()

	if cfg.Agent.MaxConcurrentAgents != 1 {
		t.Fatalf("agent.max_concurrent_agents = %d, want 1 for real dogfood profile", cfg.Agent.MaxConcurrentAgents)
	}
	if containsState(cfg.Tracker.ActiveStates, "Merging") {
		t.Fatalf("tracker.active_states includes Merging; real dogfood default must not dispatch Merging")
	}
	if _, ok := cfg.Agent.MaxConcurrentAgentsByState["merging"]; ok {
		t.Fatal("agent.max_concurrent_agents_by_state includes Merging; real dogfood default must not enable Merging")
	}
	if !filepath.IsAbs(cfg.Workspace.Root) {
		t.Fatalf("workspace.root = %q, want absolute path after config resolution", cfg.Workspace.Root)
	}
}

func containsState(states []string, want string) bool {
	for _, state := range states {
		if strings.EqualFold(strings.TrimSpace(state), want) {
			return true
		}
	}
	return false
}

func findIssueByIdentifier(issues []tracker.Issue, identifier string) (tracker.Issue, bool) {
	for _, issue := range issues {
		if strings.EqualFold(issue.Identifier, identifier) {
			return issue, true
		}
	}
	return tracker.Issue{}, false
}

func realIntegrationRepoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve real integration test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "WORKFLOW.md")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cannot find WORKFLOW.md from test path %q", filename)
		}
		t.Fatalf("cannot inspect WORKFLOW.md: %v", err)
	}
	return root
}
