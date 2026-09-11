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

// fakeLookup resolves repository ids from a fixed table, recording lookups.
func fakeLookup(table map[string]Repository, calls *[]string) repositoryLookup {
	return func(_ context.Context, id string) (Repository, error) {
		if calls != nil {
			*calls = append(*calls, id)
		}
		repo, ok := table[id]
		if !ok {
			return Repository{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return repo, nil
	}
}

func TestResolveRepositoriesPrimaryOnly(t *testing.T) {
	build := Build{
		Repository:    Repository{ID: "r1", Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
		SourceBranch:  "refs/heads/main",
	}

	resolved, err := resolveRepositories(context.Background(), build, nil, fakeLookup(nil, nil))
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
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

// TestResolveRepositoriesResourceResolvedByID exercises the documented Runs
// response shape, in which a resource repository carries only id and type; the
// clone URL is resolved through the Git Repositories API.
func TestResolveRepositoriesResourceResolvedByID(t *testing.T) {
	build := Build{
		Repository:    Repository{ID: "app-id", Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceBranch:  "refs/heads/main",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		"self": {Repository: Repository{ID: "app-id", Type: "azureReposGit"}, RefName: "refs/heads/main", Version: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"lib":  {Repository: Repository{ID: "lib-id", Type: "azureReposGit"}, RefName: "refs/heads/release", Version: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}}
	var calls []string
	lookup := fakeLookup(map[string]Repository{
		"lib-id": {ID: "lib-id", Type: "azureReposGit", Name: "lib", RemoteURL: "https://dev.azure.com/org/proj/_git/lib"},
	}, &calls)

	resolved, err := resolveRepositories(context.Background(), build, run, lookup)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %+v, want two repositories", resolved)
	}
	if resolved[0].Alias != "self" || resolved[0].CloneURL != "https://dev.azure.com/org/proj/_git/app" {
		t.Errorf("primary = %+v, want self app from build repository", resolved[0])
	}
	if resolved[1].Alias != "lib" || resolved[1].Name != "lib" || resolved[1].CloneURL != "https://dev.azure.com/org/proj/_git/lib" || resolved[1].Commit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("resource = %+v, want lib resolved via lookup at its version", resolved[1])
	}
	// The primary uses the build repository URL; only the resource is looked up.
	if len(calls) != 1 || calls[0] != "lib-id" {
		t.Errorf("lookup calls = %v, want exactly [lib-id]", calls)
	}
}

func TestResolveRepositoriesResourceExplicitURL(t *testing.T) {
	build := Build{
		Repository:    Repository{ID: "app-id", Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		"ext": {Repository: Repository{Type: "gitHub", URL: "https://github.com/octo/tool"}, Version: "2222222222222222222222222222222222222222"},
	}}
	var calls []string

	resolved, err := resolveRepositories(context.Background(), build, run, fakeLookup(nil, &calls))
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	if resolved[1].CloneURL != "https://github.com/octo/tool" {
		t.Errorf("resource clone URL = %q, want explicit github URL", resolved[1].CloneURL)
	}
	if len(calls) != 0 {
		t.Errorf("lookup calls = %v, want none when a URL is present", calls)
	}
}

func TestResolveRepositoriesUnresolvableResource(t *testing.T) {
	build := Build{
		Repository:    Repository{ID: "app-id", Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	// A GitHub resource in the documented shape carries only a type; there is no
	// id to look up and no URL to clone.
	run := &Run{Repositories: map[string]RepositoryResource{
		"tool": {Repository: Repository{Type: "gitHub"}, Version: "2222222222222222222222222222222222222222"},
	}}

	_, err := resolveRepositories(context.Background(), build, run, fakeLookup(nil, nil))
	if !errors.Is(err, ErrUnresolvableRepository) {
		t.Fatalf("resolveRepositories() error = %v, want ErrUnresolvableRepository", err)
	}
}

func TestResolveRepositoriesUnsupportedTypeWithURL(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	// A non-Git type must be rejected even though it carries a URL.
	run := &Run{Repositories: map[string]RepositoryResource{
		"legacy": {Repository: Repository{Type: "TfsVersionControl", URL: "https://dev.azure.com/org/proj/_versionControl"}, Version: "2"},
	}}

	_, err := resolveRepositories(context.Background(), build, run, fakeLookup(nil, nil))
	if !errors.Is(err, ErrUnsupportedRepositoryType) {
		t.Fatalf("resolveRepositories() error = %v, want ErrUnsupportedRepositoryType", err)
	}
}

func TestResolveRepositoriesDeduplicatesByID(t *testing.T) {
	build := Build{
		Repository:    Repository{ID: "app-id", Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "dddddddddddddddddddddddddddddddddddddddd",
		SourceBranch:  "refs/heads/main",
	}
	// The same repository referenced again by id under a different alias, with
	// the same commit, must collapse to a single entry.
	run := &Run{Repositories: map[string]RepositoryResource{
		"app_again": {Repository: Repository{ID: "app-id", Type: "azureReposGit"}, Version: "dddddddddddddddddddddddddddddddddddddddd"},
	}}
	lookup := fakeLookup(map[string]Repository{
		"app-id": {ID: "app-id", Type: "azureReposGit", Name: "app", RemoteURL: "https://dev.azure.com/org/proj/_git/app"},
	}, nil)

	resolved, err := resolveRepositories(context.Background(), build, run, lookup)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %+v, want deduplicated single repository", resolved)
	}
}

func TestResolveRepositoriesConflictingVersions(t *testing.T) {
	build := Build{
		Repository:    Repository{ID: "app-id", Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{Repositories: map[string]RepositoryResource{
		"app_again": {Repository: Repository{ID: "app-id", Type: "azureReposGit"}, Version: "2222222222222222222222222222222222222222"},
	}}
	lookup := fakeLookup(map[string]Repository{
		"app-id": {ID: "app-id", Type: "azureReposGit", RemoteURL: "https://dev.azure.com/org/proj/_git/app"},
	}, nil)

	_, err := resolveRepositories(context.Background(), build, run, lookup)
	if !errors.Is(err, ErrConflictingVersions) {
		t.Fatalf("resolveRepositories() error = %v, want ErrConflictingVersions", err)
	}
}

func TestResolveRepositoriesMissingVersion(t *testing.T) {
	build := Build{Repository: Repository{Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"}}

	_, err := resolveRepositories(context.Background(), build, nil, fakeLookup(nil, nil))
	if !errors.Is(err, ErrMissingVersion) {
		t.Fatalf("resolveRepositories() error = %v, want ErrMissingVersion", err)
	}
}

func TestGetBuildRunAndRepository(t *testing.T) {
	const pat = "secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+pat)); got != want {
			t.Errorf("Authorization header = %q, want %q", got, want)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1111111111111111111111111111111111111111","sourceBranch":"refs/heads/main","repository":{"id":"app-id","type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/pipelines/7/runs/42"):
			fmt.Fprint(w, `{"resources":{"repositories":{"self":{"repository":{"id":"app-id","type":"azureReposGit"},"refName":"refs/heads/main","version":"1111111111111111111111111111111111111111"},"lib":{"repository":{"id":"lib-id","type":"azureReposGit"},"refName":"refs/heads/main","version":"2222222222222222222222222222222222222222"}}}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/git/repositories/lib-id"):
			fmt.Fprint(w, `{"id":"lib-id","name":"lib","remoteUrl":"https://dev.azure.com/org/proj/_git/lib"}`)
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
	if resolved[1].Alias != "lib" || resolved[1].CloneURL != "https://dev.azure.com/org/proj/_git/lib" || resolved[1].Commit != "2222222222222222222222222222222222222222" {
		t.Errorf("resource = %+v, want lib resolved via Git Repositories API at resolved commit", resolved[1])
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
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1111111111111111111111111111111111111111","sourceBranch":"refs/heads/main","repository":{"id":"app-id","type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
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
