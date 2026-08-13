package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/painlesshippo/autofeat/internal/config"
	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/hooks"
	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/templates"
	"github.com/spf13/cobra"
)

func TestVersionCommand(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatalf("run(version) error = %v", err)
	}

	if err := run([]string{"version", "extra"}); err == nil {
		t.Error("run(version, extra) error = nil, want usage error")
	}
}

func TestConfigCommandDispatch(t *testing.T) {
	originalOpenConfigCommand := openConfigCommand
	t.Cleanup(func() {
		openConfigCommand = originalOpenConfigCommand
	})

	called := false
	openConfigCommand = func() error {
		called = true
		return nil
	}

	if err := run([]string{"config"}); err != nil {
		t.Fatalf("run(config) error = %v", err)
	}
	if !called {
		t.Error("run(config) did not open the config")
	}
	if err := run([]string{"config", "extra"}); err == nil {
		t.Error("run(config, extra) error = nil, want usage error")
	}
}

func TestFeatureCompletions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/zulu", "bug/fix", "feature/team/nested"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}

	got, directive := featureCompletions([]string{"bug/fix"}, "feature/")
	want := []string{"feature/team/nested", "feature/zulu"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("featureCompletions() = %q, want %q", got, want)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("featureCompletions() directive = %v, want NoFileComp", directive)
	}
}

func TestFeatureCompletionsEmptyAndMalformedState(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		got, directive := featureCompletions(nil, "")
		if len(got) != 0 {
			t.Errorf("featureCompletions() = %q, want no candidates", got)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("featureCompletions() directive = %v, want NoFileComp", directive)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		stateDir := filepath.Join(homeDir, ".autofeat")
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}

		got, directive := featureCompletions(nil, "")
		if len(got) != 0 {
			t.Errorf("featureCompletions() = %q, want silent empty candidates", got)
		}
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("featureCompletions() directive = %v, want NoFileComp", directive)
		}
	})
}

func TestCompletionCommandArguments(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "bash", "extra"}, {"completion", "powershell", "extra"}} {
		if err := run(args); err == nil {
			t.Errorf("run(%q) error = nil, want usage error", args)
		}
	}
}

func TestTemplateCommandArguments(t *testing.T) {
	invalid := [][]string{
		{"template"},
		{"template", "unknown"},
		{"template", "show"},
		{"template", "save", "full-stack", "feature/source"},
		{"new", "feature/test", "--template"},
		{"new", "feature/test", "--unknown", "full-stack"},
		{"new", "feature/test", "--template", "full-stack", "--ref", "develop"},
	}
	for _, args := range invalid {
		if err := run(args); err == nil {
			t.Errorf("run(%q) error = nil, want usage error", args)
		}
	}
}

func TestGeneratedBashCompletionSyntax(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}

	var completion bytes.Buffer
	command := newRootCommand()
	command.SetOut(&completion)
	command.SetArgs([]string{"completion", "bash"})
	if err := command.Execute(); err != nil {
		t.Fatalf("completion bash error = %v", err)
	}
	if completion.Len() == 0 {
		t.Fatal("completion bash generated empty output")
	}

	syntaxCommand := exec.Command(bashPath, "-n")
	syntaxCommand.Stdin = strings.NewReader(completion.String())
	if output, err := syntaxCommand.CombinedOutput(); err != nil {
		t.Fatalf("bash -n completion error = %v\n%s", err, output)
	}
}

func TestGeneratedPowerShellCompletion(t *testing.T) {
	var completion bytes.Buffer
	rootCommand := newRootCommand()
	rootCommand.SetOut(&completion)
	rootCommand.SetArgs([]string{"completion", "powershell"})
	if err := rootCommand.Execute(); err != nil {
		t.Fatalf("completion powershell error = %v", err)
	}
	if !strings.Contains(completion.String(), "Register-ArgumentCompleter") {
		t.Fatal("PowerShell completion does not register an argument completer")
	}

	powershellPath := ""
	for _, name := range []string{"pwsh", "powershell", "powershell.exe"} {
		path, err := exec.LookPath(name)
		if err == nil {
			powershellPath = path
			break
		}
	}
	if powershellPath == "" {
		t.Skip("PowerShell is not available")
	}

	command := exec.Command(powershellPath, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "-")
	command.Stdin = strings.NewReader(completion.String())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell completion error = %v\n%s", err, output)
	}
}

func TestCobraCompletionProtocol(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/zulu", "bug/fix", "feature/team/nested"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}

	var output bytes.Buffer
	var errorOutput bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetErr(&errorOutput)
	command.SetArgs([]string{"__complete", "sync", "bug/fix", "feature/"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Cobra completion error = %v", err)
	}
	if got, want := output.String(), "feature/team/nested\nfeature/zulu\n:4\n"; got != want {
		t.Errorf("Cobra completion output = %q, want %q", got, want)
	}

	output.Reset()
	errorOutput.Reset()
	command = newRootCommand()
	command.SetOut(&output)
	command.SetErr(&errorOutput)
	command.SetArgs([]string{"__complete", "new", "feature/new", "--template", "full-stack", "-"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Cobra flag completion error = %v", err)
	}
	if strings.Contains(output.String(), "--ref") {
		t.Errorf("Cobra completion offered mutually exclusive --ref:\n%s", output.String())
	}

	for _, args := range [][]string{
		{"__complete", "list", ""},
		{"__complete", "config", ""},
		{"__complete", "version", ""},
		{"__complete", "template", "list", ""},
		{"__complete", "completion", "bash", ""},
		{"__complete", "completion", "powershell", ""},
	} {
		output.Reset()
		errorOutput.Reset()
		command = newRootCommand()
		command.SetOut(&output)
		command.SetErr(&errorOutput)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Cobra completion for %q error = %v", args, err)
		}
		if got, want := output.String(), ":4\n"; got != want {
			t.Errorf("Cobra completion for %q = %q, want %q", args, got, want)
		}
	}
}

func TestTemplateCompletions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"full-stack", "backend", "frontend"} {
		template := templates.Template{Repositories: []templates.Repository{{
			Kind:   templates.RemoteRepository,
			Source: "https://example.com/repository.git",
		}}}
		if err := templates.Put(name, template); err != nil {
			t.Fatal(err)
		}
	}

	got, directive := completeTemplateNames(nil, nil, "f")
	want := []string{"frontend", "full-stack"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("completeTemplateNames() = %q, want %q", got, want)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeTemplateNames() directive = %v, want NoFileComp", directive)
	}
}

func TestStatusCommandDispatch(t *testing.T) {
	originalStatusCommand := statusCommand
	t.Cleanup(func() {
		statusCommand = originalStatusCommand
	})

	var gotSelectors []string
	statusCommand = func(selectors []string) error {
		gotSelectors = selectors
		return nil
	}

	if err := run([]string{"status"}); err != nil {
		t.Fatalf("run(status) error = %v", err)
	}
	if len(gotSelectors) != 1 || gotSelectors[0] != "*" {
		t.Errorf("run(status) selectors = %q, want [*]", gotSelectors)
	}

	if err := run([]string{"status", "feature/a", "feature/b"}); err != nil {
		t.Fatalf("run(status feature/a feature/b) error = %v", err)
	}
	if len(gotSelectors) != 2 || gotSelectors[0] != "feature/a" || gotSelectors[1] != "feature/b" {
		t.Errorf("run(status feature/a feature/b) selectors = %q, want exact selectors", gotSelectors)
	}

	if err := run([]string{"status", "--json"}); err == nil {
		t.Error("run(status --json) error = nil, want usage error")
	}
}

func TestRunCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/a", "feature/b"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}
	originalRunFeatureCommand := runFeatureCommand
	t.Cleanup(func() {
		runFeatureCommand = originalRunFeatureCommand
	})

	var gotFeatureNames []string
	var gotTask string
	runFeatureCommand = func(featureName, task string) error {
		gotFeatureNames = append(gotFeatureNames, featureName)
		gotTask = task
		return nil
	}
	if err := run([]string{"run", "feature/a"}); err != nil {
		t.Fatalf("run(run feature/a) error = %v", err)
	}
	if len(gotFeatureNames) != 1 || gotFeatureNames[0] != "feature/a" || gotTask != "" {
		t.Errorf("run command = (%q, %q), want ([feature/a], empty task)", gotFeatureNames, gotTask)
	}

	gotFeatureNames = nil
	if err := run([]string{"run", "feature/*", "--task", "write tests"}); err != nil {
		t.Fatalf("run(run feature/* --task) error = %v", err)
	}
	if len(gotFeatureNames) != 2 || gotFeatureNames[0] != "feature/a" || gotFeatureNames[1] != "feature/b" || gotTask != "write tests" {
		t.Errorf("run wildcard = (%q, %q), want sorted features and task", gotFeatureNames, gotTask)
	}

	if err := run([]string{"run", "feature/a", "-task", "write tests"}); err == nil {
		t.Error("run(run feature/a -task) error = nil, want unsupported legacy flag")
	}
}

func TestOpenCommandCopilotDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := state.SaveSession("feature/copilot", state.Session{}); err != nil {
		t.Fatal(err)
	}
	originalOpenFeatureCommand := openFeatureCommand
	originalOpenCopilotCommand := openCopilotCommand
	t.Cleanup(func() {
		openFeatureCommand = originalOpenFeatureCommand
		openCopilotCommand = originalOpenCopilotCommand
	})

	var openedWith string
	openFeatureCommand = func(string) error {
		openedWith = "editor"
		return nil
	}
	openCopilotCommand = func(string) error {
		openedWith = "copilot"
		return nil
	}

	if err := run([]string{"open", "feature/copilot", "--copilot"}); err != nil {
		t.Fatalf("run(open feature/copilot --copilot) error = %v", err)
	}
	if openedWith != "copilot" {
		t.Errorf("open command = %q, want copilot", openedWith)
	}
}

func TestSyncCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := state.SaveSession("feature/sync", state.Session{}); err != nil {
		t.Fatal(err)
	}
	originalSyncFeatureCommand := syncFeatureCommand
	t.Cleanup(func() {
		syncFeatureCommand = originalSyncFeatureCommand
	})

	var gotFeatureName string
	syncFeatureCommand = func(featureName string) error {
		gotFeatureName = featureName
		return nil
	}

	if err := run([]string{"sync", "feature/sync"}); err != nil {
		t.Fatalf("run(sync feature/sync) error = %v", err)
	}
	if gotFeatureName != "feature/sync" {
		t.Errorf("sync feature = %q, want feature/sync", gotFeatureName)
	}
}

func TestTeardownCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/x", "feature/y"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}
	originalTeardownCommand := teardownCommand
	t.Cleanup(func() {
		teardownCommand = originalTeardownCommand
	})

	var gotFeatureNames []string
	var gotForce bool
	teardownCommand = func(featureName string, force bool) error {
		gotFeatureNames = append(gotFeatureNames, featureName)
		gotForce = force
		return nil
	}

	if err := run([]string{"teardown", "feature/y", "feature/x", "--force"}); err != nil {
		t.Fatalf("run(teardown feature/y feature/x --force) error = %v", err)
	}
	if len(gotFeatureNames) != 2 || gotFeatureNames[0] != "feature/x" || gotFeatureNames[1] != "feature/y" || !gotForce {
		t.Errorf("teardown dispatch = (%q, %t), want sorted features and force", gotFeatureNames, gotForce)
	}
	if err := run([]string{"feature/x", "teardown"}); err == nil {
		t.Error("run(feature/x teardown) error = nil, want command-first usage error")
	}
}

func TestSelectFeatureNames(t *testing.T) {
	sessions := map[string]state.Session{
		"bug/fix":             {},
		"feature/a21":         {},
		"feature/beta":        {},
		"feature/team/nested": {},
	}
	tests := []struct {
		name      string
		selectors []string
		want      []string
	}{
		{name: "exact", selectors: []string{"feature/a21"}, want: []string{"feature/a21"}},
		{name: "all", selectors: []string{"*"}, want: []string{"bug/fix", "feature/a21", "feature/beta", "feature/team/nested"}},
		{name: "prefix pattern", selectors: []string{"feature/*"}, want: []string{"feature/a21", "feature/beta", "feature/team/nested"}},
		{name: "overlapping selectors", selectors: []string{"feature/*", "feature/a21"}, want: []string{"feature/a21", "feature/beta", "feature/team/nested"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectFeatureNames(sessions, test.selectors)
			if err != nil {
				t.Fatalf("selectFeatureNames(%q) error = %v", test.selectors, err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("selectFeatureNames(%q) = %q, want %q", test.selectors, got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("selectFeatureNames(%q) = %q, want %q", test.selectors, got, test.want)
					break
				}
			}
		})
	}

	for _, selectors := range [][]string{nil, {"feature/missing"}, {"feature/["}} {
		if _, err := selectFeatureNames(sessions, selectors); err == nil {
			t.Errorf("selectFeatureNames(%q) error = nil, want error", selectors)
		}
	}
}

func TestMatchFeatureSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		selector    string
		featureName string
		want        bool
	}{
		{selector: "feature/a", featureName: "feature/a", want: true},
		{selector: "feature/a", featureName: "feature/ab", want: false},
		{selector: "feature/*", featureName: "feature/a", want: true},
		{selector: "feature/*", featureName: "bug/a", want: false},
		{selector: "*suffix", featureName: "feature/with-suffix", want: true},
		{selector: "*suffix", featureName: "feature/with-suffix-not", want: false},
		{selector: "*middle*", featureName: "feature/middle/nested", want: true},
		{selector: "*middle*", featureName: "feature/other", want: false},
		{selector: "a*b*c", featureName: "a1b2c", want: true},
		{selector: "a*b*c", featureName: "a1c2b", want: false},
		{selector: "a**c", featureName: "abc", want: true},
		{selector: "*", featureName: "anything", want: true},
	}
	for _, test := range tests {
		if got := matchFeatureSelector(test.selector, test.featureName); got != test.want {
			t.Errorf("matchFeatureSelector(%q, %q) = %t, want %t", test.selector, test.featureName, got, test.want)
		}
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

func TestSyncFeatureAcceptsTagBaseWithoutFetchingBranch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath, remotePath := createRemoteMainRepository(t)
	runMainGit(t, sourcePath, "tag", "v1.2.3")
	runMainGit(t, sourcePath, "push", "-q", "origin", "v1.2.3")
	worktreePath := filepath.Join(t.TempDir(), "clone")
	runMainGit(t, t.TempDir(), "clone", "-q", remotePath, worktreePath)
	runMainGit(t, worktreePath, "checkout", "-qb", "feature/tag", "v1.2.3")

	if err := state.SaveSession("feature/tag", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: worktreePath, BaseBranch: "v1.2.3",
	}}}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if err := syncFeature("feature/tag"); err != nil {
		t.Fatalf("syncFeature() tag error = %v", err)
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

func TestSyncFeaturePreflightRejections(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	t.Run("wrong branch", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "other")
		if err := state.SaveSession("feature/wrong", state.Session{Repos: []state.Repository{{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}}}); err != nil {
			t.Fatal(err)
		}

		err := syncFeature("feature/wrong")
		if err == nil || !strings.Contains(err.Error(), "is on branch") {
			t.Errorf("syncFeature() error = %v, want wrong-branch error", err)
		}
	})

	t.Run("rebase in progress", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "feature/rebasing")
		rebasePath := strings.TrimSpace(mainGitOutput(t, repoPath, "rev-parse", "--git-path", "rebase-merge"))
		if !filepath.IsAbs(rebasePath) {
			rebasePath = filepath.Join(repoPath, rebasePath)
		}
		if err := os.MkdirAll(rebasePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := state.SaveSession("feature/rebasing", state.Session{Repos: []state.Repository{{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}}}); err != nil {
			t.Fatal(err)
		}

		err := syncFeature("feature/rebasing")
		if err == nil || !strings.Contains(err.Error(), "rebase in progress") {
			t.Errorf("syncFeature() error = %v, want rebase-in-progress error", err)
		}
	})

	t.Run("missing worktree", func(t *testing.T) {
		if err := state.SaveSession("feature/missing", state.Session{Repos: []state.Repository{{
			Name: "repository", WorktreePath: filepath.Join(t.TempDir(), "missing"),
		}}}); err != nil {
			t.Fatal(err)
		}

		if err := syncFeature("feature/missing"); err == nil {
			t.Error("syncFeature() error = nil, want missing worktree error")
		}
	})
}

func TestSyncFeatureConflictLeavesRebaseResumable(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	runMainGit(t, repoPath, "checkout", "-qb", "feature/conflict")
	writeAndCommitMainFile(t, repoPath, "README.md", "feature\n", "feature change")
	runMainGit(t, repoPath, "checkout", "main")
	writeAndCommitMainFile(t, repoPath, "README.md", "base\n", "base change")
	runMainGit(t, repoPath, "checkout", "feature/conflict")
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repoPath, "rebase", "--abort").Run()
	})

	if err := state.SaveSession("feature/conflict", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
	}}}); err != nil {
		t.Fatal(err)
	}

	if err := syncFeature("feature/conflict"); err == nil {
		t.Fatal("syncFeature() error = nil, want rebase conflict")
	}
	rebasing, err := gitcmd.IsRebaseInProgress(repoPath)
	if err != nil {
		t.Fatalf("IsRebaseInProgress() error = %v", err)
	}
	if !rebasing {
		t.Error("rebase is not in progress after conflict, want resumable rebase")
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

func TestStatusStatePrecedence(t *testing.T) {
	tests := []struct {
		name   string
		status repositoryStatus
		want   string
	}{
		{name: "error", status: repositoryStatus{inspectionError: true, missing: true}, want: "error"},
		{name: "missing", status: repositoryStatus{missing: true, detached: true}, want: "missing"},
		{name: "detached", status: repositoryStatus{detached: true, wrongBranch: true}, want: "detached"},
		{name: "wrong branch", status: repositoryStatus{wrongBranch: true, rebasing: true}, want: "wrong-branch"},
		{name: "rebasing", status: repositoryStatus{rebasing: true, baseKnown: true, baseBehind: 1}, want: "rebasing"},
		{name: "behind base", status: repositoryStatus{baseKnown: true, baseBehind: 1, dirty: true}, want: "behind-base"},
		{name: "dirty", status: repositoryStatus{dirty: true, pushKnown: true, pushAhead: 1, pushBehind: 1}, want: "dirty"},
		{name: "remote diverged", status: repositoryStatus{pushKnown: true, pushAhead: 1, pushBehind: 1}, want: "remote-diverged"},
		{name: "remote ahead", status: repositoryStatus{pushKnown: true, pushBehind: 1}, want: "remote-ahead"},
		{name: "unpublished", status: repositoryStatus{pushKnown: true, pushHasOrigin: true}, want: "unpushed"},
		{name: "unpushed commits", status: repositoryStatus{pushKnown: true, pushHasOrigin: true, pushPublished: true, pushAhead: 1}, want: "unpushed"},
		{name: "ready without origin", status: repositoryStatus{baseKnown: true, pushKnown: true}, want: "ready"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := statusState(test.status); got != test.want {
				t.Errorf("statusState(%+v) = %q, want %q", test.status, got, test.want)
			}
		})
	}
}

func TestStatusSessionsRendersSortedRepositoryHealth(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	dirtyRepo := createMainRepository(t)
	runMainGit(t, dirtyRepo, "branch", "-M", "main")
	runMainGit(t, dirtyRepo, "checkout", "-qb", "feature/alpha")
	if err := os.WriteFile(filepath.Join(dirtyRepo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	readyRepo := createMainRepository(t)
	runMainGit(t, readyRepo, "branch", "-M", "main")
	runMainGit(t, readyRepo, "checkout", "-qb", "feature/beta")
	notRepository := t.TempDir()
	missingPath := filepath.Join(t.TempDir(), "missing")

	if err := state.SaveSession("feature/beta", state.Session{Repos: []state.Repository{{
		Name: "ready", WorktreePath: readyRepo, BaseBranch: "main",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSession("feature/alpha", state.Session{Repos: []state.Repository{
		{Name: "z-error", WorktreePath: notRepository, BaseBranch: "main"},
		{Name: "a-missing", WorktreePath: missingPath},
		{Name: "m-dirty", WorktreePath: dirtyRepo, BaseBranch: "main"},
	}}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := statusSessionsTo(&output, []string{"*"}); err != nil {
		t.Fatalf("statusSessionsTo() error = %v", err)
	}
	report := output.String()
	for _, want := range []string{
		"FEATURE", "REPOSITORY", "WORKTREE", "DRIFT", "PUSH", "STATE", "DETAIL",
		"feature/alpha", "a-missing", "master", "missing",
		"m-dirty", "feature/alpha", "dirty", "+0/-0", "n/a",
		"z-error", "error",
		"feature/beta", "ready", "feature/beta", "clean",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("status report does not contain %q:\n%s", want, report)
		}
	}

	positions := []int{
		strings.Index(report, "a-missing"),
		strings.Index(report, "m-dirty"),
		strings.Index(report, "z-error"),
		strings.Index(report, "feature/beta"),
	}
	for index := 1; index < len(positions); index++ {
		if positions[index-1] < 0 || positions[index] <= positions[index-1] {
			t.Fatalf("status report is not sorted as expected: positions %v\n%s", positions, report)
		}
	}

	output.Reset()
	if err := statusSessionsTo(&output, []string{"feature/beta"}); err != nil {
		t.Fatalf("statusSessionsTo(feature/beta) error = %v", err)
	}
	if selectedReport := output.String(); !strings.Contains(selectedReport, "feature/beta") || strings.Contains(selectedReport, "feature/alpha") {
		t.Errorf("selected status report contains the wrong features:\n%s", selectedReport)
	}
	if err := statusSessionsTo(&output, []string{"feature/missing"}); err == nil {
		t.Error("statusSessionsTo(feature/missing) error = nil, want no-match error")
	}
}

func TestStatusSessionsEmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output bytes.Buffer
	if err := statusSessionsTo(&output, []string{"*"}); err != nil {
		t.Fatalf("statusSessionsTo() empty state error = %v", err)
	}
	if fields := strings.Fields(output.String()); len(fields) != 9 {
		t.Errorf("empty status report fields = %q, want header only", fields)
	}
}

func TestInspectRepositoryStatusGitStates(t *testing.T) {
	requireMainGit(t)

	t.Run("wrong branch and detached", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "other")
		repository := state.Repository{Name: "repository", WorktreePath: repoPath, BaseBranch: "main"}

		status := inspectRepositoryStatus("feature/status", repository, "master")
		if got := statusState(status); got != "wrong-branch" {
			t.Errorf("wrong branch state = %q, want wrong-branch; status = %+v", got, status)
		}

		runMainGit(t, repoPath, "checkout", "--detach", "-q")
		status = inspectRepositoryStatus("feature/status", repository, "master")
		if got := statusState(status); got != "detached" {
			t.Errorf("detached state = %q, want detached; status = %+v", got, status)
		}
	})

	t.Run("behind base", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "feature/status")
		runMainGit(t, repoPath, "checkout", "main")
		writeAndCommitMainFile(t, repoPath, "base.txt", "base\n", "base change")
		runMainGit(t, repoPath, "checkout", "feature/status")

		status := inspectRepositoryStatus("feature/status", state.Repository{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}, "master")
		if got := statusState(status); got != "behind-base" || status.baseBehind != 1 {
			t.Errorf("behind base status = %+v, state %q; want one behind and behind-base", status, got)
		}
	})

	t.Run("rebasing", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "feature/status")
		rebasePath := strings.TrimSpace(mainGitOutput(t, repoPath, "rev-parse", "--git-path", "rebase-merge"))
		if !filepath.IsAbs(rebasePath) {
			rebasePath = filepath.Join(repoPath, rebasePath)
		}
		if err := os.MkdirAll(rebasePath, 0o755); err != nil {
			t.Fatal(err)
		}

		status := inspectRepositoryStatus("feature/status", state.Repository{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}, "master")
		if got := statusState(status); got != "rebasing" {
			t.Errorf("rebase state = %q, want rebasing; status = %+v", got, status)
		}
	})

	t.Run("unpublished", func(t *testing.T) {
		_, remotePath := createRemoteMainRepository(t)
		repoPath := filepath.Join(t.TempDir(), "clone")
		runMainGit(t, t.TempDir(), "clone", "-q", remotePath, repoPath)
		runMainGit(t, repoPath, "checkout", "-qb", "feature/status", "origin/main")

		status := inspectRepositoryStatus("feature/status", state.Repository{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}, "master")
		if got := statusState(status); got != "unpushed" || formatPushStatus(status) != "unpublished" {
			t.Errorf("unpublished status = %+v, state %q, push %q", status, got, formatPushStatus(status))
		}
	})
}

func requireMainGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is not available")
	}
}

func createMainRepository(t *testing.T) string {
	t.Helper()
	configPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		writeMainConfig(t, "code", "copilot")
	} else if err != nil {
		t.Fatal(err)
	}
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
	runMainGit(t, remotePath, "symbolic-ref", "HEAD", "refs/heads/main")
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

func mainWorkspaceDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".autofeat-workspaces")
}

func writeMainConfig(t *testing.T, editorCmd, headlessCmd string) {
	t.Helper()
	writeMainConfigWithHooks(t, editorCmd, headlessCmd, []hooks.Definition{})
}

func writeMainConfigWithHooks(t *testing.T, editorCmd, headlessCmd string, definitions []hooks.Definition) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.Config{
		WorkspaceBaseDir: filepath.Join(home, ".autofeat-workspaces"),
		EditorCmd:        editorCmd,
		HeadlessCmd:      headlessCmd,
		Hooks:            definitions,
	}
	if err := config.SaveToPath(filepath.Join(home, ".autofeat", "config.json"), configuration); err != nil {
		t.Fatal(err)
	}
}

func writeMainDefaultConfig(t *testing.T) {
	t.Helper()
	configuration, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(configuration); err != nil {
		t.Fatal(err)
	}
}

func installMainMiseStub(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "mise.log")
	stubPath := filepath.Join(directory, "mise")
	contents := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$PWD\" \"$*\" >> \"$MISE_TEST_LOG\"\n"
	if err := os.WriteFile(stubPath, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISE_TEST_LOG", logPath)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func writeStubCommand(t *testing.T) (scriptPath, logPath string) {
	t.Helper()
	directory := t.TempDir()
	logPath = filepath.Join(directory, "invocation.log")
	scriptPath = filepath.Join(directory, "stub")
	contents := "#!/bin/sh\n{ pwd; printf '%s\\n' \"$@\"; } > \"" + logPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return scriptPath, logPath
}

func TestAddRepositoryCreatesSessionWorktreeAndWorkspace(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	firstRepo := createMainRepository(t)
	runMainGit(t, firstRepo, "branch", "-M", "main")
	t.Chdir(firstRepo)
	if err := addRepository("feature/potato"); err != nil {
		t.Fatalf("addRepository() error = %v", err)
	}

	session, err := state.GetSession("feature/potato")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Repos) != 1 {
		t.Fatalf("session repos = %+v, want one repository", session.Repos)
	}
	repository := session.Repos[0]
	if repository.BaseBranch != "main" || repository.IsRemoteClone {
		t.Errorf("repository = %+v, want main base local worktree", repository)
	}
	wantWorktree := filepath.Join(mainWorkspaceDir(t), "feature%2Fpotato", filepath.Base(firstRepo))
	if repository.WorktreePath != wantWorktree {
		t.Errorf("WorktreePath = %q, want %q", repository.WorktreePath, wantWorktree)
	}
	branch, err := gitcmd.CurrentBranch(repository.WorktreePath)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if branch != "feature/potato" {
		t.Errorf("worktree branch = %q, want feature/potato", branch)
	}
	if _, err := os.Stat(session.WorkspaceFile); err != nil {
		t.Errorf("workspace file missing: %v", err)
	}

	secondRepo := createMainRepository(t)
	runMainGit(t, secondRepo, "branch", "-M", "main")
	t.Chdir(secondRepo)
	if err := addRepository("feature/potato"); err != nil {
		t.Fatalf("addRepository() second repository error = %v", err)
	}
	session, err = state.GetSession("feature/potato")
	if err != nil {
		t.Fatalf("GetSession() after second add error = %v", err)
	}
	if len(session.Repos) != 2 {
		t.Fatalf("session repos after second add = %+v, want two repositories", session.Repos)
	}
	workspaceContents, err := os.ReadFile(session.WorkspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range session.Repos {
		if !strings.Contains(string(workspaceContents), repository.Name) {
			t.Errorf("workspace file does not reference %q:\n%s", repository.Name, workspaceContents)
		}
	}
}

func TestNewRefAcceptsAndRemembersAnyGitReference(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	runMainGit(t, repoPath, "checkout", "-qb", "develop")
	writeAndCommitMainFile(t, repoPath, "develop.txt", "develop\n", "develop commit")
	developCommit := strings.TrimSpace(mainGitOutput(t, repoPath, "rev-parse", "HEAD"))
	runMainGit(t, repoPath, "tag", "-am", "release", "v1.2.3")
	runMainGit(t, repoPath, "checkout", "-q", "main")
	mainCommit := strings.TrimSpace(mainGitOutput(t, repoPath, "rev-parse", "HEAD"))
	abbreviatedCommit := mainCommit[:12]
	t.Chdir(repoPath)

	if err := run([]string{"new", "feature/tag", "--ref", "v1.2.3"}); err != nil {
		t.Fatalf("run(new feature/tag --ref) error = %v", err)
	}
	tagSession, err := state.GetSession("feature/tag")
	if err != nil {
		t.Fatal(err)
	}
	if got := tagSession.Repos[0].BaseBranch; got != "refs/tags/v1.2.3" {
		t.Errorf("tag session base reference = %q, want refs/tags/v1.2.3", got)
	}
	if got := strings.TrimSpace(mainGitOutput(t, tagSession.Repos[0].WorktreePath, "rev-parse", "HEAD")); got != developCommit {
		t.Errorf("tag worktree HEAD = %q, want tagged commit %q", got, developCommit)
	}

	currentState, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := currentState.RepositoryBaseBranches[filepath.Clean(repoPath)]; got != "v1.2.3" {
		t.Errorf("remembered base reference = %q, want v1.2.3", got)
	}

	if err := run([]string{"new", "feature/remembered"}); err != nil {
		t.Fatalf("run(new feature/remembered) error = %v", err)
	}
	rememberedSession, err := state.GetSession("feature/remembered")
	if err != nil {
		t.Fatal(err)
	}
	if got := rememberedSession.Repos[0].BaseBranch; got != "refs/tags/v1.2.3" {
		t.Errorf("remembered session base reference = %q, want refs/tags/v1.2.3", got)
	}
	if got := strings.TrimSpace(mainGitOutput(t, rememberedSession.Repos[0].WorktreePath, "rev-parse", "HEAD")); got != developCommit {
		t.Errorf("remembered worktree HEAD = %q, want tagged commit %q", got, developCommit)
	}

	if err := run([]string{"new", "feature/commit", "--ref", abbreviatedCommit}); err != nil {
		t.Fatalf("run(new feature/commit --ref) error = %v", err)
	}
	commitSession, err := state.GetSession("feature/commit")
	if err != nil {
		t.Fatal(err)
	}
	if got := commitSession.Repos[0].BaseBranch; got != mainCommit {
		t.Errorf("commit session base reference = %q, want %q", got, mainCommit)
	}
	if got := strings.TrimSpace(mainGitOutput(t, commitSession.Repos[0].WorktreePath, "rev-parse", "HEAD")); got != mainCommit {
		t.Errorf("commit worktree HEAD = %q, want %q", got, mainCommit)
	}
	currentState, err = state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := currentState.RepositoryBaseBranches[filepath.Clean(repoPath)]; got != abbreviatedCommit {
		t.Errorf("updated remembered base reference = %q, want %q", got, abbreviatedCommit)
	}
}

func TestSaveTemplateFromSessionPreservesRepositorySources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	session := state.Session{Repos: []state.Repository{
		{Name: "api", OriginalPath: "/sources/api", WorktreePath: "/worktrees/api"},
		{Name: "web", OriginalPath: "git@example.com:team/web.git", WorktreePath: "/worktrees/web", IsRemoteClone: true},
	}}
	if err := state.SaveSession("feature/source", session); err != nil {
		t.Fatal(err)
	}
	if err := saveTemplateFromSession("full-stack", "feature/source"); err != nil {
		t.Fatalf("saveTemplateFromSession() error = %v", err)
	}

	got, err := templates.Get("full-stack")
	if err != nil {
		t.Fatal(err)
	}
	want := []templates.Repository{
		{Kind: templates.LocalRepository, Source: "/sources/api"},
		{Kind: templates.RemoteRepository, Source: "git@example.com:team/web.git"},
	}
	if !reflect.DeepEqual(got.Repositories, want) {
		t.Errorf("template repositories = %+v, want %+v", got.Repositories, want)
	}
}

func TestInstantiateTemplateCreatesOrderedLocalRepositories(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")

	repositories := []string{createMainRepository(t), createMainRepository(t)}
	entries := make([]templates.Repository, 0, len(repositories))
	for _, repositoryPath := range repositories {
		runMainGit(t, repositoryPath, "branch", "-M", "main")
		entries = append(entries, templates.Repository{Kind: templates.LocalRepository, Source: repositoryPath})
	}
	if err := templates.Put("full-stack", templates.Template{Repositories: entries}); err != nil {
		t.Fatal(err)
	}
	if err := instantiateTemplate("feature/template", "full-stack"); err != nil {
		t.Fatalf("instantiateTemplate() error = %v", err)
	}

	session, err := state.GetSession("feature/template")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Repos) != len(repositories) {
		t.Fatalf("session repositories = %+v, want %d", session.Repos, len(repositories))
	}
	for index, repository := range session.Repos {
		if repository.OriginalPath != repositories[index] {
			t.Errorf("repository %d source = %q, want %q", index, repository.OriginalPath, repositories[index])
		}
		branch, err := gitcmd.CurrentBranch(repository.WorktreePath)
		if err != nil {
			t.Fatal(err)
		}
		if branch != "feature/template" {
			t.Errorf("repository %d branch = %q, want feature/template", index, branch)
		}
	}
}

func TestInstantiateTemplateRollsBackEarlierRepositories(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")

	firstRepository := createMainRepository(t)
	secondRepository := createMainRepository(t)
	for _, repositoryPath := range []string{firstRepository, secondRepository} {
		runMainGit(t, repositoryPath, "branch", "-M", "main")
	}
	runMainGit(t, secondRepository, "branch", "feature/rollback")
	if err := templates.Put("rollback", templates.Template{Repositories: []templates.Repository{
		{Kind: templates.LocalRepository, Source: firstRepository},
		{Kind: templates.LocalRepository, Source: secondRepository},
	}}); err != nil {
		t.Fatal(err)
	}

	if err := instantiateTemplate("feature/rollback", "rollback"); err == nil {
		t.Fatal("instantiateTemplate() error = nil, want branch collision error")
	}
	if _, err := state.GetSession("feature/rollback"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() error = %v, want ErrSessionNotFound", err)
	}
	command := exec.Command("git", "-C", firstRepository, "show-ref", "--verify", "--quiet", "refs/heads/feature/rollback")
	if err := command.Run(); err == nil {
		t.Error("first repository feature branch remains after rollback")
	}
}

func TestAddRepositorySupportsRepositoriesWithSameName(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")

	repositoryRoot := t.TempDir()
	repositories := []string{
		filepath.Join(repositoryRoot, "first", "repository"),
		filepath.Join(repositoryRoot, "second", "repository"),
	}
	for _, repoPath := range repositories {
		if err := os.MkdirAll(repoPath, 0o755); err != nil {
			t.Fatal(err)
		}
		runMainGit(t, repoPath, "init", "-q")
		runMainGit(t, repoPath, "config", "user.email", "test@example.com")
		runMainGit(t, repoPath, "config", "user.name", "Test User")
		writeAndCommitMainFile(t, repoPath, "README.md", "initial\n", "initial commit")
		runMainGit(t, repoPath, "branch", "-M", "main")
		t.Chdir(repoPath)
		if err := addRepository("feature/same-name"); err != nil {
			t.Fatalf("addRepository(%q) error = %v", repoPath, err)
		}
	}

	session, err := state.GetSession("feature/same-name")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Repos) != 2 {
		t.Fatalf("session repos = %+v, want two repositories", session.Repos)
	}
	wantDirectories := []string{"repository", "repository-second"}
	for index, repository := range session.Repos {
		if repository.Name != "repository" {
			t.Errorf("repository name = %q, want repository", repository.Name)
		}
		if got := filepath.Base(repository.WorktreePath); got != wantDirectories[index] {
			t.Errorf("worktree directory = %q, want %q", got, wantDirectories[index])
		}
	}
	workspaceContents, err := os.ReadFile(session.WorkspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range wantDirectories {
		if !strings.Contains(string(workspaceContents), "./"+directory) {
			t.Errorf("workspace file does not reference %q:\n%s", directory, workspaceContents)
		}
	}
}

func TestAddRepositoryRunsPostAddHooksInWorktree(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	writeMainConfigWithHooks(t, "code", "copilot", []hooks.Definition{
		{When: hooks.PostAdd, Run: "printf 'first' > first-hook.txt"},
		{When: hooks.PostAdd, Run: "printf 'second' > second-hook.txt"},
	})
	t.Chdir(repoPath)

	if err := addRepository("feature/hooks"); err != nil {
		t.Fatalf("addRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/hooks")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"first-hook.txt": "first", "second-hook.txt": "second"} {
		contents, err := os.ReadFile(filepath.Join(session.Repos[0].WorktreePath, name))
		if err != nil {
			t.Errorf("read post-add output %q: %v", name, err)
			continue
		}
		if string(contents) != want {
			t.Errorf("post-add output %q = %q, want %q", name, contents, want)
		}
	}
}

func TestAddRepositoryCleansUpAfterPostAddHookFailure(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	writeMainConfigWithHooks(t, "code", "copilot", []hooks.Definition{{When: hooks.PostAdd, Run: "exit 23"}})
	t.Chdir(repoPath)

	if err := addRepository("feature/hook-failure"); err == nil {
		t.Fatal("addRepository() error = nil, want post-add hook error")
	}
	if _, err := state.GetSession("feature/hook-failure"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() after failed command error = %v, want ErrSessionNotFound", err)
	}
	worktreePath := filepath.Join(mainWorkspaceDir(t), "feature%2Fhook-failure", filepath.Base(repoPath))
	if _, err := os.Stat(worktreePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed worktree was not removed: Stat() error = %v", err)
	}
	if branches := mainGitOutput(t, repoPath, "branch", "--list", "feature/hook-failure"); strings.TrimSpace(branches) != "" {
		t.Errorf("failed feature branch still exists: %q", branches)
	}
}

func TestAddRepositoryRunsDefaultMiseHookForMiseFiles(t *testing.T) {
	requireMainGit(t)

	tests := []struct {
		name        string
		featureName string
		fileName    string
		wantRuns    bool
	}{
		{name: "mise.toml", featureName: "feature/mise", fileName: "mise.toml", wantRuns: true},
		{name: ".mise.toml", featureName: "feature/dot-mise", fileName: ".mise.toml", wantRuns: true},
		{name: "no mise file", featureName: "feature/no-mise", wantRuns: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			logPath := installMainMiseStub(t)
			repoPath := createMainRepository(t)
			runMainGit(t, repoPath, "branch", "-M", "main")
			if test.fileName != "" {
				writeAndCommitMainFile(t, repoPath, test.fileName, "[tools]\n", "add mise config")
			}
			writeMainDefaultConfig(t)
			t.Chdir(repoPath)

			if err := addRepository(test.featureName); err != nil {
				t.Fatalf("addRepository() error = %v", err)
			}
			session, err := state.GetSession(test.featureName)
			if err != nil {
				t.Fatal(err)
			}

			contents, err := os.ReadFile(logPath)
			if !test.wantRuns {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("mise log ReadFile() error = %v, want file not to exist", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			worktreePath := session.Repos[0].WorktreePath
			want := worktreePath + "\ttrust\n" + worktreePath + "\tinstall\n"
			if string(contents) != want {
				t.Errorf("mise invocations = %q, want %q", contents, want)
			}
		})
	}
}

func TestAddRemoteRepositoryClonesAndRecordsSession(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	_, remotePath := createRemoteMainRepository(t)
	if err := addRemoteRepository("feature/remote", remotePath); err != nil {
		t.Fatalf("addRemoteRepository() error = %v", err)
	}

	session, err := state.GetSession("feature/remote")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Repos) != 1 {
		t.Fatalf("session repos = %+v, want one repository", session.Repos)
	}
	repository := session.Repos[0]
	if !repository.IsRemoteClone || repository.Name != "remote" || repository.BaseBranch != "main" {
		t.Errorf("repository = %+v, want remote clone named remote on main", repository)
	}
	branch, err := gitcmd.CurrentBranch(repository.WorktreePath)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if branch != "feature/remote" {
		t.Errorf("clone branch = %q, want feature/remote", branch)
	}
}

func TestAddRemoteRepositoryRunsPostAddHooksInClone(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	_, remotePath := createRemoteMainRepository(t)
	writeMainConfigWithHooks(t, "code", "copilot", []hooks.Definition{
		{When: hooks.PostAdd, Run: "test \"$(git branch --show-current)\" = feature/remote-hook && printf 'remote' > remote-hook.txt"},
	})
	if err := addRemoteRepository("feature/remote-hook", remotePath); err != nil {
		t.Fatalf("addRemoteRepository() error = %v", err)
	}

	session, err := state.GetSession("feature/remote-hook")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(session.Repos[0].WorktreePath, "remote-hook.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "remote" {
		t.Errorf("remote post-add output = %q, want remote", contents)
	}
}

func TestAddRemoteRepositoryRunsDefaultMiseHookInClone(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	logPath := installMainMiseStub(t)

	sourcePath, remotePath := createRemoteMainRepository(t)
	writeAndCommitMainFile(t, sourcePath, "mise.toml", "[tools]\n", "add mise config")
	runMainGit(t, sourcePath, "push", "-q", "origin", "main")
	writeMainDefaultConfig(t)

	if err := addRemoteRepository("feature/remote-mise", remotePath); err != nil {
		t.Fatalf("addRemoteRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/remote-mise")
	if err != nil {
		t.Fatal(err)
	}
	worktreePath := session.Repos[0].WorktreePath
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := worktreePath + "\ttrust\n" + worktreePath + "\tinstall\n"
	if string(contents) != want {
		t.Errorf("mise invocations = %q, want %q", contents, want)
	}
}

func TestAddRemoteRepositoryRemovesCloneOnFailure(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath := createMainRepository(t)
	runMainGit(t, sourcePath, "branch", "-M", "trunk")
	remotePath := filepath.Join(t.TempDir(), "trunk-only.git")
	runMainGit(t, sourcePath, "init", "--bare", "-q", remotePath)
	runMainGit(t, sourcePath, "remote", "add", "origin", remotePath)
	runMainGit(t, sourcePath, "push", "-qu", "origin", "trunk")

	if err := addRemoteRepository("feature/remote", remotePath); err == nil {
		t.Fatal("addRemoteRepository() error = nil, want base resolution error")
	}
	worktreePath := filepath.Join(mainWorkspaceDir(t), "feature%2Fremote", "trunk-only")
	if _, err := os.Stat(worktreePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed clone was not removed: Stat() error = %v", err)
	}
	if _, err := state.GetSession("feature/remote"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() after failed clone error = %v, want ErrSessionNotFound", err)
	}
}

func TestTeardownSessionRemovesWorktreesAndState(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	t.Chdir(repoPath)
	if err := addRepository("feature/clean"); err != nil {
		t.Fatalf("addRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/clean")
	if err != nil {
		t.Fatal(err)
	}

	if err := teardownSession("feature/clean", false); err != nil {
		t.Fatalf("teardownSession() error = %v", err)
	}
	if _, err := os.Stat(session.FeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists: Stat() error = %v", err)
	}
	if _, err := state.GetSession("feature/clean"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() after teardown error = %v, want ErrSessionNotFound", err)
	}
}

func TestTeardownSessionRunsPostTeardownHooksAfterRemoval(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	logPath := filepath.Join(t.TempDir(), "post-teardown.log")
	t.Setenv("POST_TEARDOWN_LOG", logPath)
	statePath, err := state.Path()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOFEAT_TEST_STATE", statePath)
	writeMainConfigWithHooks(t, "code", "copilot", []hooks.Definition{{
		When: hooks.PostTeardown,
		Run:  `test ! -e feature%2Fhooks && ! grep -q 'feature/hooks' "$AUTOFEAT_TEST_STATE" && pwd > "$POST_TEARDOWN_LOG"`,
	}})
	t.Chdir(repoPath)

	if err := addRepository("feature/hooks"); err != nil {
		t.Fatalf("addRepository() error = %v", err)
	}
	if err := teardownSession("feature/hooks", false); err != nil {
		t.Fatalf("teardownSession() error = %v", err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := mainWorkspaceDir(t) + "\n"
	if string(contents) != want {
		t.Errorf("post-teardown working directory = %q, want %q", contents, want)
	}
}

func TestTeardownSessionDirtyWorktree(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	t.Chdir(repoPath)
	if err := addRepository("feature/dirty"); err != nil {
		t.Fatalf("addRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/dirty")
	if err != nil {
		t.Fatal(err)
	}
	worktreePath := session.Repos[0].WorktreePath
	if err := os.WriteFile(filepath.Join(worktreePath, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := teardownSession("feature/dirty", false); err == nil {
		t.Fatal("teardownSession() error = nil, want dirty worktree error")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("dirty worktree was removed without --force: %v", err)
	}

	if err := teardownSession("feature/dirty", true); err != nil {
		t.Fatalf("teardownSession(force) error = %v", err)
	}
	if _, err := os.Stat(session.FeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists after forced teardown: Stat() error = %v", err)
	}
	if _, err := state.GetSession("feature/dirty"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() after forced teardown error = %v, want ErrSessionNotFound", err)
	}
}

func TestTeardownSessionRemovesRemoteClone(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	_, remotePath := createRemoteMainRepository(t)
	if err := addRemoteRepository("feature/remote", remotePath); err != nil {
		t.Fatalf("addRemoteRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/remote")
	if err != nil {
		t.Fatal(err)
	}
	worktreePath := session.Repos[0].WorktreePath
	runMainGit(t, worktreePath, "push", "-qu", "origin", "feature/remote")
	runMainGit(t, worktreePath, "config", "user.email", "test@example.com")
	runMainGit(t, worktreePath, "config", "user.name", "Test User")
	writeAndCommitMainFile(t, worktreePath, "feature.txt", "feature\n", "unpushed feature change")

	if err := teardownSession("feature/remote", false); err != nil {
		t.Fatalf("teardownSession() error = %v", err)
	}
	if _, err := os.Stat(session.FeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists: Stat() error = %v", err)
	}
	if _, err := state.GetSession("feature/remote"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() after teardown error = %v, want ErrSessionNotFound", err)
	}
}

func TestTeardownSessionRemovesRemoteCloneWithUnpublishedBranch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	_, remotePath := createRemoteMainRepository(t)
	if err := addRemoteRepository("feature/unpublished", remotePath); err != nil {
		t.Fatalf("addRemoteRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/unpublished")
	if err != nil {
		t.Fatal(err)
	}

	if err := teardownSession("feature/unpublished", false); err != nil {
		t.Fatalf("teardownSession() unpublished branch error = %v", err)
	}
	if _, err := os.Stat(session.FeatureDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("feature directory still exists: Stat() error = %v", err)
	}
	if _, err := state.GetSession("feature/unpublished"); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("GetSession() after teardown error = %v, want ErrSessionNotFound", err)
	}
}

func TestEnsureRepositoryAdded(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	t.Chdir(repoPath)

	if err := ensureRepositoryAdded("feature/ensure"); err != nil {
		t.Fatalf("ensureRepositoryAdded() error = %v", err)
	}
	session, err := state.GetSession("feature/ensure")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Repos) != 1 {
		t.Fatalf("session repos = %+v, want one repository", session.Repos)
	}

	if err := ensureRepositoryAdded("feature/ensure"); err != nil {
		t.Fatalf("ensureRepositoryAdded() second call error = %v", err)
	}
	session, err = state.GetSession("feature/ensure")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Repos) != 1 {
		t.Errorf("session repos after repeated ensure = %+v, want one repository", session.Repos)
	}
}

func TestOpenSessionInvokesEditorWithWorkspaceFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	scriptPath, logPath := writeStubCommand(t)
	writeMainConfig(t, scriptPath, "copilot")
	workspaceFile := filepath.Join(t.TempDir(), "feature.code-workspace")
	if err := state.SaveSession("feature/open", state.Session{WorkspaceFile: workspaceFile}); err != nil {
		t.Fatal(err)
	}

	if err := openSession("feature/open"); err != nil {
		t.Fatalf("openSession() error = %v", err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), workspaceFile) {
		t.Errorf("editor invocation = %q, want workspace file %q", contents, workspaceFile)
	}
}

func TestOpenConfigInvokesEditorWithConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	scriptPath, logPath := writeStubCommand(t)
	configPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	legacyConfig := fmt.Sprintf(`{"workspace_base_dir":"/tmp/workspaces","editor_cmd":%q,"headless_cmd":"copilot"}`, scriptPath)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := openConfig(); err != nil {
		t.Fatalf("openConfig() error = %v", err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), configPath) {
		t.Errorf("editor invocation = %q, want config file %q", contents, configPath)
	}
	configuration, err := config.LoadFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Hooks) == 0 {
		t.Errorf("persisted hooks = %#v, want defaults", configuration.Hooks)
	}
	if configuration.SchemaVersion != 1 {
		t.Errorf("persisted schema version = %d, want 1", configuration.SchemaVersion)
	}
}

func TestOpenCopilotSessionStartsAgentInFeatureDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	scriptPath, logPath := writeStubCommand(t)
	writeMainConfig(t, "code", scriptPath)
	featureDir := t.TempDir()
	if err := state.SaveSession("feature/copilot", state.Session{FeatureDir: featureDir}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	if err := openCopilotSession("feature/copilot"); err != nil {
		t.Fatalf("openCopilotSession() error = %v", err)
	}

	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(contents)), featureDir; got != want {
		t.Errorf("Copilot invocation = %q, want cwd %q and no arguments", got, want)
	}
}

func TestRunFeatureStartsHeadlessAgentInFeatureDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	scriptPath, logPath := writeStubCommand(t)
	writeMainConfig(t, "code", scriptPath)
	featureDir := t.TempDir()
	if err := state.SaveSession("feature/agent", state.Session{FeatureDir: featureDir}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	if err := runFeature("feature/agent", "write tests"); err != nil {
		t.Fatalf("runFeature() error = %v", err)
	}

	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 3 || lines[1] != "-i" || lines[2] != headlessPrompt {
		t.Errorf("headless invocation = %q, want [cwd -i prompt]", lines)
	}
	if resolved, err := filepath.EvalSymlinks(featureDir); err != nil || lines[0] != resolved {
		t.Errorf("headless cwd = %q, want feature directory %q", lines[0], resolved)
	}
	task, err := os.ReadFile(filepath.Join(featureDir, "TASK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(task) != "write tests\n" {
		t.Errorf("TASK.md = %q, want appended task", task)
	}
}

func TestRunHeadlessMissingCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	writeMainConfig(t, "code", filepath.Join(t.TempDir(), "does-not-exist"))
	if err := state.SaveSession("feature/agent", state.Session{FeatureDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	if err := runHeadless("feature/agent", ""); err == nil {
		t.Fatal("runHeadless() error = nil, want missing command error")
	}
}

func TestListSessionsToRendersDrift(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	runMainGit(t, repoPath, "checkout", "-qb", "feature/list")
	runMainGit(t, repoPath, "checkout", "main")
	writeAndCommitMainFile(t, repoPath, "base.txt", "base\n", "base change")
	runMainGit(t, repoPath, "checkout", "feature/list")

	if err := state.SaveSession("feature/list", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSession("feature/current", state.Session{}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := listSessionsTo(&output); err != nil {
		t.Fatalf("listSessionsTo() error = %v", err)
	}
	report := output.String()
	for _, want := range []string{"FEATURE", "CREATED", "REPOSITORIES", "DRIFT", "feature/list", "1 behind", "feature/current", "current"} {
		if !strings.Contains(report, want) {
			t.Errorf("list report does not contain %q:\n%s", want, report)
		}
	}
	if strings.Index(report, "feature/current") > strings.Index(report, "feature/list") {
		t.Errorf("list report is not sorted by feature name:\n%s", report)
	}
}

func TestDetectBaseBranchFallsBackToStateDefault(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "trunk")

	if err := state.Save(state.State{DefaultBaseBranch: "develop"}); err != nil {
		t.Fatal(err)
	}
	got, err := detectBaseBranch(repoPath)
	if err != nil {
		t.Fatalf("detectBaseBranch() error = %v", err)
	}
	if got != "develop" {
		t.Errorf("detectBaseBranch() = %q, want state default develop", got)
	}

	runMainGit(t, repoPath, "branch", "main", "trunk")
	got, err = detectBaseBranch(repoPath)
	if err != nil {
		t.Fatalf("detectBaseBranch() with main error = %v", err)
	}
	if got != "main" {
		t.Errorf("detectBaseBranch() = %q, want detected main", got)
	}
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

func TestRepositoryDirectoryName(t *testing.T) {
	t.Parallel()

	repositories := []state.Repository{
		{WorktreePath: "/workspaces/feature/repository"},
		{WorktreePath: "/workspaces/feature/repository-parent"},
	}
	tests := []struct {
		name         string
		repositories []state.Repository
		want         string
	}{
		{name: "repository name available", want: "repository"},
		{name: "parent name preferred", repositories: repositories[:1], want: "repository-parent"},
		{name: "numeric fallback", repositories: repositories, want: "repository-2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := repositoryDirectoryName("repository", "parent", test.repositories); got != test.want {
				t.Errorf("repositoryDirectoryName() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRepositoryParentName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/sources/example/repository":               "example",
		"https://github.com/example/repository.git": "example",
		"git@github.com:example/repository.git":     "example",
	}
	for repositoryPath, want := range tests {
		if got := repositoryParentName(repositoryPath); got != want {
			t.Errorf("repositoryParentName(%q) = %q, want %q", repositoryPath, got, want)
		}
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
