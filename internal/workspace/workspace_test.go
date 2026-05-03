package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SisyphusSQ/symphony-go/internal/config"
)

func TestSanitizeIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{
			name:       "already safe",
			identifier: "TOO-120.alpha_beta",
			want:       "TOO-120.alpha_beta",
		},
		{
			name:       "path traversal characters become separators-free key",
			identifier: "../outside",
			want:       ".._outside",
		},
		{
			name:       "unicode and spaces become underscores",
			identifier: "项目 120",
			want:       "___120",
		},
		{
			name:       "shell metacharacters become underscores",
			identifier: "TOO/120:$HOME",
			want:       "TOO_120__HOME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeIdentifier(tt.identifier)
			if err != nil {
				t.Fatalf("SanitizeIdentifier() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SanitizeIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeIdentifierRejectsReservedKeys(t *testing.T) {
	for _, identifier := range []string{"", ".", ".."} {
		t.Run(identifier, func(t *testing.T) {
			_, err := SanitizeIdentifier(identifier)
			if !errors.Is(err, ErrInvalidIssueIdentifier) {
				t.Fatalf("expected ErrInvalidIssueIdentifier, got %v", err)
			}
		})
	}
}

func TestManagerCreatesNewWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	manager := mustManager(t, root)

	got, err := manager.Prepare(PrepareRequest{
		IssueID:         "linear-id",
		IssueIdentifier: "TOO-120",
		WorkflowPath:    filepath.Join(t.TempDir(), "WORKFLOW.md"),
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	wantPath := filepath.Join(root, "TOO-120")
	if got.Path != wantPath {
		t.Fatalf("Path = %q, want %q", got.Path, wantPath)
	}
	if got.Key != "TOO-120" {
		t.Fatalf("Key = %q, want TOO-120", got.Key)
	}
	if !got.CreatedNow {
		t.Fatal("CreatedNow = false, want true")
	}
	if got.IssueID != "linear-id" || got.IssueKey != "TOO-120" || got.WorkflowPath == "" || got.MetadataPath == "" {
		t.Fatalf("metadata not preserved: %#v", got)
	}
	assertDirectory(t, got.Path)
	assertMetadata(t, got.MetadataPath, Metadata{
		WorkspaceKey:    "TOO-120",
		IssueID:         "linear-id",
		IssueIdentifier: "TOO-120",
		WorkflowPath:    got.WorkflowPath,
	})
}

func TestManagerReusesExistingWorkspace(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "TOO-120")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatalf("create existing workspace: %v", err)
	}
	manager := mustManager(t, root)

	got, err := manager.Prepare(PrepareRequest{IssueIdentifier: "TOO-120"})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if got.Path != existing {
		t.Fatalf("Path = %q, want %q", got.Path, existing)
	}
	if got.CreatedNow {
		t.Fatal("CreatedNow = true, want false for existing directory")
	}
}

func TestManagerKeepsPreparedPathInsideRootAfterSanitize(t *testing.T) {
	root := t.TempDir()
	manager := mustManager(t, root)

	got, err := manager.Prepare(PrepareRequest{IssueIdentifier: "../outside"})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	rel, err := filepath.Rel(root, got.Path)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	if rel != ".._outside" {
		t.Fatalf("relative path = %q, want sanitized key", rel)
	}
	if got.Path == filepath.Dir(root) || got.Key != rel {
		t.Fatalf("workspace escaped or key/path mismatch: workspace=%#v rel=%q", got, rel)
	}
	assertDirectory(t, got.Path)
}

func TestManagerRejectsSanitizeCollisionWithMetadata(t *testing.T) {
	root := t.TempDir()
	manager := mustManager(t, root)

	first, err := manager.Prepare(PrepareRequest{
		IssueID:         "first-id",
		IssueIdentifier: "A/B",
	})
	if err != nil {
		t.Fatalf("Prepare(first) error = %v", err)
	}
	if first.Key != "A_B" {
		t.Fatalf("first Key = %q, want A_B", first.Key)
	}

	_, err = manager.Prepare(PrepareRequest{
		IssueID:         "second-id",
		IssueIdentifier: "A_B",
	})
	if !errors.Is(err, ErrWorkspaceMetadataConflict) {
		t.Fatalf("expected ErrWorkspaceMetadataConflict, got %v", err)
	}
}

func TestManagerDoesNotWeakenExistingMetadata(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(t.TempDir(), "WORKFLOW.md")
	manager := mustManager(t, root)

	first, err := manager.Prepare(PrepareRequest{
		IssueID:         "linear-id",
		IssueIdentifier: "TOO-120",
		WorkflowPath:    workflowPath,
	})
	if err != nil {
		t.Fatalf("Prepare(first) error = %v", err)
	}

	second, err := manager.Prepare(PrepareRequest{IssueIdentifier: "TOO-120"})
	if err != nil {
		t.Fatalf("Prepare(second) error = %v", err)
	}
	if second.CreatedNow {
		t.Fatal("CreatedNow = true, want false for existing workspace")
	}
	assertMetadata(t, first.MetadataPath, Metadata{
		WorkspaceKey:    "TOO-120",
		IssueID:         "linear-id",
		IssueIdentifier: "TOO-120",
		WorkflowPath:    workflowPath,
	})
}

func TestManagerRejectsUnsafeRoot(t *testing.T) {
	for _, root := range []string{"", string(filepath.Separator)} {
		t.Run(root, func(t *testing.T) {
			_, err := New(root)
			if !errors.Is(err, ErrInvalidWorkspaceRoot) {
				t.Fatalf("expected ErrInvalidWorkspaceRoot, got %v", err)
			}
		})
	}
}

func TestManagerFailsWhenWorkspacePathIsFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "TOO-120")
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write file workspace path: %v", err)
	}
	manager := mustManager(t, root)

	_, err := manager.Prepare(PrepareRequest{IssueIdentifier: "TOO-120"})
	if !errors.Is(err, ErrWorkspacePathNotDirectory) {
		t.Fatalf("expected ErrWorkspacePathNotDirectory, got %v", err)
	}
}

func TestManagerFailsWhenWorkspacePathIsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "TOO-120")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	manager := mustManager(t, root)

	_, err := manager.Prepare(PrepareRequest{IssueIdentifier: "TOO-120"})
	if !errors.Is(err, ErrWorkspacePathNotDirectory) {
		t.Fatalf("expected ErrWorkspacePathNotDirectory, got %v", err)
	}
}

func TestNewManagerUsesTypedConfigWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "configured")
	manager, err := NewManager(config.Workspace{Root: root})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if manager.Root() != root {
		t.Fatalf("Root() = %q, want %q", manager.Root(), root)
	}
}

func TestManagerCleanupTargetDoesNotCreateMissingWorkspace(t *testing.T) {
	root := t.TempDir()
	manager := mustManager(t, root)

	target, err := manager.CleanupTarget(CleanupRequest{IssueIdentifier: "TOO-125"})
	if err != nil {
		t.Fatalf("CleanupTarget() error = %v", err)
	}
	if target.Exists {
		t.Fatalf("Exists = true, want false for missing workspace")
	}
	if target.Path != filepath.Join(root, "TOO-125") {
		t.Fatalf("Path = %q, want resolved workspace path", target.Path)
	}
	if _, err := os.Stat(target.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace path stat error = %v, want not exist", err)
	}
}

func TestManagerCleanupRemovesExistingWorkspace(t *testing.T) {
	root := t.TempDir()
	manager := mustManager(t, root)
	prepared, err := manager.Prepare(PrepareRequest{
		IssueID:         "linear-id",
		IssueIdentifier: "TOO-125",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	result, err := manager.Cleanup(CleanupRequest{
		IssueID:         "linear-id",
		IssueIdentifier: "TOO-125",
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.Target.Exists || !result.Target.IsRealDirectory || !result.Removed {
		t.Fatalf("Cleanup() = %#v, want existing real directory removed", result)
	}
	if _, err := os.Stat(prepared.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace path stat error = %v, want not exist", err)
	}
}

func mustManager(t *testing.T, root string) *Manager {
	t.Helper()

	manager, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager
}

func assertDirectory(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", path)
	}
}

func assertMetadata(t *testing.T, path string, want Metadata) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metadata %q: %v", path, err)
	}
	var got Metadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal metadata %q: %v", path, err)
	}
	if got != want {
		t.Fatalf("metadata = %#v, want %#v", got, want)
	}
}
