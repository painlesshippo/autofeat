package state

import (
	"path/filepath"
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
				Name:         "repo1",
				OriginalPath: "/tmp/repo1",
				WorktreePath: "/tmp/workspaces/feature1/repo1",
			}},
		},
	}}

	if err := SaveToPath(path, want); err != nil {
		t.Fatalf("SaveToPath() error = %v", err)
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
