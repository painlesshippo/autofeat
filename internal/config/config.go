package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	appDirectoryName       = ".autofeat"
	configFileName         = "config.json"
	defaultEditorCommand   = "code"
	defaultHeadlessCommand = "copilot"
)

// Config is the global autofeat configuration stored in ~/.autofeat/config.json.
type Config struct {
	WorkspaceBaseDir string   `json:"workspace_base_dir"`
	EditorCmd        string   `json:"editor_cmd"`
	HeadlessCmd      string   `json:"headless_cmd"`
	PostAddCommands  []string `json:"post_add_commands"`
}

// Default returns the default configuration for the current user.
func Default() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("get user home directory: %w", err)
	}

	return Config{
		WorkspaceBaseDir: filepath.Join(homeDir, ".autofeat-workspaces"),
		EditorCmd:        defaultEditorCommand,
		HeadlessCmd:      defaultHeadlessCommand,
		PostAddCommands:  []string{},
	}, nil
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

	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if strings.TrimSpace(config.HeadlessCmd) == "" {
		config.HeadlessCmd = defaultHeadlessCommand
	}
	if config.PostAddCommands == nil {
		config.PostAddCommands = []string{}
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}

	return config, nil
}

// SaveToPath validates and writes config as indented JSON to path.
func SaveToPath(path string, config Config) error {
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
	for index, command := range config.PostAddCommands {
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("post_add_commands[%d] is required", index)
		}
	}

	return nil
}
