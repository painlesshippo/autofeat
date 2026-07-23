package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveToPathAndLoadFromPath(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), ".autofeat", "config.json")
	want := Config{
		WorkspaceBaseDir: "/tmp/autofeat-workspaces",
		EditorCmd:        "code",
		HeadlessCmd:      "copilot",
	}

	if err := SaveToPath(configPath, want); err != nil {
		t.Fatalf("SaveToPath() error = %v", err)
	}

	got, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if got != want {
		t.Errorf("LoadFromPath() = %+v, want %+v", got, want)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadFromPathRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"editor_cmd":"code"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromPath(configPath); err == nil {
		t.Fatal("LoadFromPath() error = nil, want validation error")
	}
}

func TestLoadFromPathDefaultsMissingHeadlessCommand(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	contents := []byte(`{"workspace_base_dir":"/tmp/autofeat-workspaces","editor_cmd":"code"}`)
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if config.HeadlessCmd != defaultHeadlessCommand {
		t.Errorf("HeadlessCmd = %q, want %q", config.HeadlessCmd, defaultHeadlessCommand)
	}
}
