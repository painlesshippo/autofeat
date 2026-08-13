// Package templates manages reusable repository groups.
package templates

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	appDirectoryName     = ".autofeat"
	templatesFileName    = "templates.json"
	currentSchemaVersion = 1

	LocalRepository  = "local"
	RemoteRepository = "remote"
)

var ErrTemplateNotFound = errors.New("template not found")

// Repository identifies a reusable local repository or remote clone source.
type Repository struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

// Template is an ordered group of repositories.
type Template struct {
	Repositories []Repository `json:"repositories"`
}

// Store is the complete persisted template document.
type Store struct {
	SchemaVersion int                 `json:"schema_version"`
	Templates     map[string]Template `json:"templates"`
}

// Path returns the location of the global templates file.
func Path() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}
	return filepath.Join(homeDir, appDirectoryName, templatesFileName), nil
}

// Load reads all templates. A missing file represents an empty store.
func Load() (Store, error) {
	path, err := Path()
	if err != nil {
		return Store{}, err
	}
	return LoadFromPath(path)
}

// LoadFromPath reads templates from path.
func LoadFromPath(path string) (Store, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyStore(), nil
	}
	if err != nil {
		return Store{}, fmt.Errorf("read templates %q: %w", path, err)
	}

	var versionDocument struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(contents, &versionDocument); err != nil {
		return Store{}, fmt.Errorf("parse templates %q: %w", path, err)
	}
	if versionDocument.SchemaVersion != nil && *versionDocument.SchemaVersion != 0 && *versionDocument.SchemaVersion != currentSchemaVersion {
		return Store{}, fmt.Errorf("parse templates %q: unsupported schema version %d; upgrade autofeat", path, *versionDocument.SchemaVersion)
	}

	var store Store
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store); err != nil {
		return Store{}, fmt.Errorf("parse templates %q: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return Store{}, fmt.Errorf("parse templates %q: %w", path, err)
	}
	if store.Templates == nil {
		store.Templates = make(map[string]Template)
	}
	store.SchemaVersion = currentSchemaVersion
	if err := store.Validate(); err != nil {
		return Store{}, fmt.Errorf("validate templates %q: %w", path, err)
	}
	return store, nil
}

// Save writes all templates to the global templates file.
func Save(store Store) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveToPath(path, store)
}

// SaveToPath validates and writes templates to path.
func SaveToPath(path string, store Store) error {
	if store.SchemaVersion != 0 && store.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("validate templates: unsupported schema version %d; upgrade autofeat", store.SchemaVersion)
	}
	store.SchemaVersion = currentSchemaVersion
	if store.Templates == nil {
		store.Templates = make(map[string]Template)
	}
	if err := store.Validate(); err != nil {
		return fmt.Errorf("validate templates: %w", err)
	}
	contents, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal templates: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create templates directory: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write templates %q: %w", path, err)
	}
	return nil
}

// Put adds or replaces a named template.
func Put(name string, template Template) error {
	store, err := Load()
	if err != nil {
		return err
	}
	store.Templates[name] = template
	return Save(store)
}

// Get returns the named template.
func Get(name string) (Template, error) {
	store, err := Load()
	if err != nil {
		return Template{}, err
	}
	template, ok := store.Templates[name]
	if !ok {
		return Template{}, fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	return template, nil
}

// Names returns template names in lexical order.
func Names() ([]string, error) {
	store, err := Load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(store.Templates))
	for name := range store.Templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Validate verifies template names and repository sources.
func (store Store) Validate() error {
	for name, template := range store.Templates {
		if strings.TrimSpace(name) == "" {
			return errors.New("template name is required")
		}
		if len(template.Repositories) == 0 {
			return fmt.Errorf("template %q must contain at least one repository", name)
		}
		seen := make(map[string]struct{}, len(template.Repositories))
		for index, repository := range template.Repositories {
			if repository.Kind != LocalRepository && repository.Kind != RemoteRepository {
				return fmt.Errorf("template %q repositories[%d]: kind must be %q or %q", name, index, LocalRepository, RemoteRepository)
			}
			if strings.TrimSpace(repository.Source) == "" {
				return fmt.Errorf("template %q repositories[%d]: source is required", name, index)
			}
			if repository.Kind == LocalRepository && !filepath.IsAbs(repository.Source) {
				return fmt.Errorf("template %q repositories[%d]: local source must be an absolute path", name, index)
			}
			key := repository.Kind + "\x00" + repository.Source
			if repository.Kind == LocalRepository {
				key = repository.Kind + "\x00" + filepath.Clean(repository.Source)
			}
			if _, ok := seen[key]; ok {
				return fmt.Errorf("template %q repositories[%d]: duplicate repository source %q", name, index, repository.Source)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func emptyStore() Store {
	return Store{SchemaVersion: currentSchemaVersion, Templates: make(map[string]Template)}
}
