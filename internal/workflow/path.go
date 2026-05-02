package workflow

import (
	"errors"
	"fmt"
	"os"
)

const DefaultPath = "./WORKFLOW.md"

var (
	// ErrMissingWorkflowFile marks a workflow path that does not exist.
	ErrMissingWorkflowFile = errors.New("missing_workflow_file")

	// ErrWorkflowPathIsDirectory marks a workflow path that points at a directory.
	ErrWorkflowPathIsDirectory = errors.New("workflow_path_is_directory")
)

// ResolvePath applies the CLI default for workflow file selection.
func ResolvePath(path string) string {
	if path == "" {
		return DefaultPath
	}
	return path
}

// RequireReadable verifies that a workflow path exists and is a regular readable file.
func RequireReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: workflow %q does not exist", ErrMissingWorkflowFile, path)
		}
		return fmt.Errorf("workflow %q cannot be inspected: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: workflow %q is a directory", ErrWorkflowPathIsDirectory, path)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("workflow %q is not readable: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("workflow %q could not be closed after validation: %w", path, err)
	}

	return nil
}
