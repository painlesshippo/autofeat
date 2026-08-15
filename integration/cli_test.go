package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	Name         string `json:"name"`
	WorktreePath string `json:"worktree_path"`
}

func TestLocalFeatureLifecycle(t *testing.T) {
	requireCommand(t, "git")
	requireCommand(t, "go")

	rootDir := moduleRoot(t)
	binaryPath := filepath.Join(t.TempDir(), "autofeat")
	runRequiredCommand(t, rootDir, os.Environ(), "go", "build", "-o", binaryPath, "./cmd/autofeat")

	homeDir := t.TempDir()
	environment := environmentWithHome(homeDir)
	repositoryPath := filepath.Join(t.TempDir(), "repository")
	if err := os.Mkdir(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	initializeRepository(t, repositoryPath, environment)

	const featureName = "feature/integration"
	result := runAutofeat(binaryPath, repositoryPath, environment, "new", featureName)
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
	result = runAutofeat(binaryPath, outsideRepository, environment, "list")
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "FEATURE", "REPOSITORIES", featureName)

	result = runAutofeat(binaryPath, outsideRepository, environment, "status", featureName)
	requireSuccess(t, result)
	requireOutputContains(t, result.stdout, "FEATURE", "REPOSITORY", "WORKTREE", "STATE", featureName, repository.Name, "clean")

	dirtyPath := filepath.Join(repository.WorktreePath, "dirty.txt")
	if err := os.WriteFile(dirtyPath, []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result = runAutofeat(binaryPath, outsideRepository, environment, "teardown", featureName)
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

	result = runAutofeat(binaryPath, outsideRepository, environment, "teardown", featureName, "--force")
	requireSuccess(t, result)
	if _, err := os.Stat(wantFeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists after forced teardown: %v", err)
	}
	if _, ok := loadState(t, statePath).Sessions[featureName]; ok {
		t.Errorf("session %q remains after forced teardown", featureName)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	return filepath.Dir(filepath.Dir(filename))
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
