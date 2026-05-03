package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/config"
)

var (
	// ErrInvalidWorkspaceRoot marks a workspace root that cannot safely host per-issue directories.
	ErrInvalidWorkspaceRoot = errors.New("invalid_workspace_root")

	// ErrInvalidIssueIdentifier marks an issue identifier that cannot produce a safe workspace key.
	ErrInvalidIssueIdentifier = errors.New("invalid_issue_identifier")

	// ErrUnsafeWorkspacePath marks a computed path outside the configured workspace root.
	ErrUnsafeWorkspacePath = errors.New("unsafe_workspace_path")

	// ErrWorkspacePathNotDirectory marks an existing workspace path that is not a real directory.
	ErrWorkspacePathNotDirectory = errors.New("workspace_path_not_directory")

	// ErrWorkspaceMetadataConflict marks a workspace key already claimed by different issue metadata.
	ErrWorkspaceMetadataConflict = errors.New("workspace_metadata_conflict")
)

const MetadataFilename = ".symphony-workspace.json"

// Workspace identifies a filesystem directory assigned to one issue.
type Workspace struct {
	Path         string
	Key          string
	CreatedNow   bool
	IssueID      string
	MetadataPath string
	// IssueKey preserves the tracker-visible issue identifier that produced Key.
	IssueKey     string
	WorkflowPath string
}

// Metadata is the durable per-workspace claim used to detect sanitized-key collisions.
type Metadata struct {
	WorkspaceKey    string `json:"workspace_key"`
	IssueID         string `json:"issue_id,omitempty"`
	IssueIdentifier string `json:"issue_identifier"`
	WorkflowPath    string `json:"workflow_path,omitempty"`
}

// PrepareRequest contains the issue metadata needed to assign a workspace.
type PrepareRequest struct {
	IssueID         string
	IssueIdentifier string
	WorkflowPath    string
}

// CleanupRequest contains issue metadata needed to locate a terminal workspace.
type CleanupRequest struct {
	IssueID         string
	IssueIdentifier string
	WorkflowPath    string
}

// CleanupTarget is the resolved workspace path for a terminal cleanup attempt.
type CleanupTarget struct {
	Workspace
	Exists          bool
	IsRealDirectory bool
}

// CleanupResult records the outcome of a workspace removal attempt.
type CleanupResult struct {
	Target  CleanupTarget
	Removed bool
}

// Manager creates and reuses per-issue workspaces under one configured root.
type Manager struct {
	root string
}

// New creates a workspace manager from a raw workspace root.
func New(root string) (*Manager, error) {
	normalized, err := normalizeRoot(root)
	if err != nil {
		return nil, err
	}
	return &Manager{root: normalized}, nil
}

// NewManager creates a workspace manager from the typed workflow config.
func NewManager(cfg config.Workspace) (*Manager, error) {
	return New(cfg.Root)
}

// Root returns the normalized absolute workspace root used by this manager.
func (m *Manager) Root() string {
	if m == nil {
		return ""
	}
	return m.root
}

// Prepare creates or reuses the per-issue directory for req and returns metadata
// for later hook and agent execution.
func (m *Manager) Prepare(req PrepareRequest) (Workspace, error) {
	if m == nil || m.root == "" {
		return Workspace{}, fmt.Errorf("%w: manager root is empty", ErrInvalidWorkspaceRoot)
	}

	key, err := SanitizeIdentifier(req.IssueIdentifier)
	if err != nil {
		return Workspace{}, err
	}

	if err := ensureRealDirectory(m.root); err != nil {
		return Workspace{}, err
	}

	workspacePath := filepath.Clean(filepath.Join(m.root, key))
	workspacePath, err = filepath.Abs(workspacePath)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: cannot normalize %q: %w", ErrUnsafeWorkspacePath, workspacePath, err)
	}
	workspacePath = filepath.Clean(workspacePath)
	if !isChildPath(m.root, workspacePath) {
		return Workspace{}, fmt.Errorf("%w: %q is not under %q", ErrUnsafeWorkspacePath, workspacePath, m.root)
	}

	createdNow, err := ensureIssueDirectory(workspacePath)
	if err != nil {
		return Workspace{}, err
	}

	result := Workspace{
		Path:         workspacePath,
		Key:          key,
		CreatedNow:   createdNow,
		IssueID:      req.IssueID,
		MetadataPath: filepath.Join(workspacePath, MetadataFilename),
		IssueKey:     req.IssueIdentifier,
		WorkflowPath: req.WorkflowPath,
	}
	if err := writeOrValidateMetadata(result); err != nil {
		return Workspace{}, err
	}
	return result, nil
}

// CleanupTarget resolves the per-issue workspace path without creating any
// filesystem entries. Missing workspace roots or issue directories are treated
// as no-op cleanup targets.
func (m *Manager) CleanupTarget(req CleanupRequest) (CleanupTarget, error) {
	if m == nil || m.root == "" {
		return CleanupTarget{}, fmt.Errorf("%w: manager root is empty", ErrInvalidWorkspaceRoot)
	}

	key, err := SanitizeIdentifier(req.IssueIdentifier)
	if err != nil {
		return CleanupTarget{}, err
	}
	workspacePath, err := m.workspacePathForKey(key)
	if err != nil {
		return CleanupTarget{}, err
	}

	target := CleanupTarget{
		Workspace: Workspace{
			Path:         workspacePath,
			Key:          key,
			IssueID:      req.IssueID,
			IssueKey:     req.IssueIdentifier,
			WorkflowPath: req.WorkflowPath,
			MetadataPath: filepath.Join(workspacePath, MetadataFilename),
		},
	}

	info, err := os.Lstat(workspacePath)
	if errors.Is(err, os.ErrNotExist) {
		return target, nil
	}
	if err != nil {
		return CleanupTarget{}, fmt.Errorf("inspect workspace path %q: %w", workspacePath, err)
	}

	target.Exists = true
	target.IsRealDirectory = isRealDirectory(info)
	return target, nil
}

// Remove deletes a previously resolved cleanup target. It is safe to call for
// missing targets and removes only the sanitized path under the manager root.
func (m *Manager) Remove(target CleanupTarget) (CleanupResult, error) {
	if m == nil || m.root == "" {
		return CleanupResult{}, fmt.Errorf("%w: manager root is empty", ErrInvalidWorkspaceRoot)
	}
	if target.Path == "" || !isChildPath(m.root, filepath.Clean(target.Path)) {
		return CleanupResult{}, fmt.Errorf("%w: %q is not under %q", ErrUnsafeWorkspacePath, target.Path, m.root)
	}

	result := CleanupResult{Target: target}
	if !target.Exists {
		return result, nil
	}
	if err := os.RemoveAll(target.Path); err != nil {
		return result, fmt.Errorf("remove workspace path %q: %w", target.Path, err)
	}
	result.Removed = true
	return result, nil
}

// Cleanup resolves and removes a terminal workspace without running lifecycle
// hooks. Orchestrator code should prefer CleanupTarget + before_remove + Remove.
func (m *Manager) Cleanup(req CleanupRequest) (CleanupResult, error) {
	target, err := m.CleanupTarget(req)
	if err != nil {
		return CleanupResult{}, err
	}
	return m.Remove(target)
}

// SanitizeIdentifier converts an issue identifier into a deterministic workspace key.
func SanitizeIdentifier(identifier string) (string, error) {
	if identifier == "" {
		return "", fmt.Errorf("%w: empty identifier", ErrInvalidIssueIdentifier)
	}

	var builder strings.Builder
	builder.Grow(len(identifier))
	for _, r := range identifier {
		if isAllowedKeyRune(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}

	key := builder.String()
	if key == "" || key == "." || key == ".." {
		return "", fmt.Errorf("%w: sanitized key %q is reserved", ErrInvalidIssueIdentifier, key)
	}
	return key, nil
}

func normalizeRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%w: root must not be empty", ErrInvalidWorkspaceRoot)
	}
	if strings.ContainsRune(root, 0) {
		return "", fmt.Errorf("%w: root must not contain NUL bytes", ErrInvalidWorkspaceRoot)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: cannot normalize %q: %w", ErrInvalidWorkspaceRoot, root, err)
	}
	absolute = filepath.Clean(absolute)
	if isFilesystemRoot(absolute) {
		return "", fmt.Errorf("%w: root must not be the filesystem root", ErrInvalidWorkspaceRoot)
	}
	return absolute, nil
}

func (m *Manager) workspacePathForKey(key string) (string, error) {
	workspacePath := filepath.Clean(filepath.Join(m.root, key))
	workspacePath, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", fmt.Errorf("%w: cannot normalize %q: %w", ErrUnsafeWorkspacePath, workspacePath, err)
	}
	workspacePath = filepath.Clean(workspacePath)
	if !isChildPath(m.root, workspacePath) {
		return "", fmt.Errorf("%w: %q is not under %q", ErrUnsafeWorkspacePath, workspacePath, m.root)
	}
	return workspacePath, nil
}

func ensureIssueDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !isRealDirectory(info) {
			return false, fmt.Errorf("%w: %q exists but is not a real directory", ErrWorkspacePathNotDirectory, path)
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect workspace path %q: %w", path, err)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return false, fmt.Errorf("create workspace path %q: %w", path, err)
	}
	info, err = os.Lstat(path)
	if err != nil {
		return false, fmt.Errorf("inspect created workspace path %q: %w", path, err)
	}
	if !isRealDirectory(info) {
		return false, fmt.Errorf("%w: %q exists but is not a real directory", ErrWorkspacePathNotDirectory, path)
	}
	return true, nil
}

func ensureRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create workspace root %q: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect workspace root %q: %w", path, err)
	}
	if !isRealDirectory(info) {
		return fmt.Errorf("%w: root %q exists but is not a real directory", ErrWorkspacePathNotDirectory, path)
	}
	return nil
}

func writeOrValidateMetadata(workspace Workspace) error {
	next := Metadata{
		WorkspaceKey:    workspace.Key,
		IssueID:         workspace.IssueID,
		IssueIdentifier: workspace.IssueKey,
		WorkflowPath:    workspace.WorkflowPath,
	}
	current, ok, err := readMetadata(workspace.MetadataPath)
	if err != nil {
		return err
	}
	if ok {
		if current.WorkspaceKey != next.WorkspaceKey || current.IssueIdentifier != next.IssueIdentifier {
			return fmt.Errorf(
				"%w: workspace key %q is already claimed by issue identifier %q",
				ErrWorkspaceMetadataConflict,
				workspace.Key,
				current.IssueIdentifier,
			)
		}
		if current.IssueID != "" && next.IssueID != "" && current.IssueID != next.IssueID {
			return fmt.Errorf(
				"%w: workspace key %q is already claimed by issue id %q",
				ErrWorkspaceMetadataConflict,
				workspace.Key,
				current.IssueID,
			)
		}
		if next.IssueID == "" {
			next.IssueID = current.IssueID
		}
		if next.WorkflowPath == "" {
			next.WorkflowPath = current.WorkflowPath
		}
	}
	return writeMetadata(workspace.MetadataPath, next)
}

func readMetadata(path string) (Metadata, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, fmt.Errorf("inspect workspace metadata %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Metadata{}, false, fmt.Errorf("%w: metadata path %q is not a regular file", ErrWorkspaceMetadataConflict, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, false, fmt.Errorf("read workspace metadata %q: %w", path, err)
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, false, fmt.Errorf("%w: metadata path %q contains invalid JSON: %w", ErrWorkspaceMetadataConflict, path, err)
	}
	if metadata.WorkspaceKey == "" || metadata.IssueIdentifier == "" {
		return Metadata{}, false, fmt.Errorf("%w: metadata path %q is missing workspace claim fields", ErrWorkspaceMetadataConflict, path)
	}
	return metadata, true, nil
}

func writeMetadata(path string, metadata Metadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace metadata: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".symphony-workspace-*.tmp")
	if err != nil {
		return fmt.Errorf("create workspace metadata temp file in %q: %w", dir, err)
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)

	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write workspace metadata temp file %q: %w", tmpPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close workspace metadata temp file %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace workspace metadata %q: %w", path, err)
	}
	return nil
}

func isRealDirectory(info os.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func isChildPath(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func isAllowedKeyRune(r rune) bool {
	return r >= 'A' && r <= 'Z' ||
		r >= 'a' && r <= 'z' ||
		r >= '0' && r <= '9' ||
		r == '.' || r == '_' || r == '-'
}

func isFilesystemRoot(path string) bool {
	cleaned := filepath.Clean(path)
	return filepath.Dir(cleaned) == cleaned
}
