package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveToPathAndLoadFromPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".autofeat", "state.json")
	createdAt := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	want := State{Sessions: map[string]Session{
		"feature1": {
			CreatedAt:     createdAt,
			FeatureDir:    "/tmp/workspaces/feature1",
			WorkspaceFile: "/tmp/workspaces/feature1/feature1.code-workspace",
			Repos: []Repository{{
				Name:          "repo1",
				OriginalPath:  "https://github.com/example/repo1.git",
				WorktreePath:  "/tmp/workspaces/feature1/repo1",
				IsRemoteClone: true,
			}},
		},
	}}

	if err := SaveToPath(path, want); err != nil {
		t.Fatalf("SaveToPath() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(contents), `"is_remote_clone": true`) {
		t.Errorf("state JSON does not contain is_remote_clone: %s", contents)
	}

	got, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if !got.Sessions["feature1"].CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %v, want %v", got.Sessions["feature1"].CreatedAt, createdAt)
	}
	if got.Sessions["feature1"].WorkspaceFile != want.Sessions["feature1"].WorkspaceFile {
		t.Errorf("WorkspaceFile = %q, want %q", got.Sessions["feature1"].WorkspaceFile, want.Sessions["feature1"].WorkspaceFile)
	}
	if !got.Sessions["feature1"].Repos[0].IsRemoteClone {
		t.Error("IsRemoteClone = false, want true")
	}
}

func TestLoadFromPathReturnsEmptyStateWhenMissing(t *testing.T) {
	t.Parallel()

	state, err := LoadFromPath(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if len(state.Sessions) != 0 {
		t.Errorf("sessions = %v, want empty map", state.Sessions)
	}
}
