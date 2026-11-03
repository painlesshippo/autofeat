package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/templates"
)

func TestRootAndVersionCommand(t *testing.T) {
	if err := run(nil); err == nil {
		t.Error("run(nil) error = nil, want usage error")
	}

	var output bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatalf("version error = %v", err)
	}
	for _, want := range []string{"autofeat ", "commit: ", "built: ", "go: "} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("version output missing %q:\n%s", want, output.String())
		}
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

func TestCompletionCommandArguments(t *testing.T) {
	for _, args := range [][]string{{"completion"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "bash", "extra"}, {"completion", "powershell", "extra"}} {
		if err := run(args); err == nil {
			t.Errorf("run(%q) error = nil, want usage error", args)
		}
	}
}

func TestTemplateAndNewCommandArguments(t *testing.T) {
	invalid := [][]string{
		{"template"},
		{"template", "unknown"},
		{"template", "show"},
		{"template", "save", "full-stack", "feature/source"},
		{"new", "feature/test", "https://example.com/repository.git", "--template", "full-stack"},
		{"new", "feature/test", "/tmp/repository", "--template", "full-stack"},
		{"new", "feature/test", "/tmp/repository", "--azure-build", "42"},
		{"new", "feature/test", "/tmp/repository", "--template="},
		{"new", "feature/test", "/tmp/repository", "--azure-build="},
		{"new", "feature/test", ""},
		{"new", "feature/test", "/tmp/repository", "extra"},
		{"new", "feature/test", "--local", "/tmp/repository"},
		{"new", "feature/test", "--remote", "https://example.com/repository.git"},
		{"new", "feature/test", "--template", "full-stack", "--azure-build", "42"},
		{"new", "feature/test", "--template"},
		{"new", "feature/test", "--unknown", "full-stack"},
		{"new", "feature/test", "--template", "full-stack", "--ref", "develop"},
		{"new", "feature/test", "--azure-org", "contoso"},
		{"new", "feature/test", "--azure-project", "Widgets"},
		{"new", "feature/test", "--azure-url", "https://dev.azure.com"},
	}
	for _, args := range invalid {
		if err := run(args); err == nil {
			t.Errorf("run(%q) error = nil, want usage error", args)
		}
	}
}

func TestTemplateListShowAndSaveCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := state.SaveSession("feature/source", state.Session{Repos: []state.Repository{
		{Name: "api", OriginalPath: "/sources/api", WorktreePath: "/worktrees/api"},
		{Name: "web", OriginalPath: "https://example.com/web.git", WorktreePath: "/worktrees/web", IsRemoteClone: true},
	}}); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"template", "save", "full-stack", "--from", "feature/source"}); err != nil {
		t.Fatalf("run(template save) error = %v", err)
	}
	if err := templates.Put("backend", templates.Template{Repositories: []templates.Repository{
		{Kind: templates.LocalRepository, Source: "/sources/api"},
	}}); err != nil {
		t.Fatal(err)
	}

	var listOutput bytes.Buffer
	command := newRootCommand()
	command.SetOut(&listOutput)
	command.SetArgs([]string{"template", "list"})
	if err := command.Execute(); err != nil {
		t.Fatalf("template list error = %v", err)
	}
	for _, want := range []string{"NAME", "REPOSITORIES", "backend", "1", "full-stack", "2"} {
		if !strings.Contains(listOutput.String(), want) {
			t.Errorf("template list output missing %q:\n%s", want, listOutput.String())
		}
	}
	if strings.Index(listOutput.String(), "backend") > strings.Index(listOutput.String(), "full-stack") {
		t.Errorf("template list not sorted lexically:\n%s", listOutput.String())
	}

	var showOutput bytes.Buffer
	command = newRootCommand()
	command.SetOut(&showOutput)
	command.SetArgs([]string{"template", "show", "full-stack"})
	if err := command.Execute(); err != nil {
		t.Fatalf("template show error = %v", err)
	}
	for _, want := range []string{"KIND", "SOURCE", "local", "/sources/api", "remote", "https://example.com/web.git"} {
		if !strings.Contains(showOutput.String(), want) {
			t.Errorf("template show output missing %q:\n%s", want, showOutput.String())
		}
	}

	if err := run([]string{"template", "show", "missing"}); !errors.Is(err, templates.ErrTemplateNotFound) {
		t.Errorf("run(template show missing) error = %v, want ErrTemplateNotFound", err)
	}
	if err := run([]string{"template", "save", "broken", "--from", "feature/missing"}); !errors.Is(err, state.ErrSessionNotFound) {
		t.Errorf("run(template save --from missing) error = %v, want ErrSessionNotFound", err)
	}
}

func TestListCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := state.SaveSession("feature/listed", state.Session{}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"list"})
	if err := command.Execute(); err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !strings.Contains(output.String(), "feature/listed") {
		t.Errorf("list output missing session:\n%s", output.String())
	}

	if err := run([]string{"list", "extra"}); err == nil {
		t.Error("run(list extra) error = nil, want argument error")
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
	if !slices.Equal(gotSelectors, []string{"*"}) {
		t.Errorf("run(status) selectors = %q, want [*]", gotSelectors)
	}

	if err := run([]string{"status", "feature/a", "feature/b"}); err != nil {
		t.Fatalf("run(status feature/a feature/b) error = %v", err)
	}
	if !slices.Equal(gotSelectors, []string{"feature/a", "feature/b"}) {
		t.Errorf("run(status feature/a feature/b) selectors = %q, want exact selectors", gotSelectors)
	}

	// Shell-expanded wildcard: two or more arguments that exist on disk without '*'.
	dir := t.TempDir()
	firstFile := filepath.Join(dir, "a.txt")
	secondFile := filepath.Join(dir, "b.txt")
	for _, path := range []string{firstFile, secondFile} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"status", firstFile, secondFile}); err != nil {
		t.Fatalf("run(status expanded files) error = %v", err)
	}
	if !slices.Equal(gotSelectors, []string{"*"}) {
		t.Errorf("run(status expanded files) selectors = %q, want [*]", gotSelectors)
	}

	if err := run([]string{"status", "--json"}); err == nil {
		t.Error("run(status --json) error = nil, want usage error")
	}
}

func TestRemoveCommandDispatch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	repositoryPath := createMainRepository(t)
	nestedPath := filepath.Join(repositoryPath, "nested")
	if err := os.Mkdir(nestedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	unattachedPath := createMainRepository(t)
	if err := state.SaveSession("feature/remove", state.Session{Repos: []state.Repository{{
		Name:         filepath.Base(repositoryPath),
		OriginalPath: repositoryPath,
		WorktreePath: filepath.Join(t.TempDir(), filepath.Base(repositoryPath)),
	}}}); err != nil {
		t.Fatal(err)
	}

	originalRemoveRepositoryCommand := removeRepositoryCommand
	originalRemoveRepositoryByNameCommand := removeRepositoryByNameCommand
	t.Cleanup(func() {
		removeRepositoryCommand = originalRemoveRepositoryCommand
		removeRepositoryByNameCommand = originalRemoveRepositoryByNameCommand
	})

	var gotFeatureName string
	var gotRepositorySource string
	var gotRemote bool
	var gotForce bool
	var gotName string
	reset := func() {
		gotFeatureName, gotRepositorySource, gotName = "", "", ""
		gotRemote, gotForce = false, false
	}
	removeRepositoryCommand = func(featureName, repositorySource string, remote, force bool) error {
		gotFeatureName = featureName
		gotRepositorySource = repositorySource
		gotRemote = remote
		gotForce = force
		return nil
	}
	removeRepositoryByNameCommand = func(featureName, name string, force bool) error {
		gotFeatureName = featureName
		gotName = name
		gotForce = force
		return nil
	}

	if err := run([]string{"remove", "feature/remove", nestedPath, "--force"}); err != nil {
		t.Fatalf("run(remove PATH) error = %v", err)
	}
	if gotFeatureName != "feature/remove" || gotRepositorySource != repositoryPath || gotRemote || !gotForce || gotName != "" {
		t.Errorf("local remove dispatch = (%q, %q, %t, %t), want feature, repository root, local, force", gotFeatureName, gotRepositorySource, gotRemote, gotForce)
	}

	reset()
	if err := run([]string{"remove", "feature/remove", "https://example.com/repository.git/"}); err != nil {
		t.Fatalf("run(remove URL) error = %v", err)
	}
	if gotRepositorySource != "https://example.com/repository.git" || !gotRemote || gotForce || gotName != "" {
		t.Errorf("remote remove dispatch = (%q, %t, %t), want normalized remote without force", gotRepositorySource, gotRemote, gotForce)
	}

	reset()
	if err := run([]string{"remove", "feature/remove", unattachedPath}); err != nil {
		t.Fatalf("run(remove unattached PATH) error = %v", err)
	}
	if gotName != unattachedPath || gotRepositorySource != "" {
		t.Errorf("unattached path dispatch = (name %q, source %q), want name fallback", gotName, gotRepositorySource)
	}

	reset()
	if err := run([]string{"remove", "feature/named", " repo1"}); err != nil {
		t.Fatalf("run(remove ' repo1') error = %v", err)
	}
	if gotName != " repo1" {
		t.Errorf("named remove dispatch selected %q, want literal %q", gotName, " repo1")
	}

	reset()
	if err := run([]string{"remove", "feature/named", "repo1", "--force"}); err != nil {
		t.Fatalf("run(remove REPO) error = %v", err)
	}
	if gotFeatureName != "feature/named" || gotName != "repo1" || !gotForce {
		t.Errorf("named remove dispatch = (%q, %q, %t), want feature, repo1, force", gotFeatureName, gotName, gotForce)
	}
}

func TestRemoveCommandArguments(t *testing.T) {
	invalid := [][]string{
		{"remove"},
		{"remove", "feature/remove"},
		{"remove", "feature/remove", ""},
		{"remove", "feature/remove", "--local", "."},
		{"remove", "feature/remove", "--remote", "https://example.com/repository.git"},
		{"remove", "feature/remove", "repo1", "repo2"},
	}
	for _, args := range invalid {
		if err := run(args); err == nil {
			t.Errorf("run(%q) error = nil, want usage error", args)
		}
	}
}

func TestOpenCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/a", "feature/b"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}
	originalOpenFeatureCommand := openFeatureCommand
	originalOpenCopilotCommand := openCopilotCommand
	originalOpenDevcontainerCommand := openDevcontainerCommand
	t.Cleanup(func() {
		openFeatureCommand = originalOpenFeatureCommand
		openCopilotCommand = originalOpenCopilotCommand
		openDevcontainerCommand = originalOpenDevcontainerCommand
	})

	var openedMode string
	var openedFeatures []string
	var configHints []string
	reset := func() {
		openedMode = ""
		openedFeatures = nil
		configHints = nil
	}
	openFeatureCommand = func(featureName string) error {
		openedMode = "editor"
		openedFeatures = append(openedFeatures, featureName)
		return nil
	}
	openCopilotCommand = func(featureName string) error {
		openedMode = "copilot"
		openedFeatures = append(openedFeatures, featureName)
		return nil
	}
	openDevcontainerCommand = func(featureName, configHint string) error {
		openedMode = "devcontainer"
		openedFeatures = append(openedFeatures, featureName)
		configHints = append(configHints, configHint)
		return nil
	}

	if err := run([]string{"open", "feature/a"}); err != nil {
		t.Fatalf("run(open feature/a) error = %v", err)
	}
	if openedMode != "editor" || !slices.Equal(openedFeatures, []string{"feature/a"}) {
		t.Errorf("default open = (%q, %q), want editor for feature/a", openedMode, openedFeatures)
	}

	reset()
	if err := run([]string{"open", "feature/a", "--copilot"}); err != nil {
		t.Fatalf("run(open --copilot) error = %v", err)
	}
	if openedMode != "copilot" || !slices.Equal(openedFeatures, []string{"feature/a"}) {
		t.Errorf("copilot open = (%q, %q), want copilot for feature/a", openedMode, openedFeatures)
	}

	reset()
	if err := run([]string{"open", "feature/*", "--devcontainer", "/path/to/devcontainer.json"}); err != nil {
		t.Fatalf("run(open feature/* --devcontainer PATH) error = %v", err)
	}
	if openedMode != "devcontainer" || !slices.Equal(openedFeatures, []string{"feature/a", "feature/b"}) || !slices.Equal(configHints, []string{"/path/to/devcontainer.json", "/path/to/devcontainer.json"}) {
		t.Errorf("devcontainer open = (%q, %q, %q), want both features with supplied path", openedMode, openedFeatures, configHints)
	}

	for _, args := range [][]string{
		{"open"},
		{"open", "feature/a", "--devcontainer", "/x", "--copilot"},
		{"open", "feature/a", "--devcontainer="},
	} {
		reset()
		if err := run(args); err == nil {
			t.Errorf("run(%q) error = nil, want error", args)
		}
		if openedMode != "" {
			t.Errorf("run(%q) unexpectedly launched mode %q", args, openedMode)
		}
	}
}

func TestSyncCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/one", "feature/two"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}
	originalSyncFeatureCommand := syncFeatureCommand
	originalCancelSyncCommand := cancelSyncCommand
	t.Cleanup(func() {
		syncFeatureCommand = originalSyncFeatureCommand
		cancelSyncCommand = originalCancelSyncCommand
	})

	var syncedFeatureNames []string
	var gotKeepConflicts []bool
	var cancelledFeatureNames []string
	syncFeatureCommand = func(featureName string, keepConflicts bool) error {
		syncedFeatureNames = append(syncedFeatureNames, featureName)
		gotKeepConflicts = append(gotKeepConflicts, keepConflicts)
		if keepConflicts && featureName == "feature/one" {
			return errors.New("conflict")
		}
		return nil
	}
	cancelSyncCommand = func(featureName string) error {
		cancelledFeatureNames = append(cancelledFeatureNames, featureName)
		return nil
	}

	if err := run([]string{"sync", "feature/one"}); err != nil {
		t.Fatalf("run(sync feature/one) error = %v", err)
	}
	if !slices.Equal(syncedFeatureNames, []string{"feature/one"}) || len(cancelledFeatureNames) != 0 {
		t.Errorf("sync single = (synced %q, cancelled %q), want [feature/one] and []", syncedFeatureNames, cancelledFeatureNames)
	}

	syncedFeatureNames, gotKeepConflicts = nil, nil
	err := run([]string{"sync", "feature/*", "--keep-conflicts"})
	if err == nil || !strings.Contains(err.Error(), `"feature/one"`) {
		t.Errorf("run(sync feature/* --keep-conflicts) error = %v, want failure naming feature/one", err)
	}
	if !slices.Equal(syncedFeatureNames, []string{"feature/one", "feature/two"}) || !slices.Equal(gotKeepConflicts, []bool{true, true}) {
		t.Errorf("sync multi = (%q, %v), want both features with keepConflicts=true", syncedFeatureNames, gotKeepConflicts)
	}

	syncedFeatureNames = nil
	if err := run([]string{"sync", "feature/*", "--cancel"}); err != nil {
		t.Fatalf("run(sync feature/* --cancel) error = %v", err)
	}
	if !slices.Equal(cancelledFeatureNames, []string{"feature/one", "feature/two"}) || len(syncedFeatureNames) != 0 {
		t.Errorf("sync --cancel = (cancelled %q, synced %q), want both cancelled and none synced", cancelledFeatureNames, syncedFeatureNames)
	}

	if err := run([]string{"sync", "feature/two", "--cancel", "--keep-conflicts"}); err == nil {
		t.Error("run(sync --cancel --keep-conflicts) error = nil, want mutually exclusive flags error")
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
	if !slices.Equal(gotFeatureNames, []string{"feature/x", "feature/y"}) || !gotForce {
		t.Errorf("teardown dispatch = (%q, %t), want sorted features and force", gotFeatureNames, gotForce)
	}
	if err := run([]string{"feature/x", "teardown"}); err == nil {
		t.Error("run(feature/x teardown) error = nil, want command-first usage error")
	}
}
