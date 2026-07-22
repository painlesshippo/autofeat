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
