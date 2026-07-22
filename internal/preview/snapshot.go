package preview

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteSnapshot atomically replaces path with contents using private file permissions.
func WriteSnapshot(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create preview directory: %w", err)
	}

	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".preview-*.html")
	if err != nil {
		return fmt.Errorf("create temporary preview file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)

	if err := temporaryFile.Chmod(0o600); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("set temporary preview permissions: %w", err)
	}
	if _, err := temporaryFile.Write(contents); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("write temporary preview file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close temporary preview file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace preview file: %w", err)
	}

	return nil
}
