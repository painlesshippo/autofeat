package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/painlesshippo/autofeat/internal/hooks"
)

func TestSaveToPathAndLoadFromPath(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), ".autofeat", "config.json")
	want := Config{
		SchemaVersion:    currentSchemaVersion,
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
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(contents), `"schema_version": 1`) {
		t.Errorf("config JSON does not contain schema version: %s", contents)
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

func TestLoadFromPathNormalizationAndDefaults(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		contents string
		want     Config
	}{
		"defaults missing headless command and hooks": {
			contents: `{"workspace_base_dir":"/tmp/autofeat-workspaces","editor_cmd":"code"}`,
			want: Config{
				SchemaVersion:    currentSchemaVersion,
				WorkspaceBaseDir: "/tmp/autofeat-workspaces",
				EditorCmd:        "code",
				HeadlessCmd:      defaultHeadlessCommand,
				Hooks:            defaultHooks(),
			},
		},
		"defaults missing hooks when headless command is set": {
			contents: `{"workspace_base_dir":"/tmp/autofeat-workspaces","editor_cmd":"code","headless_cmd":"copilot"}`,
			want: Config{
				SchemaVersion:    currentSchemaVersion,
				WorkspaceBaseDir: "/tmp/autofeat-workspaces",
				EditorCmd:        "code",
				HeadlessCmd:      "copilot",
				Hooks:            defaultHooks(),
			},
		},
		"preserves explicit empty hooks slice": {
			contents: `{"workspace_base_dir":"/tmp/autofeat-workspaces","editor_cmd":"code","headless_cmd":"copilot","hooks":[]}`,
			want: Config{
				SchemaVersion:    currentSchemaVersion,
				WorkspaceBaseDir: "/tmp/autofeat-workspaces",
				EditorCmd:        "code",
				HeadlessCmd:      "copilot",
				Hooks:            []hooks.Definition{},
			},
		},
		"normalizes legacy schema version zero": {
			contents: `{"schema_version":0,"workspace_base_dir":"/tmp/workspaces","editor_cmd":"code"}`,
			want: Config{
				SchemaVersion:    currentSchemaVersion,
				WorkspaceBaseDir: "/tmp/workspaces",
				EditorCmd:        "code",
				HeadlessCmd:      defaultHeadlessCommand,
				Hooks:            defaultHooks(),
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := LoadFromPath(configPath)
			if err != nil {
				t.Fatalf("LoadFromPath() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("LoadFromPath() = %#v, want %#v", got, test.want)
			}
		})
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
		SchemaVersion:    currentSchemaVersion,
		WorkspaceBaseDir: filepath.Join(home, ".autofeat-workspaces"),
		EditorCmd:        defaultEditorCommand,
		HeadlessCmd:      defaultHeadlessCommand,
		Hooks:            defaultHooks(),
	}
	if !reflect.DeepEqual(config, want) {
		t.Errorf("Load() first use = %+v, want %+v", config, want)
	}
	configFile := filepath.Join(home, ".autofeat", "config.json")
	if _, err := os.Stat(configFile); err != nil {
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

	if err := os.WriteFile(configFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("Load() with corrupt config error = nil, want error")
	}

	blockingFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveToPath(filepath.Join(blockingFile, "config.json"), config); err == nil {
		t.Error("SaveToPath(under regular file) error = nil, want directory creation error")
	}
}

func TestLoadFromPathRejectsInvalidSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		version string
		want    string
	}{
		"future":   {version: "2", want: "unsupported schema version 2"},
		"negative": {version: "-1", want: "non-negative integer"},
		"null":     {version: "null", want: "non-negative integer"},
		"string":   {version: `"1"`, want: "must be an integer"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "config.json")
			contents := []byte(`{"schema_version":` + test.version + `,"workspace_base_dir":"/tmp/workspaces","editor_cmd":"code"}`)
			if err := os.WriteFile(configPath, contents, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadFromPath(configPath)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("LoadFromPath() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestLoadFromPathRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"legacy top-level": `{"workspace_base_dir":"/tmp/workspaces","editor_cmd":"code","unknown":true}`,
		"versioned hook":   `{"schema_version":1,"workspace_base_dir":"/tmp/workspaces","editor_cmd":"code","hooks":[{"when":"post-add","run":"true","unknown":true}]}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadFromPath(configPath)
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Errorf("LoadFromPath() error = %v, want unknown field error", err)
			}
		})
	}
}

func TestLoadFromPathRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	contents := []byte(`{"workspace_base_dir":"/tmp/workspaces","editor_cmd":"code"} {}`)
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromPath(configPath); err == nil {
		t.Fatal("LoadFromPath() error = nil, want trailing JSON error")
	}
}

func TestSaveToPathRejectsUnsupportedSchemaVersion(t *testing.T) {
	t.Parallel()

	config := Config{
		SchemaVersion:    currentSchemaVersion + 1,
		WorkspaceBaseDir: "/tmp/workspaces",
		EditorCmd:        "code",
		HeadlessCmd:      "copilot",
	}
	err := SaveToPath(filepath.Join(t.TempDir(), "config.json"), config)
	if err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
		t.Errorf("SaveToPath() error = %v, want unsupported schema version error", err)
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
