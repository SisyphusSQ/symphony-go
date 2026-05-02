package config

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/SisyphusSQ/symphony-go/internal/workflow"
)

var ErrReloadInvalid = errors.New("workflow_reload_invalid")

// ReloadStatus is the deterministic outcome class for a workflow reload check.
type ReloadStatus string

const (
	ReloadUnchanged ReloadStatus = "unchanged"
	ReloadApplied   ReloadStatus = "applied"
	ReloadInvalid   ReloadStatus = "invalid"
)

// ReloadError wraps a failed dynamic reload while documenting that the runtime
// continues with the last known good config.
type ReloadError struct {
	Path string
	Err  error
}

func (e *ReloadError) Error() string {
	return fmt.Sprintf(
		"%s: workflow %q reload rejected; keeping last known good config: %v",
		ErrReloadInvalid,
		e.Path,
		e.Err,
	)
}

func (e *ReloadError) Unwrap() error {
	return e.Err
}

func (e *ReloadError) Is(target error) bool {
	return target == ErrReloadInvalid
}

// ReloadResult records one reload check outcome. Config is always the effective
// last known good config after the check.
type ReloadResult struct {
	Status   ReloadStatus
	Changed  bool
	Config   Config
	Previous Config
	Err      error
}

// OperatorMessage returns a stable log message for the reload outcome.
func (r ReloadResult) OperatorMessage() string {
	path := r.Config.WorkflowRef
	if path == "" {
		path = r.Previous.WorkflowRef
	}

	switch r.Status {
	case ReloadApplied:
		return fmt.Sprintf("workflow_reload_applied path=%q", path)
	case ReloadInvalid:
		return fmt.Sprintf(
			"workflow_reload_invalid path=%q keeping_last_known_good=true error=%v",
			path,
			r.Err,
		)
	default:
		return fmt.Sprintf("workflow_reload_unchanged path=%q", path)
	}
}

// Reloader polls the selected workflow file by content hash and retains the
// last known good typed config. It is intentionally passive; the orchestrator
// event loop owns when to call ReloadIfChanged.
type Reloader struct {
	path         string
	opts         []Option
	current      Config
	observedHash [sha256.Size]byte
}

// NewReloader performs the startup workflow load. Startup errors are returned
// directly because no last-known-good config exists yet.
func NewReloader(path string, opts ...Option) (*Reloader, error) {
	cfg, digest, _, err := loadConfigCandidate(path, opts)
	if err != nil {
		return nil, err
	}
	return &Reloader{
		path:         path,
		opts:         append([]Option(nil), opts...),
		current:      cfg.Clone(),
		observedHash: digest,
	}, nil
}

// Current returns the current last known good config.
func (r *Reloader) Current() Config {
	return r.current.Clone()
}

// ReloadIfChanged re-reads the workflow when file content changes. Valid
// reloads replace the current config; invalid reloads keep the last known good
// config and return ErrReloadInvalid.
func (r *Reloader) ReloadIfChanged() ReloadResult {
	cfg, digest, hasDigest, err := loadConfigCandidate(r.path, r.opts)
	if hasDigest && digest == r.observedHash {
		current := r.current.Clone()
		return ReloadResult{
			Status:  ReloadUnchanged,
			Changed: false,
			Config:  current,
		}
	}

	previous := r.current.Clone()
	if err != nil {
		if hasDigest {
			r.observedHash = digest
		}
		reloadErr := &ReloadError{Path: r.path, Err: err}
		return ReloadResult{
			Status:   ReloadInvalid,
			Changed:  true,
			Config:   previous,
			Previous: previous,
			Err:      reloadErr,
		}
	}

	r.current = cfg.Clone()
	r.observedHash = digest
	current := r.current.Clone()
	return ReloadResult{
		Status:   ReloadApplied,
		Changed:  true,
		Config:   current,
		Previous: previous,
	}
}

func loadConfigCandidate(path string, opts []Option) (Config, [sha256.Size]byte, bool, error) {
	definition, data, err := workflow.LoadBytes(path)
	if len(data) == 0 {
		return Config{}, [sha256.Size]byte{}, false, err
	}

	digest := sha256.Sum256(data)
	if err != nil {
		return Config{}, digest, true, err
	}

	cfg, err := FromWorkflow(definition, opts...)
	if err != nil {
		return Config{}, digest, true, err
	}
	return cfg, digest, true, nil
}
