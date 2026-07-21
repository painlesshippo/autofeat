// Package workspace generates VS Code workspace files for feature sessions.
package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Folder is a folder entry in a VS Code workspace file.
type Folder struct {
	Path string `json:"path"`
}

// Workspace is the strict JSON representation of a VS Code workspace file.
type Workspace struct {
	Folders []Folder `json:"folders"`
}

// New returns a workspace with relative folder entries for repoNames.
func New(repoNames []string) Workspace {
	folders := make([]Folder, 0, len(repoNames))
	for _, name := range repoNames {
		folders = append(folders, Folder{Path: "./" + name})
	}

	return Workspace{Folders: folders}
}

// Write creates a VS Code workspace file at path for the provided repository names.
func Write(path string, repoNames []string) error {
	return WriteWorkspace(path, New(repoNames))
}

// WriteWorkspace writes a Workspace as indented JSON at path.
func WriteWorkspace(path string, workspace Workspace) error {
	contents, err := json.MarshalIndent(workspace, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace: %w", err)
	}
	contents = append(contents, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create workspace directory: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write workspace %q: %w", path, err)
	}

	return nil
}
