package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/workflow"
)

const (
	TrackerKindLinear = "linear"

	DefaultLinearEndpoint      = "https://api.linear.app/graphql"
	DefaultPollingInterval     = 30 * time.Second
	DefaultWorkspaceRootBase   = "symphony_workspaces"
	DefaultHookTimeout         = 60 * time.Second
	DefaultMaxConcurrentAgents = 10
	DefaultMaxTurns            = 20
	DefaultMaxRetryBackoff     = 5 * time.Minute
	DefaultStateStoreLease     = 5 * time.Minute
	DefaultCodexCommand        = "codex app-server"
	DefaultCodexTurnTimeout    = time.Hour
	DefaultCodexReadTimeout    = 5 * time.Second
	DefaultCodexStallTimeout   = 5 * time.Minute
	DefaultServerPort          = 0
	MaxServerPort              = 65535
	defaultLinearAPIKeyEnv     = "LINEAR_API_KEY"
)

var (
	DefaultActiveStates   = []string{"Todo", "In Progress"}
	DefaultTerminalStates = []string{"Closed", "Cancelled", "Canceled", "Duplicate", "Done"}

	// ErrInvalidConfig marks a workflow config that cannot be used for dispatch preflight.
	ErrInvalidConfig = errors.New("invalid_config")

	maxIntValue = int64(^uint(0) >> 1)
	minIntValue = -maxIntValue - 1
)

// Config is the future typed view consumed by the orchestrator.
type Config struct {
	Tracker     Tracker
	Polling     Polling
	Server      Server
	StateStore  StateStore
	Workspace   Workspace
	Hooks       Hooks
	Agent       Agent
	Codex       Codex
	PromptBody  string
	WorkflowRef string
}

type Tracker struct {
	Kind           string
	Endpoint       string
	APIKey         string
	ProjectSlug    string
	ActiveStates   []string
	TerminalStates []string
	IssueFilter    IssueFilter
}

// IssueFilter is a Go-port tracker extension used to route issues within one
// Linear project to a single execution repository instance.
type IssueFilter struct {
	RequireLabels                []string
	RejectLabels                 []string
	RequireAnyLabels             []string
	RequireExactlyOneLabelPrefix string
}

type Polling struct {
	Interval time.Duration
}

type Server struct {
	Port int
}

type StateStore struct {
	Path         string
	InstanceID   string
	LeaseTimeout time.Duration
}

type Workspace struct {
	Root string
}

type Hooks struct {
	AfterCreate  string
	BeforeRun    string
	AfterRun     string
	BeforeRemove string
	Timeout      time.Duration
}

type Agent struct {
	MaxConcurrentAgents        int
	MaxTurns                   int
	MaxRetryBackoff            time.Duration
	MaxConcurrentAgentsByState map[string]int
}

type Codex struct {
	Command           string
	ApprovalPolicy    string
	ThreadSandbox     string
	TurnTimeout       time.Duration
	ReadTimeout       time.Duration
	StallTimeout      time.Duration
	TurnSandboxPolicy map[string]any
}

// Clone returns a copy of Config safe for callers to inspect without mutating
// the internally retained runtime snapshot.
func (cfg Config) Clone() Config {
	cloned := cfg
	cloned.Tracker.ActiveStates = cloneStrings(cfg.Tracker.ActiveStates)
	cloned.Tracker.TerminalStates = cloneStrings(cfg.Tracker.TerminalStates)
	cloned.Tracker.IssueFilter = cloneIssueFilter(cfg.Tracker.IssueFilter)
	cloned.Agent.MaxConcurrentAgentsByState = cloneIntMap(cfg.Agent.MaxConcurrentAgentsByState)
	cloned.Codex.TurnSandboxPolicy = cloneMap(cfg.Codex.TurnSandboxPolicy)
	return cloned
}

// FieldError records one invalid config field.
type FieldError struct {
	Field string
	Err   error
}

// ValidationError reports all config problems found in one pass.
type ValidationError struct {
	Problems []FieldError
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrInvalidConfig.Error()
	}

	parts := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		parts = append(parts, fmt.Sprintf("%s: %v", problem.Field, problem.Err))
	}
	return fmt.Sprintf("%s: %s", ErrInvalidConfig, strings.Join(parts, "; "))
}

func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidConfig
}

// Option customizes config resolution. It is primarily useful for deterministic tests.
type Option func(*options)

// WithEnv overrides environment lookup during $VAR resolution.
func WithEnv(env func(string) (string, bool)) Option {
	return func(opts *options) {
		if env != nil {
			opts.env = env
		}
	}
}

// WithHomeDir overrides the home directory used for ~ path expansion.
func WithHomeDir(homeDir string) Option {
	return func(opts *options) {
		opts.homeDir = homeDir
	}
}

// WithTempDir overrides the temp directory used for the default workspace root.
func WithTempDir(tempDir string) Option {
	return func(opts *options) {
		opts.tempDir = tempDir
	}
}

// Load reads a workflow file and returns its typed runtime config.
func Load(path string, opts ...Option) (Config, error) {
	def, err := workflow.Load(path)
	if err != nil {
		return Config{}, err
	}
	return FromWorkflow(def, opts...)
}

// FromWorkflow converts a raw workflow definition into typed runtime config.
func FromWorkflow(def workflow.Definition, opts ...Option) (Config, error) {
	resolver := newResolver(def, opts...)
	cfg := defaultConfig(resolver.opts.tempDir)
	cfg.PromptBody = def.PromptTemplate
	cfg.WorkflowRef = def.Path

	tracker := resolver.object(def.Config, "tracker", false)
	issueFilter := resolver.object(tracker, "issue_filter", false)
	polling := resolver.object(def.Config, "polling", false)
	server := resolver.object(def.Config, "server", false)
	stateStore := resolver.object(def.Config, "state_store", false)
	workspaceConfig := resolver.object(def.Config, "workspace", false)
	hooks := resolver.object(def.Config, "hooks", false)
	agent := resolver.object(def.Config, "agent", false)
	codex := resolver.object(def.Config, "codex", false)

	cfg.Tracker.Kind = strings.ToLower(strings.TrimSpace(resolver.stringField(tracker, "tracker.kind")))
	cfg.Tracker.Endpoint = strings.TrimSpace(resolver.stringFieldDefault(
		tracker,
		"tracker.endpoint",
		cfg.Tracker.Endpoint,
	))
	cfg.Tracker.APIKey = strings.TrimSpace(resolver.expandEnvToken(
		resolver.stringField(tracker, "tracker.api_key"),
		"tracker.api_key",
	))
	cfg.Tracker.ProjectSlug = strings.TrimSpace(resolver.stringField(tracker, "tracker.project_slug"))
	cfg.Tracker.ActiveStates = resolver.stringSliceFieldDefault(
		tracker,
		"tracker.active_states",
		cfg.Tracker.ActiveStates,
	)
	cfg.Tracker.TerminalStates = resolver.stringSliceFieldDefault(
		tracker,
		"tracker.terminal_states",
		cfg.Tracker.TerminalStates,
	)
	cfg.Tracker.IssueFilter.RequireLabels = resolver.stringSliceFieldDefault(
		issueFilter,
		"tracker.issue_filter.require_labels",
		cfg.Tracker.IssueFilter.RequireLabels,
	)
	cfg.Tracker.IssueFilter.RejectLabels = resolver.stringSliceFieldDefault(
		issueFilter,
		"tracker.issue_filter.reject_labels",
		cfg.Tracker.IssueFilter.RejectLabels,
	)
	cfg.Tracker.IssueFilter.RequireAnyLabels = resolver.stringSliceFieldDefault(
		issueFilter,
		"tracker.issue_filter.require_any_labels",
		cfg.Tracker.IssueFilter.RequireAnyLabels,
	)
	cfg.Tracker.IssueFilter.RequireExactlyOneLabelPrefix = strings.TrimSpace(
		resolver.stringField(issueFilter, "tracker.issue_filter.require_exactly_one_label_prefix"),
	)

	cfg.Polling.Interval = resolver.durationFieldDefault(
		polling,
		"polling.interval_ms",
		cfg.Polling.Interval,
		positiveDuration,
	)
	cfg.Server.Port = resolver.intFieldDefault(
		server,
		"server.port",
		cfg.Server.Port,
		validPort,
	)
	cfg.StateStore.Path = resolver.optionalPathField(stateStore, "state_store.path")
	cfg.StateStore.InstanceID = strings.TrimSpace(resolver.stringFieldDefault(
		stateStore,
		"state_store.instance_id",
		cfg.StateStore.InstanceID,
	))
	cfg.StateStore.LeaseTimeout = resolver.durationFieldDefault(
		stateStore,
		"state_store.lease_timeout_ms",
		cfg.StateStore.LeaseTimeout,
		positiveDuration,
	)

	workspaceRoot := resolver.stringFieldDefault(
		workspaceConfig,
		"workspace.root",
		cfg.Workspace.Root,
	)
	cfg.Workspace.Root = resolver.resolveWorkspaceRoot(workspaceRoot)

	cfg.Hooks.AfterCreate = resolver.stringField(hooks, "hooks.after_create")
	cfg.Hooks.BeforeRun = resolver.stringField(hooks, "hooks.before_run")
	cfg.Hooks.AfterRun = resolver.stringField(hooks, "hooks.after_run")
	cfg.Hooks.BeforeRemove = resolver.stringField(hooks, "hooks.before_remove")
	cfg.Hooks.Timeout = resolver.durationFieldDefault(
		hooks,
		"hooks.timeout_ms",
		cfg.Hooks.Timeout,
		positiveDuration,
	)

	cfg.Agent.MaxConcurrentAgents = resolver.intFieldDefault(
		agent,
		"agent.max_concurrent_agents",
		cfg.Agent.MaxConcurrentAgents,
		positiveInt,
	)
	cfg.Agent.MaxTurns = resolver.intFieldDefault(
		agent,
		"agent.max_turns",
		cfg.Agent.MaxTurns,
		positiveInt,
	)
	cfg.Agent.MaxRetryBackoff = resolver.durationFieldDefault(
		agent,
		"agent.max_retry_backoff_ms",
		cfg.Agent.MaxRetryBackoff,
		positiveDuration,
	)
	cfg.Agent.MaxConcurrentAgentsByState = resolver.positiveIntMapField(
		agent,
		"agent.max_concurrent_agents_by_state",
	)

	cfg.Codex.Command = strings.TrimSpace(resolver.stringFieldDefault(
		codex,
		"codex.command",
		cfg.Codex.Command,
	))
	cfg.Codex.ApprovalPolicy = strings.TrimSpace(resolver.stringField(codex, "codex.approval_policy"))
	cfg.Codex.ThreadSandbox = strings.TrimSpace(resolver.stringField(codex, "codex.thread_sandbox"))
	cfg.Codex.TurnSandboxPolicy = resolver.mapField(codex, "codex.turn_sandbox_policy")
	cfg.Codex.TurnTimeout = resolver.durationFieldDefault(
		codex,
		"codex.turn_timeout_ms",
		cfg.Codex.TurnTimeout,
		positiveDuration,
	)
	cfg.Codex.ReadTimeout = resolver.durationFieldDefault(
		codex,
		"codex.read_timeout_ms",
		cfg.Codex.ReadTimeout,
		positiveDuration,
	)
	cfg.Codex.StallTimeout = resolver.durationFieldDefault(
		codex,
		"codex.stall_timeout_ms",
		cfg.Codex.StallTimeout,
		anyDuration,
	)

	resolver.validateDispatchPreflight(cfg)
	if len(resolver.problems) > 0 {
		return Config{}, &ValidationError{Problems: resolver.problems}
	}
	return cfg, nil
}

type options struct {
	env     func(string) (string, bool)
	homeDir string
	tempDir string
}

type resolver struct {
	def      workflow.Definition
	opts     options
	problems []FieldError
}

func newResolver(def workflow.Definition, opts ...Option) *resolver {
	homeDir, _ := os.UserHomeDir()
	resolverOptions := options{
		env:     os.LookupEnv,
		homeDir: homeDir,
		tempDir: os.TempDir(),
	}
	for _, opt := range opts {
		opt(&resolverOptions)
	}
	return &resolver{def: def, opts: resolverOptions}
}

func defaultConfig(tempDir string) Config {
	return Config{
		Tracker: Tracker{
			Endpoint:       DefaultLinearEndpoint,
			ActiveStates:   cloneStrings(DefaultActiveStates),
			TerminalStates: cloneStrings(DefaultTerminalStates),
		},
		Polling: Polling{
			Interval: DefaultPollingInterval,
		},
		Server: Server{
			Port: DefaultServerPort,
		},
		StateStore: StateStore{
			LeaseTimeout: DefaultStateStoreLease,
		},
		Workspace: Workspace{
			Root: filepath.Join(tempDir, DefaultWorkspaceRootBase),
		},
		Hooks: Hooks{
			Timeout: DefaultHookTimeout,
		},
		Agent: Agent{
			MaxConcurrentAgents:        DefaultMaxConcurrentAgents,
			MaxTurns:                   DefaultMaxTurns,
			MaxRetryBackoff:            DefaultMaxRetryBackoff,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Codex: Codex{
			Command:           DefaultCodexCommand,
			TurnTimeout:       DefaultCodexTurnTimeout,
			ReadTimeout:       DefaultCodexReadTimeout,
			StallTimeout:      DefaultCodexStallTimeout,
			TurnSandboxPolicy: map[string]any{},
		},
	}
}

func (r *resolver) object(root map[string]any, key string, required bool) map[string]any {
	raw, ok := root[key]
	if !ok || raw == nil {
		if required {
			r.add(key, errors.New("required object is missing"))
		}
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		r.add(key, fmt.Errorf("must be an object, got %T", raw))
		return nil
	}
	return obj
}

func (r *resolver) optionalPathField(obj map[string]any, field string) string {
	raw, ok := lookup(obj, field)
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		r.add(field, fmt.Errorf("must be a string, got %T", raw))
		return ""
	}
	expanded := strings.TrimSpace(r.expandEnv(value, field))
	if expanded == "" {
		return ""
	}
	if strings.ContainsRune(expanded, 0) {
		r.add(field, errors.New("must not contain NUL bytes"))
		return ""
	}
	expanded = r.expandHome(expanded, field)
	if expanded == "" {
		return ""
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(workflowDir(r.def.Path), expanded)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		r.add(field, fmt.Errorf("cannot be normalized: %w", err))
		return ""
	}
	return filepath.Clean(absolute)
}

func (r *resolver) stringField(obj map[string]any, field string) string {
	raw, ok := lookup(obj, field)
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		r.add(field, fmt.Errorf("must be a string, got %T", raw))
		return ""
	}
	return value
}

func (r *resolver) stringFieldDefault(obj map[string]any, field string, fallback string) string {
	if _, ok := lookup(obj, field); !ok {
		return fallback
	}
	return r.stringField(obj, field)
}

func (r *resolver) stringSliceFieldDefault(obj map[string]any, field string, fallback []string) []string {
	raw, ok := lookup(obj, field)
	if !ok || raw == nil {
		return cloneStrings(fallback)
	}

	switch values := raw.(type) {
	case []any:
		result := make([]string, 0, len(values))
		for i, item := range values {
			value, ok := item.(string)
			if !ok {
				r.add(fmt.Sprintf("%s[%d]", field, i), fmt.Errorf("must be a string, got %T", item))
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				r.add(fmt.Sprintf("%s[%d]", field, i), errors.New("must not be empty"))
				continue
			}
			result = append(result, value)
		}
		return result
	case []string:
		result := make([]string, 0, len(values))
		for i, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				r.add(fmt.Sprintf("%s[%d]", field, i), errors.New("must not be empty"))
				continue
			}
			result = append(result, value)
		}
		return result
	default:
		r.add(field, fmt.Errorf("must be a list of strings, got %T", raw))
		return cloneStrings(fallback)
	}
}

func (r *resolver) intFieldDefault(
	obj map[string]any,
	field string,
	fallback int,
	validate func(int) error,
) int {
	raw, ok := lookup(obj, field)
	if !ok || raw == nil {
		return fallback
	}
	value, ok := coerceInt(raw)
	if !ok {
		r.add(field, fmt.Errorf("must be an integer, got %T", raw))
		return fallback
	}
	if err := validate(value); err != nil {
		r.add(field, err)
		return fallback
	}
	return value
}

func (r *resolver) durationFieldDefault(
	obj map[string]any,
	field string,
	fallback time.Duration,
	validate func(time.Duration) error,
) time.Duration {
	ms := r.intFieldDefault(obj, field, millis(fallback), validateMillis)
	duration := time.Duration(ms) * time.Millisecond
	if err := validate(duration); err != nil {
		r.add(field, err)
		return fallback
	}
	return duration
}

func (r *resolver) positiveIntMapField(obj map[string]any, field string) map[string]int {
	raw, ok := lookup(obj, field)
	if !ok || raw == nil {
		return map[string]int{}
	}
	values, ok := raw.(map[string]any)
	if !ok {
		if typedValues, typedOK := raw.(map[string]int); typedOK {
			result := make(map[string]int, len(typedValues))
			for rawKey, value := range typedValues {
				key := strings.ToLower(strings.TrimSpace(rawKey))
				if key == "" || value <= 0 {
					continue
				}
				result[key] = value
			}
			return result
		}
		r.add(field, fmt.Errorf("must be a map of positive integers, got %T", raw))
		return map[string]int{}
	}

	result := make(map[string]int, len(values))
	for rawKey, rawValue := range values {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			continue
		}
		value, ok := coerceInt(rawValue)
		if !ok || value <= 0 {
			continue
		}
		result[key] = value
	}
	return result
}

func (r *resolver) mapField(obj map[string]any, field string) map[string]any {
	raw, ok := lookup(obj, field)
	if !ok || raw == nil {
		return map[string]any{}
	}
	value, ok := raw.(map[string]any)
	if !ok {
		r.add(field, fmt.Errorf("must be an object, got %T", raw))
		return map[string]any{}
	}
	return cloneMap(value)
}

func (r *resolver) expandEnv(value string, field string) string {
	if !strings.Contains(value, "$") {
		return value
	}

	return os.Expand(value, func(name string) string {
		resolved, ok := r.opts.env(name)
		if !ok || resolved == "" {
			r.add(field, fmt.Errorf("environment variable %q is not set or is empty", name))
			return ""
		}
		return resolved
	})
}

func (r *resolver) expandEnvToken(value string, field string) string {
	trimmed := strings.TrimSpace(value)
	name, ok := envTokenName(trimmed)
	if !ok {
		return value
	}
	resolved, exists := r.opts.env(name)
	if !exists || resolved == "" {
		r.add(field, fmt.Errorf("environment variable %q is not set or is empty", name))
		return ""
	}
	return resolved
}

func (r *resolver) resolveWorkspaceRoot(value string) string {
	expanded := strings.TrimSpace(r.expandEnv(value, "workspace.root"))
	if expanded == "" {
		r.add("workspace.root", errors.New("must not be empty"))
		return ""
	}
	if strings.ContainsRune(expanded, 0) {
		r.add("workspace.root", errors.New("must not contain NUL bytes"))
		return ""
	}

	expanded = r.expandHome(expanded, "workspace.root")
	if expanded == "" {
		return ""
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(workflowDir(r.def.Path), expanded)
	}

	absolute, err := filepath.Abs(expanded)
	if err != nil {
		r.add("workspace.root", fmt.Errorf("cannot be normalized: %w", err))
		return ""
	}
	absolute = filepath.Clean(absolute)
	if isFilesystemRoot(absolute) {
		r.add("workspace.root", errors.New("must not be the filesystem root"))
		return ""
	}
	return absolute
}

func (r *resolver) expandHome(value string, field string) string {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value
	}
	if r.opts.homeDir == "" {
		r.add(field, errors.New("cannot expand ~ because home directory is unavailable"))
		return ""
	}
	if value == "~" {
		return r.opts.homeDir
	}
	return filepath.Join(r.opts.homeDir, strings.TrimPrefix(value, "~/"))
}

func (r *resolver) validateDispatchPreflight(cfg Config) {
	switch cfg.Tracker.Kind {
	case "":
		r.add("tracker.kind", errors.New("is required"))
	case TrackerKindLinear:
		if cfg.Tracker.Endpoint == "" {
			r.add("tracker.endpoint", errors.New("is required for linear tracker"))
		}
		if cfg.Tracker.APIKey == "" {
			r.add("tracker.api_key", fmt.Errorf("is required for linear tracker; use %s or a literal token", defaultLinearAPIKeyEnv))
		}
		if cfg.Tracker.ProjectSlug == "" {
			r.add("tracker.project_slug", errors.New("is required for linear tracker"))
		}
	default:
		r.add("tracker.kind", fmt.Errorf("unsupported tracker kind %q", cfg.Tracker.Kind))
	}

	if len(cfg.Tracker.ActiveStates) == 0 {
		r.add("tracker.active_states", errors.New("must contain at least one state"))
	}
	if len(cfg.Tracker.TerminalStates) == 0 {
		r.add("tracker.terminal_states", errors.New("must contain at least one state"))
	}
	if cfg.Codex.Command == "" {
		r.add("codex.command", errors.New("is required"))
	}
}

func (r *resolver) add(field string, err error) {
	r.problems = append(r.problems, FieldError{Field: field, Err: err})
}

func lookup(obj map[string]any, field string) (any, bool) {
	if obj == nil {
		return nil, false
	}
	key := field
	if dot := strings.LastIndex(field, "."); dot >= 0 {
		key = field[dot+1:]
	}
	value, exists := obj[key]
	return value, exists
}

func coerceInt(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		if value > maxIntValue || value < minIntValue {
			return 0, false
		}
		return int(value), true
	case uint:
		if uint64(value) > uint64(maxIntValue) {
			return 0, false
		}
		return int(value), true
	case uint8:
		return int(value), true
	case uint16:
		return int(value), true
	case uint32:
		if uint64(value) > uint64(maxIntValue) {
			return 0, false
		}
		return int(value), true
	case uint64:
		if value > uint64(maxIntValue) {
			return 0, false
		}
		return int(value), true
	case float64:
		if math.Trunc(value) != value || value > float64(maxIntValue) || value < float64(minIntValue) {
			return 0, false
		}
		return int(value), true
	default:
		return 0, false
	}
}

func envTokenName(value string) (string, bool) {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		name := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
		return name, isEnvName(name)
	}
	if !strings.HasPrefix(value, "$") {
		return "", false
	}
	name := strings.TrimPrefix(value, "$")
	return name, isEnvName(name)
}

func isEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		valid := r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
		if i > 0 {
			valid = valid || r >= '0' && r <= '9'
		}
		if !valid {
			return false
		}
	}
	return true
}

func positiveInt(value int) error {
	if value <= 0 {
		return errors.New("must be positive")
	}
	return nil
}

func validPort(value int) error {
	if value < 0 || value > MaxServerPort {
		return fmt.Errorf("must be between 0 and %d", MaxServerPort)
	}
	return nil
}

func validateMillis(value int) error {
	const maxMillis = int64(time.Duration(1<<63-1) / time.Millisecond)
	if int64(value) > maxMillis || int64(value) < -maxMillis {
		return errors.New("duration milliseconds overflow time.Duration")
	}
	return nil
}

func positiveDuration(value time.Duration) error {
	if value <= 0 {
		return errors.New("must be positive")
	}
	return nil
}

func anyDuration(time.Duration) error {
	return nil
}

func millis(value time.Duration) int {
	return int(value.Milliseconds())
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneMap(values map[string]any) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = cloneAny(value)
	}
	return result
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = cloneAny(item)
		}
		return result
	default:
		return value
	}
}

func cloneIntMap(values map[string]int) map[string]int {
	result := make(map[string]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneIssueFilter(value IssueFilter) IssueFilter {
	return IssueFilter{
		RequireLabels:                cloneStrings(value.RequireLabels),
		RejectLabels:                 cloneStrings(value.RejectLabels),
		RequireAnyLabels:             cloneStrings(value.RequireAnyLabels),
		RequireExactlyOneLabelPrefix: value.RequireExactlyOneLabelPrefix,
	}
}

func workflowDir(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}

func isFilesystemRoot(path string) bool {
	cleaned := filepath.Clean(path)
	return filepath.Dir(cleaned) == cleaned
}
