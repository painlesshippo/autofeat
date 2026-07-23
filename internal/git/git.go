// Package git provides wrappers around the system Git executable.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// BaseStatus describes the cached relationship between HEAD and a base branch.
type BaseStatus struct {
	BaseRef string
	Ahead   int
	Behind  int
}

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

// AddWorktree creates a worktree at destPath on a new branch named branchName,
// starting from baseRef.
func AddWorktree(branchName, destPath, baseRef string) error {
	if _, err := run("worktree", "add", destPath, "-b", branchName, baseRef); err != nil {
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

// CheckoutNewBranch creates and checks out branchName from baseRef in the
// repository at destPath.
func CheckoutNewBranch(destPath, branchName, baseRef string) error {
	if _, err := run("-C", destPath, "checkout", "-b", branchName, baseRef); err != nil {
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

// ResolveBaseRef returns the cached ref for baseBranch. Repositories with an
// origin remote use its remote-tracking branch; repositories without origin
// use the local branch.
func ResolveBaseRef(destPath, baseBranch string) (string, error) {
	hasOrigin, err := HasOrigin(destPath)
	if err != nil {
		return "", err
	}

	baseRef := "refs/heads/" + baseBranch
	if hasOrigin {
		baseRef = "refs/remotes/origin/" + baseBranch
	}
	if _, err := run("-C", destPath, "rev-parse", "--verify", "--quiet", baseRef+"^{commit}"); err != nil {
		return "", fmt.Errorf("resolve base branch %q in repository %q: %w", baseBranch, destPath, err)
	}

	return baseRef, nil
}

// HasOrigin reports whether the repository at destPath has an origin remote.
func HasOrigin(destPath string) (bool, error) {
	command := exec.Command("git", "-C", destPath, "remote", "get-url", "origin")
	if err := command.Run(); err == nil {
		return true, nil
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("detect origin remote in repository %q: %w", destPath, err)
	}
}

// FetchBase fetches baseBranch from origin into its remote-tracking ref.
func FetchBase(destPath, baseBranch string) error {
	refspec := "+refs/heads/" + baseBranch + ":refs/remotes/origin/" + baseBranch
	if _, err := run("-C", destPath, "fetch", "origin", refspec); err != nil {
		return fmt.Errorf("fetch base branch %q in repository %q: %w", baseBranch, destPath, err)
	}

	return nil
}

// AheadBehind returns the number of commits HEAD is ahead of and behind baseRef.
func AheadBehind(destPath, baseRef string) (ahead, behind int, err error) {
	output, err := run("-C", destPath, "rev-list", "--left-right", "--count", baseRef+"...HEAD")
	if err != nil {
		return 0, 0, fmt.Errorf("calculate drift from %q in repository %q: %w", baseRef, destPath, err)
	}

	fields := strings.Fields(output)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("calculate drift from %q in repository %q: unexpected output %q", baseRef, destPath, strings.TrimSpace(output))
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind count %q: %w", fields[0], err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead count %q: %w", fields[1], err)
	}

	return ahead, behind, nil
}

// CachedBaseStatus resolves baseBranch without fetching and reports how HEAD
// relates to it.
func CachedBaseStatus(destPath, baseBranch string) (BaseStatus, error) {
	baseRef, err := ResolveBaseRef(destPath, baseBranch)
	if err != nil {
		return BaseStatus{}, err
	}
	ahead, behind, err := AheadBehind(destPath, baseRef)
	if err != nil {
		return BaseStatus{}, err
	}

	return BaseStatus{BaseRef: baseRef, Ahead: ahead, Behind: behind}, nil
}

// Rebase rebases the current branch in destPath onto baseRef.
func Rebase(destPath, baseRef string) error {
	if _, err := run("-C", destPath, "rebase", baseRef); err != nil {
		return fmt.Errorf("rebase repository %q onto %q: %w", destPath, baseRef, err)
	}

	return nil
}

// IsRebaseInProgress reports whether destPath has an apply or merge rebase in progress.
func IsRebaseInProgress(destPath string) (bool, error) {
	for _, stateDirectory := range []string{"rebase-merge", "rebase-apply"} {
		path, err := run("-C", destPath, "rev-parse", "--git-path", stateDirectory)
		if err != nil {
			return false, fmt.Errorf("locate Git rebase state in repository %q: %w", destPath, err)
		}
		statePath := strings.TrimSpace(path)
		if !filepath.IsAbs(statePath) {
			statePath = filepath.Join(destPath, statePath)
		}
		if _, err := os.Stat(statePath); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect Git rebase state in repository %q: %w", destPath, err)
		}
	}

	return false, nil
}

// MergeBase returns the common ancestor of HEAD and baseRef in destPath.
func MergeBase(destPath, baseRef string) (string, error) {
	output, err := run("-C", destPath, "merge-base", "HEAD", baseRef)
	if err != nil {
		return "", fmt.Errorf("find merge base of HEAD and %q in worktree %q: %w", baseRef, destPath, err)
	}

	return strings.TrimSpace(output), nil
}

// Diff returns the complete worktree diff against baseRef, including
// untracked files. The base reference must resolve to a commit in destPath.
func Diff(destPath, baseRef string) (string, error) {
	if _, err := run("-C", destPath, "rev-parse", "--verify", "--quiet", baseRef+"^{commit}"); err != nil {
		return "", fmt.Errorf("verify base reference %q in worktree %q: %w", baseRef, destPath, err)
	}
	comparisonRef, err := MergeBase(destPath, baseRef)
	if err != nil {
		return "", err
	}

	diff, err := run("-C", destPath, "diff", "--binary", "--find-renames", "--find-copies-harder", "--unified=2147483647", comparisonRef)
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
	command := exec.Command("git", "-C", destPath, "diff", "--no-index", "--binary", "--unified=2147483647", "--", "/dev/null", relativePath)
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
