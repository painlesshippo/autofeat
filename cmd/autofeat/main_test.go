package main

import (
	"os"
	"path/filepath"
	"testing"
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

	if err := run([]string{"preview"}); err != nil {
		t.Fatalf("run(preview) error = %v", err)
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
	if got, want := featureBranchName("feature/potato"), "agent/feature/potato"; got != want {
		t.Errorf("featureBranchName() = %q, want %q", got, want)
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
