package state

import (
	"errors"
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
	want := State{DefaultBaseBranch: "develop", RepositoryBaseBranches: map[string]string{"/sources/repo1": "release"}, Sessions: map[string]Session{
		"feature1": {
			CreatedAt:     createdAt,
			FeatureDir:    "/tmp/workspaces/feature1",
			WorkspaceFile: "/tmp/workspaces/feature1/feature1.code-workspace",
			Repos: []Repository{{
				Name:          "repo1",
				OriginalPath:  "https://github.com/example/repo1.git",
				WorktreePath:  "/tmp/workspaces/feature1/repo1",
				IsRemoteClone: true,
				BaseBranch:    "main",
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
	if !strings.Contains(string(contents), `"schema_version": 2`) {
		t.Errorf("state JSON does not contain schema version: %s", contents)
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
	if got.DefaultBaseBranch != want.DefaultBaseBranch {
		t.Errorf("DefaultBaseBranch = %q, want %q", got.DefaultBaseBranch, want.DefaultBaseBranch)
	}
	if got.RepositoryBaseBranches["/sources/repo1"] != "release" {
		t.Errorf("RepositoryBaseBranches = %v, want persisted release branch", got.RepositoryBaseBranches)
	}
	if got.Sessions["feature1"].Repos[0].BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want main", got.Sessions["feature1"].Repos[0].BaseBranch)
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
	if state.DefaultBaseBranch != DefaultBaseBranch {
		t.Errorf("DefaultBaseBranch = %q, want %q", state.DefaultBaseBranch, DefaultBaseBranch)
	}
	if state.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", state.SchemaVersion, currentSchemaVersion)
	}
	if state.RepositoryBaseBranches == nil {
		t.Error("RepositoryBaseBranches = nil, want empty map")
	}
}

func TestLoadFromPathDefaultsMissingGlobalBaseBranch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"sessions":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if state.DefaultBaseBranch != DefaultBaseBranch {
		t.Errorf("DefaultBaseBranch = %q, want %q", state.DefaultBaseBranch, DefaultBaseBranch)
	}
}

func TestLoadFromPathRejectsCorruptState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"sessions":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromPath(path); err == nil {
		t.Fatal("LoadFromPath() error = nil, want parse error")
	}
}

func TestLoadFromPathNormalizesLegacySchemaVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":0,"sessions":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if state.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", state.SchemaVersion, currentSchemaVersion)
	}
	if state.DefaultBaseBranch != DefaultBaseBranch {
		t.Errorf("DefaultBaseBranch = %q, want %q", state.DefaultBaseBranch, DefaultBaseBranch)
	}
}

func TestLoadFromPathMigratesVersionOne(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"default_base_branch":"main","sessions":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if state.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", state.SchemaVersion, currentSchemaVersion)
	}
	if state.RepositoryBaseBranches == nil {
		t.Error("RepositoryBaseBranches = nil, want empty map")
	}
}

func TestLoadFromPathRejectsInvalidSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		version string
		want    string
	}{
		"future":   {version: "3", want: "unsupported schema version 3"},
		"negative": {version: "-1", want: "non-negative integer"},
		"null":     {version: "null", want: "non-negative integer"},
		"string":   {version: `"1"`, want: "must be an integer"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state.json")
			contents := []byte(`{"schema_version":` + test.version + `,"sessions":{}}`)
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadFromPath(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("LoadFromPath() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestLoadFromPathRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"legacy top-level":     `{"sessions":{},"unknown":true}`,
		"versioned repository": `{"schema_version":1,"sessions":{"feature":{"repos":[{"name":"repo","worktree_path":"/tmp/repo","unknown":true}]}}}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadFromPath(path)
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Errorf("LoadFromPath() error = %v, want unknown field error", err)
			}
		})
	}
}

func TestLoadFromPathRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"sessions":{}} {}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromPath(path); err == nil {
		t.Fatal("LoadFromPath() error = nil, want trailing JSON error")
	}
}

func TestLoadFromPathValidatesState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	contents := []byte(`{"sessions":{"feature":{"repos":[{"name":"repo"}]}}}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPath(path)
	if err == nil || !strings.Contains(err.Error(), "worktree_path is required") {
		t.Errorf("LoadFromPath() error = %v, want worktree validation error", err)
	}
}

func TestSaveToPathValidatesState(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		state State
		want  string
	}{
		"unsupported version": {
			state: State{SchemaVersion: currentSchemaVersion + 1},
			want:  "unsupported schema version",
		},
		"blank session name": {
			state: State{Sessions: map[string]Session{" ": {}}},
			want:  "session name is required",
		},
		"missing repository name": {
			state: State{Sessions: map[string]Session{"feature": {Repos: []Repository{{WorktreePath: "/tmp/repo"}}}}},
			want:  "name is required",
		},
		"missing worktree path": {
			state: State{Sessions: map[string]Session{"feature": {Repos: []Repository{{Name: "repo"}}}}},
			want:  "worktree_path is required",
		},
		"blank repository override": {
			state: State{RepositoryBaseBranches: map[string]string{" ": "main"}},
			want:  "empty repository",
		},
		"blank base branch override": {
			state: State{RepositoryBaseBranches: map[string]string{"/repo": " "}},
			want:  "is required",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := SaveToPath(filepath.Join(t.TempDir(), "state.json"), test.state)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("SaveToPath() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestSessionLifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	session := Session{FeatureDir: "/tmp/workspaces/feature1"}
	if err := SaveSession("feature1", session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	got, err := GetSession("feature1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.FeatureDir != session.FeatureDir {
		t.Errorf("FeatureDir = %q, want %q", got.FeatureDir, session.FeatureDir)
	}

	session.FeatureDir = "/tmp/workspaces/feature1-moved"
	if err := UpdateSession("feature1", session); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}
	got, err = GetSession("feature1")
	if err != nil {
		t.Fatalf("GetSession() after update error = %v", err)
	}
	if got.FeatureDir != "/tmp/workspaces/feature1-moved" {
		t.Errorf("FeatureDir after update = %q, want updated value", got.FeatureDir)
	}

	sessions, err := ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("ListSessions() = %v, want one session", sessions)
	}

	if err := DeleteSession("feature1"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := GetSession("feature1"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession() after delete error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionOperationsOnMissingSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := GetSession("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession(missing) error = %v, want ErrSessionNotFound", err)
	}
	if err := UpdateSession("missing", Session{}); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("UpdateSession(missing) error = %v, want ErrSessionNotFound", err)
	}
	if err := DeleteSession("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("DeleteSession(missing) error = %v, want ErrSessionNotFound", err)
	}
}

func TestSaveSessionRejectsEmptyName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := SaveSession("", Session{}); err == nil {
		t.Fatal("SaveSession(\"\") error = nil, want error")
	}
}
