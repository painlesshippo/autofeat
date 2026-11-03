package vscode

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestWorkspaceIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		workspaceFile string
		wslDistro     string
		want          []string
	}{
		{
			name:          "local path as seen by Linux and by macOS or Windows",
			workspaceFile: "/Work/Feature A.code-workspace",
			want:          []string{"20d35092e9bca2cfe22536248c62894f", "78d610c783485b4b0e38e5d3ab26e8a4"},
		},
		{
			// IDs recorded by VS Code for Remote-WSL windows.
			name:          "WSL remote URI",
			workspaceFile: "/home/alix/.autofeat-workspaces/lafeature/lafeature.code-workspace",
			wslDistro:     "Debian",
			want:          []string{"bac582215942a4637492d69712dc9af9"},
		},
		{
			name:          "WSL remote URI escapes percent signs",
			workspaceFile: "/home/alix/.autofeat-workspaces/feature%2Ftest-af/feature%2Ftest-af.code-workspace",
			wslDistro:     "Debian",
			want:          []string{"7a9de7ba6fad1448284b9b3bc903df03"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := workspaceIDs(test.workspaceFile, test.wslDistro)
			for _, want := range test.want {
				if !slices.Contains(got, want) {
					t.Errorf("workspaceIDs(%q, %q) = %v, missing %s", test.workspaceFile, test.wslDistro, got, want)
				}
			}
			wantCount := 2
			if test.wslDistro != "" {
				wantCount = 3
			}
			if len(got) != wantCount {
				t.Errorf("workspaceIDs() returned %d IDs, want %d", len(got), wantCount)
			}
		})
	}
}

func TestEscapeURIComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text   string
		isPath bool
		want   string
	}{
		{text: "/a-b_c.d~e/F9", isPath: true, want: "/a-b_c.d~e/F9"},
		{text: "/x y#z?%/é", isPath: true, want: "/x%20y%23z%3F%25/%C3%A9"},
		{text: "Ubuntu/24.04", isPath: false, want: "Ubuntu%2F24.04"},
	}
	for _, test := range tests {
		if got := escapeURIComponent(test.text, test.isPath); got != test.want {
			t.Errorf("escapeURIComponent(%q, %t) = %q, want %q", test.text, test.isPath, got, test.want)
		}
	}
}

func TestWorkspaceOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	workspaceFile := "/home/alix/.autofeat-workspaces/lafeature/lafeature.code-workspace"
	writeLock := func(t *testing.T, storageDir, id string, modified time.Time) {
		t.Helper()
		lock := filepath.Join(storageDir, id, "vscode.lock")
		if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lock, []byte(`{"pid":1,"willReleaseAt":0}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(lock, modified, modified); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name          string
		workspaceFile string
		wslDistro     string
		lockID        string
		lockAge       time.Duration
		want          bool
	}{
		{name: "fresh WSL lock", workspaceFile: workspaceFile, wslDistro: "Debian", lockID: "bac582215942a4637492d69712dc9af9", lockAge: 2 * time.Second, want: true},
		{name: "stale WSL lock", workspaceFile: workspaceFile, wslDistro: "Debian", lockID: "bac582215942a4637492d69712dc9af9", lockAge: time.Minute},
		{name: "WSL lock outside WSL", workspaceFile: workspaceFile, lockID: "bac582215942a4637492d69712dc9af9", lockAge: time.Second},
		{name: "fresh native lock", workspaceFile: "/Work/Feature A.code-workspace", lockID: "20d35092e9bca2cfe22536248c62894f", want: true},
		{name: "other workspace lock", workspaceFile: workspaceFile, wslDistro: "Debian", lockID: "20d35092e9bca2cfe22536248c62894f"},
		{name: "no workspace file", lockID: "d41d8cd98f00b204e9800998ecf8427e"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			missingDir := filepath.Join(t.TempDir(), "missing")
			storageDir := t.TempDir()
			writeLock(t, storageDir, test.lockID, now.Add(-test.lockAge))
			if got := workspaceOpen(test.workspaceFile, []string{missingDir, storageDir}, test.wslDistro, now); got != test.want {
				t.Errorf("workspaceOpen() = %t, want %t", got, test.want)
			}
		})
	}
}
