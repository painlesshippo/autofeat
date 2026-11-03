// Package filelock provides exclusive file locks that serialize work across
// concurrent autofeat processes.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Lock blocks until it holds an exclusive lock on the file at path, creating
// the file and its directory when needed. Call the returned function to
// release the lock.
func Lock(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", path, err)
	}
	if err := lock(file); err != nil {
		return nil, errors.Join(fmt.Errorf("lock %q: %w", path, err), file.Close())
	}

	return func() error {
		if err := errors.Join(unlock(file), file.Close()); err != nil {
			return fmt.Errorf("unlock %q: %w", path, err)
		}
		return nil
	}, nil
}
