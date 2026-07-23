package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsInsideWorkTree(t *testing.T) {
	requireGit(t)

	outsideRepo := t.TempDir()
	changeDirectory(t, outsideRepo)

	inside, err := IsInsideWorkTree()
	if err != nil {
		t.Fatalf("IsInsideWorkTree() outside repository error = %v", err)
	}
	if inside {
		t.Error("IsInsideWorkTree() outside repository = true, want false")
	}

	repoPath := createRepository(t)
	changeDirectory(t, repoPath)

	inside, err = IsInsideWorkTree()
	if err != nil {
		t.Fatalf("IsInsideWorkTree() inside repository error = %v", err)
	}
	if !inside {
		t.Error("IsInsideWorkTree() inside repository = false, want true")
	}
}

func TestGetRepoRoot(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	subdirectory := filepath.Join(repoPath, "nested", "directory")
	if err := os.MkdirAll(subdirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	changeDirectory(t, subdirectory)

	root, err := GetRepoRoot()
	if err != nil {
		t.Fatalf("GetRepoRoot() error = %v", err)
	}
	if root != repoPath {
		t.Errorf("GetRepoRoot() = %q, want %q", root, repoPath)
	}
}

func TestValidateBranchName(t *testing.T) {
	requireGit(t)

	for _, branchName := range []string{
		"feature/potato",
		"bug/f321s-aaa",
		"feature/team/potato",
		"flat-feature",
	} {
		if err := ValidateBranchName(branchName); err != nil {
			t.Errorf("ValidateBranchName(%q) error = %v", branchName, err)
		}
	}

	for _, branchName := range []string{
		"",
		"/feature",
		"feature/",
		"feature//potato",
		"feature/.",
		"feature/..",
		"feature/potato.lock",
		"feature potato",
		"feature?potato",
	} {
		if err := ValidateBranchName(branchName); err == nil {
			t.Errorf("ValidateBranchName(%q) error = nil, want error", branchName)
		}
	}
}

func TestDiffIncludesCommittedAndWorkingTreeChanges(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	runGit(t, repoPath, "branch", "-M", "master")
	runGit(t, repoPath, "checkout", "-qb", "feature/review")

	if err := os.WriteFile(filepath.Join(repoPath, "committed.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "committed.txt")
	runGit(t, repoPath, "commit", "-qm", "add committed file")

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(repoPath, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff, err := Diff(repoPath, "master")
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	for _, want := range []string{"committed.txt", "staged.txt", "README.md", "untracked.txt", "+untracked"} {
		if !strings.Contains(diff, want) {
			t.Errorf("Diff() output does not contain %q:\n%s", want, diff)
		}
	}
}

func TestDiffIncludesFullFileContextAndDetectsRenames(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	runGit(t, repoPath, "branch", "-M", "master")
	if err := os.WriteFile(filepath.Join(repoPath, "full.txt"), []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "source.txt"), []byte("copied\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "full.txt", "source.txt")
	runGit(t, repoPath, "commit", "-qm", "add full file")
	runGit(t, repoPath, "checkout", "-qb", "feature/review")

	if err := os.WriteFile(filepath.Join(repoPath, "full.txt"), []byte("first\nchanged\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repoPath, "README.md"), filepath.Join(repoPath, "RENAMED.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "COPIED.txt"), []byte("copied\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "-A")

	diff, err := Diff(repoPath, "master")
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	for _, want := range []string{" first", " third", "similarity index 100%", "rename from README.md", "rename to RENAMED.md", "copy from source.txt", "copy to COPIED.txt"} {
		if !strings.Contains(diff, want) {
			t.Errorf("Diff() output does not contain %q:\n%s", want, diff)
		}
	}
}

func TestDiffWithNoChanges(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	runGit(t, repoPath, "branch", "-M", "master")

	diff, err := Diff(repoPath, "master")
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if diff != "" {
		t.Errorf("Diff() = %q, want empty output", diff)
	}
}

func TestDiffRejectsMissingBase(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	if _, err := Diff(repoPath, "does-not-exist"); err == nil {
		t.Fatal("Diff() error = nil, want missing base error")
	}
}

func TestDiffExcludesChangesAddedOnlyToBaseAfterDivergence(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	runGit(t, repoPath, "branch", "-M", "master")
	runGit(t, repoPath, "checkout", "-qb", "feature/review")
	if err := os.WriteFile(filepath.Join(repoPath, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "feature.txt")
	runGit(t, repoPath, "commit", "-qm", "feature change")

	runGit(t, repoPath, "checkout", "master")
	if err := os.WriteFile(filepath.Join(repoPath, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "base.txt")
	runGit(t, repoPath, "commit", "-qm", "base change")
	runGit(t, repoPath, "checkout", "feature/review")

	diff, err := Diff(repoPath, "master")
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if !strings.Contains(diff, "feature.txt") {
		t.Errorf("Diff() does not contain feature change:\n%s", diff)
	}
	if strings.Contains(diff, "base.txt") {
		t.Errorf("Diff() contains base-only change:\n%s", diff)
	}
}

func TestDetectBaseBranch(t *testing.T) {
	requireGit(t)

	masterRepository := createRepository(t)
	runGit(t, masterRepository, "branch", "-M", "master")
	baseBranch, err := DetectBaseBranch(masterRepository)
	if err != nil {
		t.Fatalf("DetectBaseBranch(master repository) error = %v", err)
	}
	if baseBranch != "master" {
		t.Errorf("DetectBaseBranch(master repository) = %q, want master", baseBranch)
	}

	mainRepository := createRepository(t)
	runGit(t, mainRepository, "branch", "-M", "main")
	baseBranch, err = DetectBaseBranch(mainRepository)
	if err != nil {
		t.Fatalf("DetectBaseBranch(main repository) error = %v", err)
	}
	if baseBranch != "main" {
		t.Errorf("DetectBaseBranch(main repository) = %q, want main", baseBranch)
	}

	noBaseRepository := createRepository(t)
	runGit(t, noBaseRepository, "branch", "-M", "feature/no-base")
	baseBranch, err = DetectBaseBranch(noBaseRepository)
	if err != nil {
		t.Fatalf("DetectBaseBranch(no-base repository) error = %v", err)
	}
	if baseBranch != "" {
		t.Errorf("DetectBaseBranch(no-base repository) = %q, want empty", baseBranch)
	}
}

func TestResolveBaseRefPrefersOriginAndFallsBackWithoutIt(t *testing.T) {
	requireGit(t)

	localPath := createRepository(t)
	runGit(t, localPath, "branch", "-M", "main")
	baseRef, err := ResolveBaseRef(localPath, "main")
	if err != nil {
		t.Fatalf("ResolveBaseRef() local error = %v", err)
	}
	if baseRef != "refs/heads/main" {
		t.Errorf("ResolveBaseRef() local = %q, want refs/heads/main", baseRef)
	}

	remotePath := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, localPath, "init", "--bare", "-q", remotePath)
	runGit(t, localPath, "remote", "add", "origin", remotePath)
	runGit(t, localPath, "push", "-qu", "origin", "main")
	baseRef, err = ResolveBaseRef(localPath, "main")
	if err != nil {
		t.Fatalf("ResolveBaseRef() origin error = %v", err)
	}
	if baseRef != "refs/remotes/origin/main" {
		t.Errorf("ResolveBaseRef() origin = %q, want refs/remotes/origin/main", baseRef)
	}
}

func TestFetchBaseAndAheadBehind(t *testing.T) {
	requireGit(t)

	sourcePath := createRepository(t)
	runGit(t, sourcePath, "branch", "-M", "main")
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, sourcePath, "init", "--bare", "-q", remotePath)
	runGit(t, sourcePath, "remote", "add", "origin", remotePath)
	runGit(t, sourcePath, "push", "-qu", "origin", "main")

	clonePath := filepath.Join(t.TempDir(), "clone")
	if err := Clone(remotePath, clonePath); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if err := CheckoutNewBranch(clonePath, "feature/sync", "origin/main"); err != nil {
		t.Fatalf("CheckoutNewBranch() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(clonePath, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, clonePath, "add", "feature.txt")
	runGit(t, clonePath, "config", "user.email", "test@example.com")
	runGit(t, clonePath, "config", "user.name", "Test User")
	runGit(t, clonePath, "commit", "-qm", "feature change")

	if err := os.WriteFile(filepath.Join(sourcePath, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourcePath, "add", "base.txt")
	runGit(t, sourcePath, "commit", "-qm", "base change")
	runGit(t, sourcePath, "push", "-q", "origin", "main")

	baseRef, err := ResolveBaseRef(clonePath, "main")
	if err != nil {
		t.Fatalf("ResolveBaseRef() error = %v", err)
	}
	ahead, behind, err := AheadBehind(clonePath, baseRef)
	if err != nil {
		t.Fatalf("AheadBehind() before fetch error = %v", err)
	}
	if ahead != 1 || behind != 0 {
		t.Errorf("AheadBehind() before fetch = (%d, %d), want (1, 0)", ahead, behind)
	}

	if err := FetchBase(clonePath, "main"); err != nil {
		t.Fatalf("FetchBase() error = %v", err)
	}
	ahead, behind, err = AheadBehind(clonePath, baseRef)
	if err != nil {
		t.Fatalf("AheadBehind() after fetch error = %v", err)
	}
	if ahead != 1 || behind != 1 {
		t.Errorf("AheadBehind() after fetch = (%d, %d), want (1, 1)", ahead, behind)
	}

	if err := Rebase(clonePath, baseRef); err != nil {
		t.Fatalf("Rebase() error = %v", err)
	}
	ahead, behind, err = AheadBehind(clonePath, baseRef)
	if err != nil {
		t.Fatalf("AheadBehind() after rebase error = %v", err)
	}
	if ahead != 1 || behind != 0 {
		t.Errorf("AheadBehind() after rebase = (%d, %d), want (1, 0)", ahead, behind)
	}
}

func TestRebaseConflictRemainsInProgress(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	runGit(t, repoPath, "branch", "-M", "main")
	runGit(t, repoPath, "checkout", "-qb", "feature/conflict")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "commit", "-qam", "feature change")
	runGit(t, repoPath, "checkout", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "commit", "-qam", "base change")
	runGit(t, repoPath, "checkout", "feature/conflict")
	t.Cleanup(func() {
		_, _ = run("-C", repoPath, "rebase", "--abort")
	})

	if err := Rebase(repoPath, "main"); err == nil {
		t.Fatal("Rebase() error = nil, want conflict")
	}
	rebasing, err := IsRebaseInProgress(repoPath)
	if err != nil {
		t.Fatalf("IsRebaseInProgress() error = %v", err)
	}
	if !rebasing {
		t.Error("IsRebaseInProgress() = false, want true after conflict")
	}
}

func TestAddWorktreeAndDetectUncommittedChanges(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	changeDirectory(t, repoPath)
	worktreePath := filepath.Join(t.TempDir(), "agent-worktree")

	if err := AddWorktree("agent/test", worktreePath, "master"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveWorktree(worktreePath, true)
	})

	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree path does not exist: %v", err)
	}
	branchName, err := CurrentBranch(worktreePath)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if branchName != "agent/test" {
		t.Errorf("CurrentBranch() = %q, want agent/test", branchName)
	}

	dirty, err := HasUncommittedChanges(worktreePath)
	if err != nil {
		t.Fatalf("HasUncommittedChanges() clean worktree error = %v", err)
	}
	if dirty {
		t.Error("HasUncommittedChanges() clean worktree = true, want false")
	}

	if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = HasUncommittedChanges(worktreePath)
	if err != nil {
		t.Fatalf("HasUncommittedChanges() dirty worktree error = %v", err)
	}
	if !dirty {
		t.Error("HasUncommittedChanges() dirty worktree = false, want true")
	}
}

func TestAddWorktreeStartsFromExplicitBase(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	runGit(t, repoPath, "branch", "-M", "master")
	runGit(t, repoPath, "checkout", "-qb", "unrelated")
	if err := os.WriteFile(filepath.Join(repoPath, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "unrelated.txt")
	runGit(t, repoPath, "commit", "-qm", "unrelated change")
	changeDirectory(t, repoPath)

	worktreePath := filepath.Join(t.TempDir(), "agent-worktree")
	if err := AddWorktree("agent/explicit-base", worktreePath, "master"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveWorktree(worktreePath, true)
	})

	if _, err := os.Stat(filepath.Join(worktreePath, "unrelated.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("explicit-base worktree contains unrelated branch file: %v", err)
	}
	mergeBase, err := MergeBase(worktreePath, "master")
	if err != nil {
		t.Fatalf("MergeBase() error = %v", err)
	}
	head := strings.TrimSpace(runGitOutput(t, worktreePath, "rev-parse", "HEAD"))
	if mergeBase != head {
		t.Errorf("MergeBase() = %q, want feature HEAD %q", mergeBase, head)
	}
}

func TestRemoveWorktree(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	changeDirectory(t, repoPath)
	worktreePath := filepath.Join(t.TempDir(), "agent-worktree")
	if err := AddWorktree("agent/remove", worktreePath, "master"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	changeDirectory(t, t.TempDir())
	if err := RemoveWorktree(worktreePath, false); err != nil {
		t.Fatalf("RemoveWorktree() error = %v", err)
	}
	if _, err := os.Stat(worktreePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree still exists or could not be checked: %v", err)
	}
}

func TestCloneCheckoutNewBranchAndDetectUnpushedCommits(t *testing.T) {
	requireGit(t)

	sourcePath := createRepository(t)
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, sourcePath, "init", "--bare", "-q", remotePath)
	runGit(t, sourcePath, "remote", "add", "origin", remotePath)
	runGit(t, sourcePath, "push", "-qu", "origin", "HEAD")
	runGit(t, sourcePath, "checkout", "-qb", "agent/test")
	runGit(t, sourcePath, "push", "-qu", "origin", "agent/test")

	clonePath := filepath.Join(t.TempDir(), "clone")
	if err := Clone(remotePath, clonePath); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if err := CheckoutNewBranch(clonePath, "agent/test", "origin/agent/test"); err != nil {
		t.Fatalf("CheckoutNewBranch() error = %v", err)
	}

	unpushed, err := HasUnpushedCommits(clonePath, "agent/test")
	if err != nil {
		t.Fatalf("HasUnpushedCommits() clean branch error = %v", err)
	}
	if unpushed {
		t.Error("HasUnpushedCommits() clean branch = true, want false")
	}

	if err := os.WriteFile(filepath.Join(clonePath, "new-file.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, clonePath, "add", "new-file.txt")
	runGit(t, clonePath, "config", "user.email", "test@example.com")
	runGit(t, clonePath, "config", "user.name", "Test User")
	runGit(t, clonePath, "commit", "-qm", "new commit")

	unpushed, err = HasUnpushedCommits(clonePath, "agent/test")
	if err != nil {
		t.Fatalf("HasUnpushedCommits() branch with local commit error = %v", err)
	}
	if !unpushed {
		t.Error("HasUnpushedCommits() branch with local commit = false, want true")
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is not available")
	}
}

func createRepository(t *testing.T) string {
	t.Helper()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init", "-q")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", "README.md")
	runGit(t, repoPath, "commit", "-qm", "initial commit")

	return repoPath
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func runGitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

func changeDirectory(t *testing.T, directory string) {
	t.Helper()

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}
