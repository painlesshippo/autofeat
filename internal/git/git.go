// Package git provides wrappers around the system Git executable.
package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// IsInsideWorkTree reports whether the current directory is inside a Git worktree.
func IsInsideWorkTree() (bool, error) {
	output, err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("check Git worktree: %w", err)
	}

	return strings.TrimSpace(string(output)) == "true", nil
}

// GetRepoRoot returns the absolute path to the current repository's root.
func GetRepoRoot() (string, error) {
	output, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("get repository root: %w", err)
	}

	return strings.TrimSpace(output), nil
}

// AddWorktree creates a worktree at destPath on a new branch named branchName.
func AddWorktree(branchName, destPath string) error {
	if _, err := run("worktree", "add", destPath, "-b", branchName); err != nil {
		return fmt.Errorf("add worktree %q: %w", destPath, err)
	}

	return nil
}

// RemoveWorktree removes the worktree at destPath. When force is true, Git is
// instructed to remove the worktree even if it contains modified files.
func RemoveWorktree(destPath string, force bool) error {
	args := []string{"-C", destPath, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, destPath)

	if _, err := run(args...); err != nil {
		return fmt.Errorf("remove worktree %q: %w", destPath, err)
	}

	return nil
}

// HasUncommittedChanges reports whether the worktree at destPath is dirty.
func HasUncommittedChanges(destPath string) (bool, error) {
	output, err := run("-C", destPath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("check worktree %q status: %w", destPath, err)
	}

	return len(output) > 0, nil
}

func run(args ...string) (string, error) {
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}
