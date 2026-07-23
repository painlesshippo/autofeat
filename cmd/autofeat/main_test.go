package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/state"
)

func TestVersionCommand(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatalf("run(version) error = %v", err)
	}

	if err := run([]string{"version", "extra"}); err == nil {
		t.Error("run(version, extra) error = nil, want usage error")
	}
}

func TestReviewBase(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "stored base", want: ""},
		{name: "custom branch", args: []string{"--base", "develop"}, want: "develop"},
		{name: "hierarchical branch", args: []string{"--base", "release/next"}, want: "release/next"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := reviewBase(test.args)
			if err != nil {
				t.Fatalf("reviewBase(%v) error = %v", test.args, err)
			}
			if got != test.want {
				t.Errorf("reviewBase(%v) = %q, want %q", test.args, got, test.want)
			}
		})
	}

	for _, args := range [][]string{
		{"--base"},
		{"--other", "develop"},
		{"--base", ""},
		{"--base", "bad branch"},
		{"--base", "develop", "extra"},
	} {
		if _, err := reviewBase(args); err == nil {
			t.Errorf("reviewBase(%v) error = nil, want error", args)
		}
	}
}

func TestReviewCommandDispatch(t *testing.T) {
	originalReviewCommand := reviewCommand
	t.Cleanup(func() {
		reviewCommand = originalReviewCommand
	})

	var gotBase string
	reviewCommand = func(baseRef string) error {
		gotBase = baseRef
		return nil
	}

	if err := run([]string{"review"}); err != nil {
		t.Fatalf("run(review) error = %v", err)
	}
	if gotBase != "" {
		t.Errorf("run(review) base = %q, want stored base", gotBase)
	}

	if err := run([]string{"review", "--base", "develop"}); err != nil {
		t.Fatalf("run(review --base develop) error = %v", err)
	}
	if gotBase != "develop" {
		t.Errorf("run(review --base develop) base = %q, want develop", gotBase)
	}

	if err := run([]string{"review", "--base"}); err == nil {
		t.Error("run(review --base) error = nil, want usage error")
	}

}

func TestRunAndReviewCommandDispatch(t *testing.T) {
	originalRunFeatureCommand := runFeatureCommand
	originalReviewFeatureCommand := reviewFeatureCommand
	t.Cleanup(func() {
		runFeatureCommand = originalRunFeatureCommand
		reviewFeatureCommand = originalReviewFeatureCommand
	})

	var gotFeatureName string
	var gotTask string
	var gotBase string
	runFeatureCommand = func(featureName, task string) error {
		gotFeatureName = featureName
		gotTask = task
		return nil
	}
	if err := run([]string{"feature/run", "run"}); err != nil {
		t.Fatalf("run(feature/run run) error = %v", err)
	}
	if gotFeatureName != "feature/run" || gotTask != "" {
		t.Errorf("run command = (%q, %q), want feature and empty task", gotFeatureName, gotTask)
	}

	if err := run([]string{"feature/run", "run", "-task", "write tests"}); err != nil {
		t.Fatalf("run(feature/run run -task) error = %v", err)
	}
	if gotTask != "write tests" {
		t.Errorf("run task = %q, want write tests", gotTask)
	}

	reviewFeatureCommand = func(featureName, baseRef string) error {
		gotFeatureName = featureName
		gotBase = baseRef
		return nil
	}
	if err := run([]string{"feature/review", "review"}); err != nil {
		t.Fatalf("run(feature/review review) error = %v", err)
	}
	if gotFeatureName != "feature/review" {
		t.Errorf("review feature = %q, want feature/review", gotFeatureName)
	}
	if gotBase != "" {
		t.Errorf("review base = %q, want stored base", gotBase)
	}

	if err := run([]string{"feature/review", "review", "--base", "main"}); err != nil {
		t.Fatalf("run(feature/review review --base main) error = %v", err)
	}
	if gotBase != "main" {
		t.Errorf("feature review base = %q, want main", gotBase)
	}

	if err := run([]string{"feature/run", "run", "-task"}); err == nil {
		t.Error("run(feature/run run -task) error = nil, want usage error")
	}
}

func TestSyncCommandDispatch(t *testing.T) {
	originalSyncFeatureCommand := syncFeatureCommand
	t.Cleanup(func() {
		syncFeatureCommand = originalSyncFeatureCommand
	})

	var gotFeatureName string
	syncFeatureCommand = func(featureName string) error {
		gotFeatureName = featureName
		return nil
	}

	if err := run([]string{"feature/sync", "sync"}); err != nil {
		t.Fatalf("run(feature/sync sync) error = %v", err)
	}
	if gotFeatureName != "feature/sync" {
		t.Errorf("sync feature = %q, want feature/sync", gotFeatureName)
	}
}

func TestSyncFeatureFetchesAndRebases(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath, remotePath := createRemoteMainRepository(t)
	worktreePath := filepath.Join(t.TempDir(), "clone")
	runMainGit(t, t.TempDir(), "clone", "-q", remotePath, worktreePath)
	runMainGit(t, worktreePath, "config", "user.email", "test@example.com")
	runMainGit(t, worktreePath, "config", "user.name", "Test User")
	runMainGit(t, worktreePath, "checkout", "-qb", "feature/sync", "origin/main")
	writeAndCommitMainFile(t, worktreePath, "feature.txt", "feature\n", "feature change")
	writeAndCommitMainFile(t, sourcePath, "base.txt", "base\n", "base change")
	runMainGit(t, sourcePath, "push", "-q", "origin", "main")

	if err := state.SaveSession("feature/sync", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: worktreePath, BaseBranch: "main",
	}}}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if err := syncFeature("feature/sync"); err != nil {
		t.Fatalf("syncFeature() error = %v", err)
	}

	status, err := gitcmd.CachedBaseStatus(worktreePath, "main")
	if err != nil {
		t.Fatalf("CachedBaseStatus() error = %v", err)
	}
	if status.Ahead != 1 || status.Behind != 0 {
		t.Errorf("status after sync = (%d ahead, %d behind), want (1, 0)", status.Ahead, status.Behind)
	}
}

func TestSyncFeatureDirtyPreflightDoesNotFetch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath, remotePath := createRemoteMainRepository(t)
	worktreePath := filepath.Join(t.TempDir(), "clone")
	runMainGit(t, t.TempDir(), "clone", "-q", remotePath, worktreePath)
	runMainGit(t, worktreePath, "checkout", "-qb", "feature/dirty", "origin/main")
	originalBase := mainGitOutput(t, worktreePath, "rev-parse", "origin/main")
	writeAndCommitMainFile(t, sourcePath, "base.txt", "base\n", "base change")
	runMainGit(t, sourcePath, "push", "-q", "origin", "main")
	if err := os.WriteFile(filepath.Join(worktreePath, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := state.SaveSession("feature/dirty", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: worktreePath, BaseBranch: "main",
	}}}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if err := syncFeature("feature/dirty"); err == nil {
		t.Fatal("syncFeature() error = nil, want dirty worktree error")
	}
	if got := mainGitOutput(t, worktreePath, "rev-parse", "origin/main"); got != originalBase {
		t.Errorf("origin/main changed during failed preflight: got %q, want %q", got, originalBase)
	}
}

func TestSessionDrift(t *testing.T) {
	requireMainGit(t)

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	runMainGit(t, repoPath, "checkout", "-qb", "feature/drift")
	writeAndCommitMainFile(t, repoPath, "feature.txt", "feature\n", "feature change")
	runMainGit(t, repoPath, "checkout", "main")
	writeAndCommitMainFile(t, repoPath, "base.txt", "base\n", "base change")
	runMainGit(t, repoPath, "checkout", "feature/drift")

	session := state.Session{Repos: []state.Repository{{Name: "repository", WorktreePath: repoPath, BaseBranch: "main"}}}
	if got := sessionDrift(session, "master"); got != "1 behind" {
		t.Errorf("sessionDrift() = %q, want 1 behind", got)
	}
	session.Repos[0].WorktreePath = filepath.Join(t.TempDir(), "missing")
	if got := sessionDrift(session, "master"); got != "unknown" {
		t.Errorf("sessionDrift() missing repository = %q, want unknown", got)
	}
}

func requireMainGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is not available")
	}
}

func createMainRepository(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()
	runMainGit(t, repoPath, "init", "-q")
	runMainGit(t, repoPath, "config", "user.email", "test@example.com")
	runMainGit(t, repoPath, "config", "user.name", "Test User")
	writeAndCommitMainFile(t, repoPath, "README.md", "initial\n", "initial commit")
	return repoPath
}

func createRemoteMainRepository(t *testing.T) (string, string) {
	t.Helper()
	sourcePath := createMainRepository(t)
	runMainGit(t, sourcePath, "branch", "-M", "main")
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	runMainGit(t, sourcePath, "init", "--bare", "-q", remotePath)
	runMainGit(t, sourcePath, "remote", "add", "origin", remotePath)
	runMainGit(t, sourcePath, "push", "-qu", "origin", "main")
	return sourcePath, remotePath
}

func writeAndCommitMainFile(t *testing.T, repoPath, name, contents, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runMainGit(t, repoPath, "add", name)
	runMainGit(t, repoPath, "commit", "-qm", message)
}

func runMainGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func mainGitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

func TestAppendTask(t *testing.T) {
	featureDir := t.TempDir()
	if err := appendTask(featureDir, "first task"); err != nil {
		t.Fatalf("appendTask() first write error = %v", err)
	}
	if err := appendTask(featureDir, "second task"); err != nil {
		t.Fatalf("appendTask() second write error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(featureDir, "TASK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "first task\nsecond task\n"; got != want {
		t.Errorf("TASK.md = %q, want %q", got, want)
	}
}

func TestFeatureBranchName(t *testing.T) {
	if got, want := featureBranchName("feature/potato"), "feature/potato"; got != want {
		t.Errorf("featureBranchName() = %q, want feature name", got)
	}
}

func TestHeadlessArgs(t *testing.T) {
	got := headlessArgs()
	if len(got) != 2 || got[0] != "-i" || got[1] != headlessPrompt {
		t.Errorf("headlessArgs() = %q, want [-i %q]", got, headlessPrompt)
	}
}

func TestIsWSLRelease(t *testing.T) {
	for release, want := range map[string]bool{
		"6.6.87.2-microsoft-standard-WSL2":   true,
		"5.15.153.1-microsoft-standard-WSL2": true,
		"6.8.0-31-generic":                   false,
	} {
		if got := isWSLRelease(release); got != want {
			t.Errorf("isWSLRelease(%q) = %t, want %t", release, got, want)
		}
	}
}

func TestIsRemoteURL(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"http://github.com/example/repo.git",
		"https://github.com/example/repo.git",
		"git@github.com:example/repo.git",
	} {
		if !isRemoteURL(value) {
			t.Errorf("isRemoteURL(%q) = false, want true", value)
		}
	}

	if isRemoteURL("ssh://git@github.com/example/repo.git") {
		t.Error("isRemoteURL() accepted an unsupported URL prefix")
	}
}

func TestRemoteRepositoryName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"http://github.com/example/repo-name.git":  "repo-name",
		"https://github.com/example/repo-name.git": "repo-name",
		"git@github.com:example/repo-name.git":     "repo-name",
		"https://github.com/example/repo-name/":    "repo-name",
	}
	for remoteURL, want := range tests {
		got, err := remoteRepositoryName(remoteURL)
		if err != nil {
			t.Errorf("remoteRepositoryName(%q) error = %v", remoteURL, err)
			continue
		}
		if got != want {
			t.Errorf("remoteRepositoryName(%q) = %q, want %q", remoteURL, got, want)
		}
	}
}

func TestValidateFeatureName(t *testing.T) {
	for _, featureName := range []string{
		"feature/potato",
		"bug/f321s-aaa",
		"feature/team/potato",
		"flat-feature",
	} {
		if err := validateFeatureName(featureName); err != nil {
			t.Errorf("validateFeatureName(%q) error = %v", featureName, err)
		}
	}

	for _, featureName := range []string{
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
		if err := validateFeatureName(featureName); err == nil {
			t.Errorf("validateFeatureName(%q) error = nil, want error", featureName)
		}
	}
}

func TestFeatureDirectoryName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"flat-feature":           "flat-feature",
		"feature/potato":         "feature%2Fpotato",
		"feature/team/potato":    "feature%2Fteam%2Fpotato",
		"feature%2Fpotato":       "feature%252Fpotato",
		"feature/potato%2Fextra": "feature%2Fpotato%252Fextra",
	}
	for featureName, want := range tests {
		if got := featureDirectoryName(featureName); got != want {
			t.Errorf("featureDirectoryName(%q) = %q, want %q", featureName, got, want)
		}
	}
}
