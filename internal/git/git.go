// Package git provides wrappers around the system Git executable.
package git

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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

// ValidateBranchName reports whether branchName is a valid local Git branch
// name. Git is the authority for ref-name syntax, including slash-delimited
// branch hierarchies.
func ValidateBranchName(branchName string) error {
	if _, err := run("check-ref-format", "--branch", branchName); err != nil {
		return fmt.Errorf("validate branch name %q: %w", branchName, err)
	}

	return nil
}

// AddWorktree creates a worktree at destPath on a new branch named branchName.
func AddWorktree(branchName, destPath string) error {
	if _, err := run("worktree", "add", destPath, "-b", branchName); err != nil {
		return fmt.Errorf("add worktree %q: %w", destPath, err)
	}

	return nil
}

// Clone clones url into destPath.
func Clone(url, destPath string) error {
	if _, err := run("clone", url, destPath); err != nil {
		return fmt.Errorf("clone %q to %q: %w", url, destPath, err)
	}

	return nil
}

// CheckoutNewBranch creates and checks out branchName in the repository at destPath.
func CheckoutNewBranch(destPath, branchName string) error {
	if _, err := run("-C", destPath, "checkout", "-b", branchName); err != nil {
		return fmt.Errorf("create branch %q in %q: %w", branchName, destPath, err)
	}

	return nil
}

// CurrentBranch returns the checked-out branch name in the worktree at destPath.
func CurrentBranch(destPath string) (string, error) {
	output, err := run("-C", destPath, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("get current branch in worktree %q: %w", destPath, err)
	}

	branchName := strings.TrimSpace(output)
	if branchName == "" {
		return "", fmt.Errorf("worktree %q is in a detached HEAD state", destPath)
	}

	return branchName, nil
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

// HasUnpushedCommits reports whether branchName has commits that are not on its
// corresponding origin branch in the repository at destPath.
func HasUnpushedCommits(destPath, branchName string) (bool, error) {
	output, err := run("-C", destPath, "log", "origin/"+branchName+".."+branchName)
	if err != nil {
		return false, fmt.Errorf("check unpushed commits for branch %q in %q: %w", branchName, destPath, err)
	}

	return strings.TrimSpace(output) != "", nil
}

// DetectBaseBranch returns master when it exists, otherwise main when it
// exists. An empty result indicates neither branch exists in destPath.
func DetectBaseBranch(destPath string) (string, error) {
	for _, branchName := range []string{"master", "main"} {
		command := exec.Command("git", "-C", destPath, "rev-parse", "--verify", "--quiet", branchName+"^{commit}")
		if err := command.Run(); err == nil {
			return branchName, nil
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				continue
			}
			return "", fmt.Errorf("detect base branch in worktree %q: %w", destPath, err)
		}
	}

	return "", nil
}

// Diff returns the complete worktree diff against baseRef, including
// untracked files. The base reference must resolve to a commit in destPath.
func Diff(destPath, baseRef string) (string, error) {
	if _, err := run("-C", destPath, "rev-parse", "--verify", "--quiet", baseRef+"^{commit}"); err != nil {
		return "", fmt.Errorf("verify base reference %q in worktree %q: %w", baseRef, destPath, err)
	}

	diff, err := run("-C", destPath, "diff", "--binary", baseRef)
	if err != nil {
		return "", fmt.Errorf("diff worktree %q against %q: %w", destPath, baseRef, err)
	}

	untracked, err := run("-C", destPath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", fmt.Errorf("list untracked files in worktree %q: %w", destPath, err)
	}
	for _, relativePath := range strings.Split(strings.TrimSuffix(untracked, "\x00"), "\x00") {
		if relativePath == "" {
			continue
		}

		fileDiff, err := diffUntrackedFile(destPath, relativePath)
		if err != nil {
			return "", err
		}
		diff += fileDiff
	}

	return diff, nil
}

func diffUntrackedFile(destPath, relativePath string) (string, error) {
	command := exec.Command("git", "-C", destPath, "diff", "--no-index", "--binary", "--", "/dev/null", relativePath)
	output, err := command.CombinedOutput()
	if err == nil {
		return string(output), nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return string(output), nil
	}

	return "", fmt.Errorf("diff untracked file %q in worktree %q: %w: %s", filepath.Clean(relativePath), destPath, err, strings.TrimSpace(string(output)))
}

func run(args ...string) (string, error) {
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}
