package main

import "testing"

func TestVersionCommand(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatalf("run(version) error = %v", err)
	}

	if err := run([]string{"version", "extra"}); err == nil {
		t.Error("run(version, extra) error = nil, want usage error")
	}
}

func TestPreviewBase(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default", want: "master"},
		{name: "custom branch", args: []string{"--base", "develop"}, want: "develop"},
		{name: "hierarchical branch", args: []string{"--base", "release/next"}, want: "release/next"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := previewBase(test.args)
			if err != nil {
				t.Fatalf("previewBase(%v) error = %v", test.args, err)
			}
			if got != test.want {
				t.Errorf("previewBase(%v) = %q, want %q", test.args, got, test.want)
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
		if _, err := previewBase(args); err == nil {
			t.Errorf("previewBase(%v) error = nil, want error", args)
		}
	}
}

func TestPreviewCommandDispatch(t *testing.T) {
	originalPreviewCommand := previewCommand
	t.Cleanup(func() {
		previewCommand = originalPreviewCommand
	})

	var gotBase string
	previewCommand = func(baseRef string) error {
		gotBase = baseRef
		return nil
	}

	if err := run([]string{"preview"}); err != nil {
		t.Fatalf("run(preview) error = %v", err)
	}
	if gotBase != defaultPreviewBase {
		t.Errorf("run(preview) base = %q, want %q", gotBase, defaultPreviewBase)
	}

	if err := run([]string{"preview", "--base", "develop"}); err != nil {
		t.Fatalf("run(preview --base develop) error = %v", err)
	}
	if gotBase != "develop" {
		t.Errorf("run(preview --base develop) base = %q, want develop", gotBase)
	}

	if err := run([]string{"preview", "--base"}); err == nil {
		t.Error("run(preview --base) error = nil, want usage error")
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
