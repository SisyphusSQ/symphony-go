package workflow

import (
	"fmt"
	"os"
)

const DefaultPath = "./WORKFLOW.md"

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
		return fmt.Errorf("workflow %q is not readable: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("workflow %q is a directory", path)
	}
	return nil
}
