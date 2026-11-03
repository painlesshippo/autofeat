package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/templates"
	"github.com/spf13/cobra"
)

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

	gotAll, directive := completeFeatureName(nil, []string{"ignored"}, "feature/")
	if !reflect.DeepEqual(gotAll, want) || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeFeatureName() = (%q, %v), want (%q, NoFileComp)", gotAll, directive, want)
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
	command.SetArgs([]string{"__complete", "new", "feature/"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Cobra new feature completion error = %v", err)
	}
	if got, want := output.String(), "feature/team/nested\nfeature/zulu\n:4\n"; got != want {
		t.Errorf("Cobra new feature completion = %q, want %q", got, want)
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

	output.Reset()
	errorOutput.Reset()
	command = newRootCommand()
	command.SetOut(&output)
	command.SetErr(&errorOutput)
	command.SetArgs([]string{"__complete", "remove", "feature/"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Cobra remove feature completion error = %v", err)
	}
	if got, want := output.String(), "feature/team/nested\nfeature/zulu\n:4\n"; got != want {
		t.Errorf("Cobra remove feature completion = %q, want %q", got, want)
	}

	output.Reset()
	errorOutput.Reset()
	command = newRootCommand()
	command.SetOut(&output)
	command.SetErr(&errorOutput)
	command.SetArgs([]string{"__complete", "remove", "feature/zulu", "-"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Cobra remove flag completion error = %v", err)
	}
	for _, flag := range []string{"--local", "--remote"} {
		if strings.Contains(output.String(), flag) {
			t.Errorf("Cobra remove completion offered removed flag %s:\n%s", flag, output.String())
		}
	}

	output.Reset()
	errorOutput.Reset()
	command = newRootCommand()
	command.SetOut(&output)
	command.SetErr(&errorOutput)
	command.SetArgs([]string{"__complete", "new", "feature/new", ""})
	if err := command.Execute(); err != nil {
		t.Fatalf("Cobra new source completion error = %v", err)
	}
	if got, want := output.String(), fmt.Sprintf(":%d\n", cobra.ShellCompDirectiveFilterDirs); got != want {
		t.Errorf("Cobra new source completion = %q, want %q", got, want)
	}

	_, directive := completeDirectories(nil, nil, "")
	if directive != cobra.ShellCompDirectiveFilterDirs {
		t.Errorf("completeDirectories() directive = %v, want FilterDirs", directive)
	}
	if got, directive := completeFilePaths(nil, nil, ""); got != nil || directive != cobra.ShellCompDirectiveDefault {
		t.Errorf("completeFilePaths() = (%v, %v), want (nil, Default)", got, directive)
	}
	if got, directive := completeNewFeatureArguments(nil, []string{"feature/new", "/tmp/repo"}, ""); got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeNewFeatureArguments(2 args) = (%v, %v), want (nil, NoFileComp)", got, directive)
	}

	for _, args := range [][]string{
		{"__complete", "list", ""},
		{"__complete", "dash", ""},
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
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
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

	if got, directive := completeTemplateNames(nil, []string{"already-selected"}, ""); got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeTemplateNames(1 arg) = (%v, %v), want (nil, NoFileComp)", got, directive)
	}

	// Corrupt templates.json should fail silently with NoFileComp.
	if err := os.WriteFile(filepath.Join(homeDir, ".autofeat", "templates.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, directive := completeTemplateNames(nil, nil, ""); got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeTemplateNames(corrupt) = (%v, %v), want (nil, NoFileComp)", got, directive)
	}
}

func TestRemoveCompletesSessionRepositories(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	localPath := createMainRepository(t)
	runMainGit(t, localPath, "branch", "-M", "main")
	if _, err := addRepositoryAt("feature/complete-remove", localPath); err != nil {
		t.Fatalf("addRepositoryAt() error = %v", err)
	}
	_, remotePath := createRemoteMainRepository(t)
	if err := addRemoteRepository("feature/complete-remove", remotePath); err != nil {
		t.Fatalf("addRemoteRepository() error = %v", err)
	}
	session, err := state.GetSession("feature/complete-remove")
	if err != nil {
		t.Fatal(err)
	}

	command := newRemoveCommand()
	got, directive := completeSessionRepositories(command, []string{"feature/complete-remove"}, "")
	want := []string{
		filepath.Base(session.Repos[0].WorktreePath),
		filepath.Base(session.Repos[1].WorktreePath),
		localPath,
		remotePath,
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("repository completion = %q, want %q", got, want)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("repository completion directive = %v, want NoFileComp", directive)
	}
	if got, _ := completeSessionRepositories(command, []string{"feature/complete-remove"}, localPath); !reflect.DeepEqual(got, []string{localPath}) {
		t.Errorf("local path completion = %q, want %q", got, []string{localPath})
	}
	if got, directive := completeSessionRepositories(command, []string{"feature/complete-remove", "extra"}, ""); got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeSessionRepositories(2 args) = (%v, %v), want (nil, NoFileComp)", got, directive)
	}
	if got, directive := completeSessionRepositories(command, []string{"feature/missing"}, ""); got != nil || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeSessionRepositories(missing) = (%v, %v), want (nil, NoFileComp)", got, directive)
	}
}
