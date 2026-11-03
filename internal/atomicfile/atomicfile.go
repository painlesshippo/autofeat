// Package atomicfile replaces files so concurrent readers never observe a
// partially written file.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFile writes contents to a temporary file in the directory of path and
// renames it over path. The resulting file has 0600 permissions.
func WriteFile(path string, contents []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := file.Write(contents)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return errors.Join(err, os.Remove(file.Name()))
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return errors.Join(err, os.Remove(file.Name()))
	}
	return nil
}
