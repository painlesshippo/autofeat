package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/painlesshippo/autofeat/internal/hooks"
)

const (
	appDirectoryName       = ".autofeat"
	configFileName         = "config.json"
	defaultEditorCommand   = "code"
	defaultHeadlessCommand = "copilot"
	currentSchemaVersion   = 1
)

// Config is the global autofeat configuration stored in ~/.autofeat/config.json.
type Config struct {
	SchemaVersion    int                `json:"schema_version"`
	WorkspaceBaseDir string             `json:"workspace_base_dir"`
	EditorCmd        string             `json:"editor_cmd"`
	HeadlessCmd      string             `json:"headless_cmd"`
	Hooks            []hooks.Definition `json:"hooks"`
}

// Default returns the default configuration for the current user.
func Default() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("get user home directory: %w", err)
	}

	return Config{
		SchemaVersion:    currentSchemaVersion,
		WorkspaceBaseDir: filepath.Join(homeDir, ".autofeat-workspaces"),
		EditorCmd:        defaultEditorCommand,
		HeadlessCmd:      defaultHeadlessCommand,
		Hooks:            defaultHooks(),
	}, nil
}

func defaultHooks() []hooks.Definition {
	return []hooks.Definition{{
		When:    hooks.PostAdd,
		IfFiles: []string{"mise.toml", ".mise.toml"},
		Run:     "mise trust && mise install",
	}}
}

// Path returns the location of the global configuration file.
func Path() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}

	return filepath.Join(homeDir, appDirectoryName, configFileName), nil
}

// Load reads the global configuration. If it does not exist, Load creates it
// with the default values before returning it.
func Load() (Config, error) {
	configPath, err := Path()
	if err != nil {
		return Config{}, err
	}

	config, err := LoadFromPath(configPath)
	if err == nil {
		return config, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	config, err = Default()
	if err != nil {
		return Config{}, err
	}
	if err := SaveToPath(configPath, config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// Save writes config to the global configuration file.
func Save(config Config) error {
	configPath, err := Path()
	if err != nil {
		return err
	}

	return SaveToPath(configPath, config)
}

// LoadFromPath reads and validates a configuration file at path.
func LoadFromPath(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	schemaVersion, err := schemaVersion(contents)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if schemaVersion != 0 && schemaVersion != currentSchemaVersion {
		return Config{}, fmt.Errorf("parse config %q: unsupported schema version %d; upgrade autofeat", path, schemaVersion)
	}

	var config Config
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if strings.TrimSpace(config.HeadlessCmd) == "" {
		config.HeadlessCmd = defaultHeadlessCommand
	}
	if config.Hooks == nil {
		config.Hooks = defaultHooks()
	}
	config.SchemaVersion = currentSchemaVersion
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}

	return config, nil
}

// SaveToPath validates and writes config as indented JSON to path.
func SaveToPath(path string, config Config) error {
	if config.SchemaVersion != 0 && config.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("validate config: unsupported schema version %d; upgrade autofeat", config.SchemaVersion)
	}
	config.SchemaVersion = currentSchemaVersion
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	contents, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	contents = append(contents, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}

	return nil
}

func schemaVersion(contents []byte) (int, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return 0, err
	}

	rawVersion, ok := document["schema_version"]
	if !ok {
		return 0, nil
	}

	var version *int
	if err := json.Unmarshal(rawVersion, &version); err != nil {
		return 0, fmt.Errorf("schema_version must be an integer: %w", err)
	}
	if version == nil || *version < 0 {
		return 0, errors.New("schema_version must be a non-negative integer")
	}

	return *version, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}

	return nil
}

// Validate verifies that the configuration contains the required values.
func (config Config) Validate() error {
	if strings.TrimSpace(config.WorkspaceBaseDir) == "" {
		return errors.New("workspace_base_dir is required")
	}
	if strings.TrimSpace(config.EditorCmd) == "" {
		return errors.New("editor_cmd is required")
	}
	if strings.TrimSpace(config.HeadlessCmd) == "" {
		return errors.New("headless_cmd is required")
	}
	for index, hook := range config.Hooks {
		if err := hook.Validate(); err != nil {
			return fmt.Errorf("hooks[%d]: %w", index, err)
		}
	}

	return nil
}
