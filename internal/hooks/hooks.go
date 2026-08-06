package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Event string

const PostAdd Event = "post-add"

type Definition struct {
	When    Event    `json:"when"`
	IfFiles []string `json:"if_files,omitempty"`
	Run     string   `json:"run"`
}

func (definition Definition) Validate() error {
	if definition.When != PostAdd {
		return fmt.Errorf("unsupported hook event %q", definition.When)
	}
	if strings.TrimSpace(definition.Run) == "" {
		return fmt.Errorf("hook command is required")
	}
	for index, path := range definition.IfFiles {
		if !filepath.IsLocal(path) || filepath.Clean(path) == "." {
			return fmt.Errorf("if_files[%d] must be a relative file path", index)
		}
	}

	return nil
}

func Run(definitions []Definition, event Event, worktreePath string) error {
	for _, definition := range definitions {
		if definition.When != event {
			continue
		}
		matches, err := matchesFiles(definition.IfFiles, worktreePath)
		if err != nil {
			return fmt.Errorf("match %s hook files in %q: %w", event, worktreePath, err)
		}
		if !matches {
			continue
		}

		fmt.Printf("Running %s hook in %s: %s\n", event, worktreePath, definition.Run)
		command := exec.Command("sh", "-c", definition.Run)
		command.Dir = worktreePath
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("run %s hook %q in %q: %w", event, definition.Run, worktreePath, err)
		}
	}

	return nil
}

func matchesFiles(paths []string, worktreePath string) (bool, error) {
	if len(paths) == 0 {
		return true, nil
	}

	for _, path := range paths {
		info, err := os.Stat(filepath.Join(worktreePath, path))
		if err == nil {
			if info.Mode().IsRegular() {
				return true, nil
			}
			continue
		}
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("stat %q: %w", path, err)
		}
	}

	return false, nil
}
