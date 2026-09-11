package azuredevops

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveRepositoriesPrimaryOnly(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
		SourceBranch:  "refs/heads/main",
	}

	resolved, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if err != nil {
		t.Fatalf("ResolveRepositories() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %+v, want one repository", resolved)
	}
	want := ResolvedRepository{
		Alias:    "self",
		Name:     "app",
		Type:     "TfsGit",
		CloneURL: "https://dev.azure.com/org/proj/_git/app",
		Commit:   build.SourceVersion,
		RefName:  "refs/heads/main",
	}
	if resolved[0] != want {
		t.Errorf("resolved[0] = %+v, want %+v", resolved[0], want)
	}
}

func TestResolveRepositoriesPrimaryAndResources(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app"},
		SourceVersion: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceBranch:  "refs/heads/main",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		"self":   {Repository: Repository{Type: "azureReposGit", Name: "app"}, RefName: "refs/heads/main", Version: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"tools":  {Repository: Repository{Type: "azureReposGit", Name: "tools"}, RefName: "refs/heads/release", Version: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		"shared": {Repository: Repository{Type: "gitHub", Name: "octo/shared"}, RefName: "refs/heads/main", Version: "cccccccccccccccccccccccccccccccccccccccc"},
	}}

	resolved, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if err != nil {
		t.Fatalf("ResolveRepositories() error = %v", err)
	}
	if len(resolved) != 3 {
		t.Fatalf("resolved = %+v, want three repositories", resolved)
	}
	if resolved[0].Alias != "self" || resolved[0].CloneURL != "https://dev.azure.com/org/proj/_git/app" {
		t.Errorf("primary = %+v, want self app", resolved[0])
	}
	// Resource aliases are ordered deterministically (sorted): shared, tools.
	if resolved[1].Alias != "shared" || resolved[1].CloneURL != "https://github.com/octo/shared.git" {
		t.Errorf("resolved[1] = %+v, want github shared", resolved[1])
	}
	if resolved[2].Alias != "tools" || resolved[2].CloneURL != "https://dev.azure.com/org/proj/_git/tools" || resolved[2].Commit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("resolved[2] = %+v, want azure tools", resolved[2])
	}
}

func TestResolveRepositoriesDeduplicatesOverlap(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "dddddddddddddddddddddddddddddddddddddddd",
		SourceBranch:  "refs/heads/main",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		// Same repository as the primary, referenced again under an alias with
		// the same commit. It must collapse to a single entry.
		"app_again": {Repository: Repository{Type: "azureReposGit", Name: "app"}, RefName: "refs/heads/main", Version: "dddddddddddddddddddddddddddddddddddddddd"},
	}}

	resolved, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if err != nil {
		t.Fatalf("ResolveRepositories() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %+v, want deduplicated single repository", resolved)
	}
}

func TestResolveRepositoriesConflictingVersions(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app"},
		SourceVersion: "1111111111111111111111111111111111111111",
		SourceBranch:  "refs/heads/main",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		"app_again": {Repository: Repository{Type: "azureReposGit", Name: "app"}, Version: "2222222222222222222222222222222222222222"},
	}}

	_, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if !errors.Is(err, ErrConflictingVersions) {
		t.Fatalf("ResolveRepositories() error = %v, want ErrConflictingVersions", err)
	}
}

func TestResolveRepositoriesMissingVersion(t *testing.T) {
	build := Build{Repository: Repository{Type: "TfsGit", Name: "app"}}

	_, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if !errors.Is(err, ErrMissingVersion) {
		t.Fatalf("ResolveRepositories() error = %v, want ErrMissingVersion", err)
	}
}

func TestResolveRepositoriesUnsupportedType(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsVersionControl", Name: "$/app"},
		SourceVersion: "1",
	}

	_, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if !errors.Is(err, ErrUnsupportedRepositoryType) {
		t.Fatalf("ResolveRepositories() error = %v, want ErrUnsupportedRepositoryType", err)
	}
}

func TestResolveRepositoriesCrossProjectAzureRepo(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		"other": {Repository: Repository{Type: "azureReposGit", Name: "OtherProject/library"}, Version: "2222222222222222222222222222222222222222"},
	}}

	resolved, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if err != nil {
		t.Fatalf("ResolveRepositories() error = %v", err)
	}
	if got, want := resolved[1].CloneURL, "https://dev.azure.com/org/OtherProject/_git/library"; got != want {
		t.Errorf("cross-project clone URL = %q, want %q", got, want)
	}
}

func TestResolveRepositoriesPrefersExplicitURL(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "gitHubEnterprise", Name: "team/app", URL: "https://ghe.example.com/team/app.git"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}

	resolved, err := ResolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if err != nil {
		t.Fatalf("ResolveRepositories() error = %v", err)
	}
	if got, want := resolved[0].CloneURL, "https://ghe.example.com/team/app.git"; got != want {
		t.Errorf("clone URL = %q, want explicit %q", got, want)
	}
}

func TestGetBuildAndRun(t *testing.T) {
	const pat = "secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+pat)); got != want {
			t.Errorf("Authorization header = %q, want %q", got, want)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1111111111111111111111111111111111111111","sourceBranch":"refs/heads/main","repository":{"id":"r1","type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/pipelines/7/runs/42"):
			fmt.Fprint(w, `{"resources":{"repositories":{"self":{"repository":{"type":"azureReposGit","name":"app"},"refName":"refs/heads/main","version":"1111111111111111111111111111111111111111"},"lib":{"repository":{"type":"azureReposGit","name":"lib"},"refName":"refs/heads/main","version":"2222222222222222222222222222222222222222"}}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/org", "proj", pat, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	resolved, err := client.ResolveBuildRepositories(context.Background(), 42)
	if err != nil {
		t.Fatalf("ResolveBuildRepositories() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %+v, want two repositories", resolved)
	}
	if resolved[0].CloneURL != "https://dev.azure.com/org/proj/_git/app" {
		t.Errorf("primary clone URL = %q", resolved[0].CloneURL)
	}
	if resolved[1].Alias != "lib" || resolved[1].Commit != "2222222222222222222222222222222222222222" {
		t.Errorf("resource = %+v, want lib at resolved commit", resolved[1])
	}
}

func TestGetBuildUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/org", "proj", "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ResolveBuildRepositories(context.Background(), 42)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "token") {
		t.Errorf("error message leaked the token: %v", err)
	}
}

func TestGetBuildNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/org", "proj", "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ResolveBuildRepositories(context.Background(), 42)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestRunNotFoundFallsBackToPrimary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42") {
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1111111111111111111111111111111111111111","sourceBranch":"refs/heads/main","repository":{"type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/org", "proj", "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	resolved, err := client.ResolveBuildRepositories(context.Background(), 42)
	if err != nil {
		t.Fatalf("ResolveBuildRepositories() error = %v", err)
	}
	if len(resolved) != 1 || resolved[0].Alias != "self" {
		t.Fatalf("resolved = %+v, want primary repository only", resolved)
	}
}

func TestNewClientRequiresToken(t *testing.T) {
	if _, err := NewClient("https://dev.azure.com/org", "proj", "  ", nil); err == nil {
		t.Fatal("NewClient() error = nil, want missing token error")
	}
}
