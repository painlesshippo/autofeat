package filelock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "test.lock")
	unlockFirst, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	acquired := make(chan func() error)
	go func() {
		unlockSecond, err := Lock(path)
		if err != nil {
			t.Errorf("second Lock() error = %v", err)
			close(acquired)
			return
		}
		acquired <- unlockSecond
	}()

	select {
	case <-acquired:
		t.Fatal("second Lock() acquired the lock while the first holder still held it")
	case <-time.After(100 * time.Millisecond):
	}

	if err := unlockFirst(); err != nil {
		t.Fatalf("unlock first error = %v", err)
	}
	select {
	case unlockSecond, ok := <-acquired:
		if !ok {
			return
		}
		if err := unlockSecond(); err != nil {
			t.Errorf("unlock second error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second Lock() did not acquire the lock after release")
	}
}
