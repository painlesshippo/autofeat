package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var autofeatBinaryPath string

type commandResult struct {
	stdout string
	stderr string
	err    error
}

type persistedState struct {
	Sessions map[string]persistedSession `json:"sessions"`
}

type persistedSession struct {
	FeatureDir    string                `json:"feature_dir"`
	WorkspaceFile string                `json:"workspace_file"`
	Repos         []persistedRepository `json:"repos"`
}

type persistedRepository struct {
	Name          string `json:"name"`
	WorktreePath  string `json:"worktree_path"`
	IsRemoteClone bool   `json:"is_remote_clone"`
	BaseBranch    string `json:"base_branch"`
}

func TestMain(m *testing.M) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "locate integration test source")
		os.Exit(1)
	}
	rootDir := filepath.Dir(filepath.Dir(filename))
	binaryDir, err := os.MkdirTemp("", "autofeat-integration-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create integration binary directory: %v\n", err)
		os.Exit(1)
	}
	autofeatBinaryPath = filepath.Join(binaryDir, "autofeat")
	command := exec.Command("go", "build", "-o", autofeatBinaryPath, "./cmd/autofeat")
	command.Dir = rootDir
	if output, err := command.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build autofeat integration binary: %v\n%s", err, output)
		_ = os.RemoveAll(binaryDir)
		os.Exit(1)
	}

	exitCode := m.Run()
	if err := os.RemoveAll(binaryDir); err != nil && exitCode == 0 {
		fmt.Fprintf(os.Stderr, "remove integration binary directory: %v\n", err)
		exitCode = 1
	}
	os.Exit(exitCode)
}

func TestLocalFeatureLifecycle(t *testing.T) {
	requireCommand(t, "git")

	homeDir := t.TempDir()
	environment := environmentWithHome(homeDir)
	repositoryPath := filepath.Join(t.TempDir(), "repository")
	if err := os.Mkdir(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	initializeRepository(t, repositoryPath, environment)

	const featureName = "feature/integration"
	result := runAutofeat(autofeatBinaryPath, repositoryPath, environment, "new", featureName)
	requireSuccess(t, result)

	statePath := filepath.Join(homeDir, ".autofeat", "state.json")
	state := loadState(t, statePath)
	session, ok := state.Sessions[featureName]
	if !ok {
		t.Fatalf("state does not contain session %q: %+v", featureName, state.Sessions)
	}
	if len(session.Repos) != 1 {
		t.Fatalf("session repositories = %+v, want one repository", session.Repos)
	}
	repository := session.Repos[0]
	wantFeatureDir := filepath.Join(homeDir, ".autofeat-workspaces", "feature%2Fintegration")
	if session.FeatureDir != wantFeatureDir {
		t.Errorf("feature directory = %q, want %q", session.FeatureDir, wantFeatureDir)
	}
	if filepath.Dir(repository.WorktreePath) != wantFeatureDir {
		t.Errorf("worktree directory = %q, want parent %q", repository.WorktreePath, wantFeatureDir)
	}
	if got := strings.TrimSpace(runRequiredCommand(t, repository.WorktreePath, environment, "git", "branch", "--show-current")); got != featureName {
		t.Errorf("worktree branch = %q, want %q", got, featureName)
	}
	workspaceContents, err := os.ReadFile(session.WorkspaceFile)
	if err != nil {
		t.Fatalf("read workspace file: %v", err)
	}
	if !bytes.Contains(workspaceContents, []byte(`"path": "./`+repository.Name+`"`)) {
		t.Errorf("workspace file does not contain repository %q:\n%s", repository.Name, workspaceContents)
	}

	outsideRepository := t.TempDir()
	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "list")
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "FEATURE", "REPOSITORIES", featureName)

	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "status", featureName)
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "FEATURE", "REPOSITORY", "WORKTREE", "STATE", featureName, repository.Name, "clean")

	dirtyPath := filepath.Join(repository.WorktreePath, "dirty.txt")
	if err := os.WriteFile(dirtyPath, []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "teardown", featureName)
	if result.err == nil {
		t.Fatalf("teardown succeeded for dirty worktree; stdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
	}
	requireOutputContains(t, result.stderr, "autofeat:", "uncommitted changes", "--force")
	if _, err := os.Stat(repository.WorktreePath); err != nil {
		t.Fatalf("dirty worktree was removed: %v", err)
	}
	if _, ok := loadState(t, statePath).Sessions[featureName]; !ok {
		t.Fatalf("session %q was removed after rejected teardown", featureName)
	}

	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "teardown", featureName, "--force")
	requireSuccess(t, result)
	if _, err := os.Stat(wantFeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists after forced teardown: %v", err)
	}
	if _, ok := loadState(t, statePath).Sessions[featureName]; ok {
		t.Errorf("session %q remains after forced teardown", featureName)
	}
}

func TestExplicitLocalRepositoryCreatesWorktree(t *testing.T) {
	requireCommand(t, "git")

	homeDir := t.TempDir()
	environment := environmentWithHome(homeDir)
	repositoryPath := filepath.Join(t.TempDir(), "repository")
	if err := os.Mkdir(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	initializeRepository(t, repositoryPath, environment)

	const featureName = "feature/explicit-local"
	outsideRepository := t.TempDir()
	result := runAutofeat(autofeatBinaryPath, outsideRepository, environment, "new", featureName, "--local", repositoryPath)
	requireSuccess(t, result)

	statePath := filepath.Join(homeDir, ".autofeat", "state.json")
	session := loadState(t, statePath).Sessions[featureName]
	if len(session.Repos) != 1 {
		t.Fatalf("session repositories = %+v, want one repository", session.Repos)
	}
	repository := session.Repos[0]
	if repository.IsRemoteClone {
		t.Error("explicit local repository was recorded as a remote clone")
	}
	if got := strings.TrimSpace(runRequiredCommand(t, repository.WorktreePath, environment, "git", "branch", "--show-current")); got != featureName {
		t.Errorf("worktree branch = %q, want %q", got, featureName)
	}

	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "teardown", featureName)
	requireSuccess(t, result)
}

func TestRemoveLocalRepositoryLifecycle(t *testing.T) {
	requireCommand(t, "git")

	homeDir := t.TempDir()
	environment := environmentWithHome(homeDir)
	repositoryPaths := []string{
		filepath.Join(t.TempDir(), "first"),
		filepath.Join(t.TempDir(), "second"),
	}
	for _, repositoryPath := range repositoryPaths {
		if err := os.Mkdir(repositoryPath, 0o755); err != nil {
			t.Fatal(err)
		}
		initializeRepository(t, repositoryPath, environment)
	}

	const featureName = "feature/remove-integration"
	outsideRepository := t.TempDir()
	for _, repositoryPath := range repositoryPaths {
		result := runAutofeat(autofeatBinaryPath, outsideRepository, environment, "new", featureName, "--local", repositoryPath)
		requireSuccess(t, result)
	}

	statePath := filepath.Join(homeDir, ".autofeat", "state.json")
	before := loadState(t, statePath).Sessions[featureName]
	if len(before.Repos) != 2 {
		t.Fatalf("session repositories = %+v, want two repositories", before.Repos)
	}
	removedRepository := before.Repos[0]
	result := runAutofeat(autofeatBinaryPath, outsideRepository, environment, "remove", featureName, "--local", repositoryPaths[0])
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "Removed", removedRepository.Name, featureName)

	if _, err := os.Stat(removedRepository.WorktreePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("removed worktree still exists: %v", err)
	}
	if branch := strings.TrimSpace(runRequiredCommand(t, repositoryPaths[0], environment, "git", "branch", "--list", featureName)); branch == "" {
		t.Error("removed repository feature branch was deleted")
	}
	after := loadState(t, statePath).Sessions[featureName]
	if len(after.Repos) != 1 || after.Repos[0].WorktreePath != before.Repos[1].WorktreePath {
		t.Fatalf("remaining repositories = %+v, want only second repository", after.Repos)
	}
	workspaceContents, err := os.ReadFile(after.WorkspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(workspaceContents, []byte(filepath.Base(removedRepository.WorktreePath))) {
		t.Errorf("workspace still contains removed repository:\n%s", workspaceContents)
	}
	if !bytes.Contains(workspaceContents, []byte(filepath.Base(after.Repos[0].WorktreePath))) {
		t.Errorf("workspace does not contain remaining repository:\n%s", workspaceContents)
	}

	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "remove", featureName, "--local", repositoryPaths[1])
	requireSuccess(t, result)
	if _, err := os.Stat(after.FeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists after final removal: %v", err)
	}
	if _, ok := loadState(t, statePath).Sessions[featureName]; ok {
		t.Errorf("session %q remains after final removal", featureName)
	}
}

func TestRemoteFeatureSyncLifecycle(t *testing.T) {
	requireCommand(t, "git")

	homeDir := t.TempDir()
	environment := environmentWithHome(homeDir)
	sourcePath := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(sourcePath, 0o755); err != nil {
		t.Fatal(err)
	}
	initializeRepository(t, sourcePath, environment)

	remotePath := filepath.Join(t.TempDir(), "repository.git")
	runRequiredCommand(t, sourcePath, environment, "git", "init", "--bare", "-q", remotePath)
	runRequiredCommand(t, remotePath, environment, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	runRequiredCommand(t, sourcePath, environment, "git", "remote", "add", "origin", remotePath)
	runRequiredCommand(t, sourcePath, environment, "git", "push", "-qu", "origin", "main")
	runRequiredCommand(t, sourcePath, environment, "git", "checkout", "-qb", "develop")
	writeAndCommitFile(t, sourcePath, environment, "develop.txt", "develop\n", "develop commit")
	runRequiredCommand(t, sourcePath, environment, "git", "push", "-qu", "origin", "develop")

	const remoteURL = "https://example.invalid/repository.git"
	localRemoteURL := "file://" + filepath.ToSlash(remotePath)
	runRequiredCommand(t, sourcePath, environment, "git", "config", "--global", "url."+localRemoteURL+".insteadOf", remoteURL)

	const featureName = "feature/remote-sync"
	outsideRepository := t.TempDir()
	result := runAutofeat(autofeatBinaryPath, outsideRepository, environment, "new", featureName, "--remote", remoteURL, "--ref", "develop")
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "Cloned remote repo", "repository", featureName)

	statePath := filepath.Join(homeDir, ".autofeat", "state.json")
	session, ok := loadState(t, statePath).Sessions[featureName]
	if !ok {
		t.Fatalf("state does not contain session %q", featureName)
	}
	if len(session.Repos) != 1 {
		t.Fatalf("session repositories = %+v, want one repository", session.Repos)
	}
	repository := session.Repos[0]
	if !repository.IsRemoteClone {
		t.Errorf("repository is_remote_clone = false, want true")
	}
	if repository.BaseBranch != "develop" {
		t.Errorf("repository base branch = %q, want develop", repository.BaseBranch)
	}
	if got := strings.TrimSpace(runRequiredCommand(t, repository.WorktreePath, environment, "git", "branch", "--show-current")); got != featureName {
		t.Errorf("worktree branch = %q, want %q", got, featureName)
	}

	runRequiredCommand(t, repository.WorktreePath, environment, "git", "config", "user.email", "integration@example.com")
	runRequiredCommand(t, repository.WorktreePath, environment, "git", "config", "user.name", "Integration Test")
	writeAndCommitFile(t, repository.WorktreePath, environment, "feature.txt", "feature\n", "feature change")
	writeAndCommitFile(t, sourcePath, environment, "base.txt", "base\n", "base change")
	runRequiredCommand(t, sourcePath, environment, "git", "push", "-q", "origin", "develop")

	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "sync", featureName)
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "1 ahead, 1 behind", "synchronized (1 ahead, 0 behind)")
	if got := strings.TrimSpace(runRequiredCommand(t, repository.WorktreePath, environment, "git", "rev-list", "--count", "origin/develop..HEAD")); got != "1" {
		t.Errorf("feature commits ahead of origin/develop = %q, want 1", got)
	}
	if got := strings.TrimSpace(runRequiredCommand(t, repository.WorktreePath, environment, "git", "rev-list", "--count", "HEAD..origin/develop")); got != "0" {
		t.Errorf("feature commits behind origin/develop = %q, want 0", got)
	}

	result = runAutofeat(autofeatBinaryPath, outsideRepository, environment, "teardown", featureName)
	requireSuccess(t, result)
	if _, err := os.Stat(session.FeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("remote feature directory still exists after teardown: %v", err)
	}
	if _, ok := loadState(t, statePath).Sessions[featureName]; ok {
		t.Errorf("session %q remains after teardown", featureName)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not available", name)
	}
}

func environmentWithHome(homeDir string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "HOME=") {
			environment = append(environment, value)
		}
	}
	return append(environment, "HOME="+homeDir)
}

func initializeRepository(t *testing.T, repositoryPath string, environment []string) {
	t.Helper()
	runRequiredCommand(t, repositoryPath, environment, "git", "init", "-q")
	runRequiredCommand(t, repositoryPath, environment, "git", "config", "user.email", "integration@example.com")
	runRequiredCommand(t, repositoryPath, environment, "git", "config", "user.name", "Integration Test")
	if err := os.WriteFile(filepath.Join(repositoryPath, "README.md"), []byte("integration fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRequiredCommand(t, repositoryPath, environment, "git", "add", "README.md")
	runRequiredCommand(t, repositoryPath, environment, "git", "commit", "-qm", "initial commit")
	runRequiredCommand(t, repositoryPath, environment, "git", "branch", "-M", "main")
}

func writeAndCommitFile(t *testing.T, repositoryPath string, environment []string, name, contents, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repositoryPath, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runRequiredCommand(t, repositoryPath, environment, "git", "add", name)
	runRequiredCommand(t, repositoryPath, environment, "git", "commit", "-qm", message)
}

func runAutofeat(binaryPath, directory string, environment []string, args ...string) commandResult {
	command := exec.Command(binaryPath, args...)
	command.Dir = directory
	command.Env = environment
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func runRequiredCommand(t *testing.T, directory string, environment []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, output)
	}
	return string(output)
}

func requireSuccess(t *testing.T, result commandResult) {
	t.Helper()
	if result.err != nil {
		t.Fatalf("autofeat failed: %v\nstdout:\n%s\nstderr:\n%s", result.err, result.stdout, result.stderr)
	}
}

func requireOutputContains(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Errorf("output does not contain %q:\n%s", value, output)
		}
	}
}

func loadState(t *testing.T, path string) persistedState {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state persistedState
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatalf("parse state file: %v", err)
	}
	return state
}
