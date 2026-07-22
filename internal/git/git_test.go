package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestAddWorktreeAndDetectUncommittedChanges(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	changeDirectory(t, repoPath)
	worktreePath := filepath.Join(t.TempDir(), "agent-worktree")

	if err := AddWorktree("agent/test", worktreePath); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveWorktree(worktreePath, true)
	})

	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree path does not exist: %v", err)
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

func TestRemoveWorktree(t *testing.T) {
	requireGit(t)

	repoPath := createRepository(t)
	changeDirectory(t, repoPath)
	worktreePath := filepath.Join(t.TempDir(), "agent-worktree")
	if err := AddWorktree("agent/remove", worktreePath); err != nil {
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
	if err := CheckoutNewBranch(clonePath, "agent/test"); err != nil {
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
