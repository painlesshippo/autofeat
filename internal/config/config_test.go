package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/painlesshippo/autofeat/internal/hooks"
)

func TestSaveToPathAndLoadFromPath(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), ".autofeat", "config.json")
	want := Config{
		WorkspaceBaseDir: "/tmp/autofeat-workspaces",
		EditorCmd:        "code",
		HeadlessCmd:      "copilot",
		Hooks: []hooks.Definition{{
			When:    hooks.PostAdd,
			IfFiles: []string{"mise.toml", ".mise.toml"},
			Run:     "mise trust && mise install",
		}},
	}

	if err := SaveToPath(configPath, want); err != nil {
		t.Fatalf("SaveToPath() error = %v", err)
	}

	got, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
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

func TestLoadFromPathDefaultsMissingHooks(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	contents := []byte(`{"workspace_base_dir":"/tmp/autofeat-workspaces","editor_cmd":"code","headless_cmd":"copilot"}`)
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if !reflect.DeepEqual(config.Hooks, defaultHooks()) {
		t.Errorf("Hooks = %#v, want defaults %#v", config.Hooks, defaultHooks())
	}
}

func TestLoadFromPathPreservesEmptyHooks(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	contents := []byte(`{"workspace_base_dir":"/tmp/autofeat-workspaces","editor_cmd":"code","headless_cmd":"copilot","hooks":[]}`)
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if config.Hooks == nil || len(config.Hooks) != 0 {
		t.Errorf("Hooks = %#v, want empty non-nil slice", config.Hooks)
	}
}

func TestLoadFromPathRejectsCorruptConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"editor_cmd":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromPath(configPath); err == nil {
		t.Fatal("LoadFromPath() error = nil, want parse error")
	}
}

func TestLoadCreatesDefaultConfigOnFirstUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() first use error = %v", err)
	}
	want := Config{
		WorkspaceBaseDir: filepath.Join(home, ".autofeat-workspaces"),
		EditorCmd:        defaultEditorCommand,
		HeadlessCmd:      defaultHeadlessCommand,
		Hooks:            defaultHooks(),
	}
	if !reflect.DeepEqual(config, want) {
		t.Errorf("Load() first use = %+v, want %+v", config, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".autofeat", "config.json")); err != nil {
		t.Errorf("default config file was not created: %v", err)
	}

	config.EditorCmd = "custom-editor"
	if err := Save(config); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() second use error = %v", err)
	}
	if !reflect.DeepEqual(got, config) {
		t.Errorf("Load() second use = %+v, want persisted %+v", got, config)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{WorkspaceBaseDir: "/tmp/workspaces", EditorCmd: "code", HeadlessCmd: "copilot"}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	tests := map[string]Config{
		"missing workspace base dir": {EditorCmd: "code", HeadlessCmd: "copilot"},
		"missing editor cmd":         {WorkspaceBaseDir: "/tmp/workspaces", HeadlessCmd: "copilot"},
		"missing headless cmd":       {WorkspaceBaseDir: "/tmp/workspaces", EditorCmd: "code"},
		"blank values":               {WorkspaceBaseDir: " ", EditorCmd: " ", HeadlessCmd: " "},
		"invalid hook": {WorkspaceBaseDir: "/tmp/workspaces", EditorCmd: "code", HeadlessCmd: "copilot", Hooks: []hooks.Definition{{
			When: hooks.PostAdd,
			Run:  " ",
		}}},
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := config.Validate(); err == nil {
				t.Errorf("Validate(%+v) error = nil, want error", config)
			}
		})
	}
}
