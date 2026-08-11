// Package state manages autofeat's global JSON session state.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	appDirectoryName     = ".autofeat"
	stateFileName        = "state.json"
	currentSchemaVersion = 1
	// DefaultBaseBranch is used when no global or repository-specific base is set.
	DefaultBaseBranch = "master"
)

// ErrSessionNotFound indicates that no session exists with the requested name.
var ErrSessionNotFound = errors.New("session not found")

// Repository describes a source repository attached to a feature session.
type Repository struct {
	Name          string `json:"name"`
	OriginalPath  string `json:"original_path"`
	WorktreePath  string `json:"worktree_path"`
	IsRemoteClone bool   `json:"is_remote_clone"`
	BaseBranch    string `json:"base_branch"`
}

// Session describes a feature session and its attached repository worktrees.
type Session struct {
	CreatedAt     time.Time    `json:"created_at"`
	FeatureDir    string       `json:"feature_dir"`
	WorkspaceFile string       `json:"workspace_file"`
	Repos         []Repository `json:"repos"`
}

// State is the complete persisted autofeat state.
type State struct {
	SchemaVersion     int                `json:"schema_version"`
	DefaultBaseBranch string             `json:"default_base_branch"`
	Sessions          map[string]Session `json:"sessions"`
}

// Path returns the location of the global state file.
func Path() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}

	return filepath.Join(homeDir, appDirectoryName, stateFileName), nil
}

// SaveSession adds or replaces a session in the global state file.
func SaveSession(name string, session Session) error {
	return saveSession(name, session, false)
}

// GetSession returns the session named name.
func GetSession(name string) (Session, error) {
	state, err := Load()
	if err != nil {
		return Session{}, err
	}

	session, ok := state.Sessions[name]
	if !ok {
		return Session{}, fmt.Errorf("%w: %s", ErrSessionNotFound, name)
	}

	return session, nil
}

// ListSessions returns all persisted sessions, keyed by feature name.
func ListSessions() (map[string]Session, error) {
	state, err := Load()
	if err != nil {
		return nil, err
	}

	return state.Sessions, nil
}

// UpdateSession replaces an existing session in the global state file.
func UpdateSession(name string, session Session) error {
	return saveSession(name, session, true)
}

// DeleteSession removes the session named name from the global state file.
func DeleteSession(name string) error {
	state, err := Load()
	if err != nil {
		return err
	}
	if _, ok := state.Sessions[name]; !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, name)
	}

	delete(state.Sessions, name)
	return Save(state)
}

// Load reads the global state. A missing state file represents an empty state.
func Load() (State, error) {
	path, err := Path()
	if err != nil {
		return State{}, err
	}

	return LoadFromPath(path)
}

// Save writes the complete state to the global state file.
func Save(state State) error {
	path, err := Path()
	if err != nil {
		return err
	}

	return SaveToPath(path, state)
}

// LoadFromPath reads state from path. A missing file returns an empty state.
func LoadFromPath(path string) (State, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{SchemaVersion: currentSchemaVersion, DefaultBaseBranch: DefaultBaseBranch, Sessions: make(map[string]Session)}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state %q: %w", path, err)
	}

	schemaVersion, err := schemaVersion(contents)
	if err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	if schemaVersion != 0 && schemaVersion != currentSchemaVersion {
		return State{}, fmt.Errorf("parse state %q: unsupported schema version %d; upgrade autofeat", path, schemaVersion)
	}

	var state State
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	if state.Sessions == nil {
		state.Sessions = make(map[string]Session)
	}
	if state.DefaultBaseBranch == "" {
		state.DefaultBaseBranch = DefaultBaseBranch
	}
	state.SchemaVersion = currentSchemaVersion
	if err := state.Validate(); err != nil {
		return State{}, fmt.Errorf("validate state %q: %w", path, err)
	}

	return state, nil
}

// SaveToPath writes state as indented JSON to path.
func SaveToPath(path string, state State) error {
	if state.SchemaVersion != 0 && state.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("validate state: unsupported schema version %d; upgrade autofeat", state.SchemaVersion)
	}
	state.SchemaVersion = currentSchemaVersion
	if state.Sessions == nil {
		state.Sessions = make(map[string]Session)
	}
	if state.DefaultBaseBranch == "" {
		state.DefaultBaseBranch = DefaultBaseBranch
	}
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate state: %w", err)
	}

	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	contents = append(contents, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write state %q: %w", path, err)
	}

	return nil
}

// Validate verifies that state contains values required by state consumers.
func (state State) Validate() error {
	if strings.TrimSpace(state.DefaultBaseBranch) == "" {
		return errors.New("default_base_branch is required")
	}
	for name, session := range state.Sessions {
		if strings.TrimSpace(name) == "" {
			return errors.New("session name is required")
		}
		for index, repository := range session.Repos {
			if strings.TrimSpace(repository.Name) == "" {
				return fmt.Errorf("session %q repos[%d]: name is required", name, index)
			}
			if strings.TrimSpace(repository.WorktreePath) == "" {
				return fmt.Errorf("session %q repos[%d]: worktree_path is required", name, index)
			}
		}
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

func saveSession(name string, session Session, mustExist bool) error {
	if name == "" {
		return errors.New("session name is required")
	}

	state, err := Load()
	if err != nil {
		return err
	}
	if mustExist {
		if _, ok := state.Sessions[name]; !ok {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, name)
		}
	}

	state.Sessions[name] = session
	return Save(state)
}
