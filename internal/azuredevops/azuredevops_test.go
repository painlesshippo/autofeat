package azuredevops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// documentedResourcesYAML declares repository resources the way a run's expanded
// pipeline YAML does: identity (type and name) lives here, not in the run's
// resources.repositories entries, which carry only a type.
const documentedResourcesYAML = `
resources:
  repositories:
  - repository: lib
    type: git
    name: proj/lib
    ref: refs/heads/release
  - repository: tool
    type: github
    name: octo/tool
    ref: refs/heads/main
`

// libOnlyResourcesYAML declares a single Azure Repos resource.
const libOnlyResourcesYAML = `
resources:
  repositories:
  - repository: lib
    type: git
    name: proj/lib
    ref: refs/heads/release
`

func TestResolveRepositoriesPrimaryOnly(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
		SourceBranch:  "refs/heads/main",
	}

	resolved, err := resolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	want := []ResolvedRepository{{
		Alias: "self", Name: "app", Type: "TfsGit",
		CloneURL: "https://dev.azure.com/org/proj/_git/app",
		Commit:   build.SourceVersion, RefName: "refs/heads/main",
	}}
	if len(resolved) != 1 || resolved[0] != want[0] {
		t.Errorf("resolved = %+v, want %+v", resolved, want)
	}
}

// TestResolveRepositoriesResourcesFromFinalYAML uses the documented run shape:
// resource repository objects carry only a type, and identity comes from the
// run's expanded YAML.
func TestResolveRepositoriesResourcesFromFinalYAML(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceBranch:  "refs/heads/main",
	}
	run := &Run{
		FinalYAML: documentedResourcesYAML,
		Repositories: map[string]RepositoryResource{
			"self": {Repository: Repository{Type: "azureReposGit"}, RefName: "refs/heads/main", Version: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			"lib":  {Repository: Repository{Type: "azureReposGit"}, RefName: "refs/heads/release", Version: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			"tool": {Repository: Repository{Type: "gitHub"}, RefName: "refs/heads/main", Version: "cccccccccccccccccccccccccccccccccccccccc"},
		},
	}

	resolved, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	if len(resolved) != 3 {
		t.Fatalf("resolved = %+v, want three repositories", resolved)
	}
	if resolved[0].Alias != "self" || resolved[0].CloneURL != "https://dev.azure.com/org/proj/_git/app" {
		t.Errorf("primary = %+v, want self app", resolved[0])
	}
	// Aliases are ordered deterministically (sorted): lib, tool.
	if resolved[1].Alias != "lib" || resolved[1].CloneURL != "https://dev.azure.com/org/proj/_git/lib" || resolved[1].Commit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("resolved[1] = %+v, want azure lib at resolved commit", resolved[1])
	}
	if resolved[2].Alias != "tool" || resolved[2].CloneURL != "https://github.com/octo/tool.git" || resolved[2].Commit != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Errorf("resolved[2] = %+v, want github tool at resolved commit", resolved[2])
	}
}

func TestResolveRepositoriesCrossProjectAzureRepo(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{
		FinalYAML: "resources:\n  repositories:\n  - repository: other\n    type: git\n    name: OtherProject/library\n",
		Repositories: map[string]RepositoryResource{
			"other": {Repository: Repository{Type: "azureReposGit"}, Version: "2222222222222222222222222222222222222222"},
		},
	}

	resolved, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	if got, want := resolved[1].CloneURL, "https://dev.azure.com/org/OtherProject/_git/library"; got != want {
		t.Errorf("cross-project clone URL = %q, want %q", got, want)
	}
}

func TestResolveRepositoriesMissingDeclaration(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{
		FinalYAML: "resources:\n  repositories: []\n",
		Repositories: map[string]RepositoryResource{
			"lib": {Repository: Repository{Type: "azureReposGit"}, Version: "2222222222222222222222222222222222222222"},
		},
	}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if !errors.Is(err, ErrUnresolvableRepository) {
		t.Fatalf("resolveRepositories() error = %v, want ErrUnresolvableRepository", err)
	}
}

func TestResolveRepositoriesUnsupportedType(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{
		FinalYAML: "resources:\n  repositories:\n  - repository: legacy\n    type: tfvc\n    name: $/legacy\n",
		Repositories: map[string]RepositoryResource{
			"legacy": {Repository: Repository{Type: "tfvc"}, Version: "2"},
		},
	}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if !errors.Is(err, ErrUnsupportedRepositoryType) {
		t.Fatalf("resolveRepositories() error = %v, want ErrUnsupportedRepositoryType", err)
	}
}

func TestResolveRepositoriesDeduplicatesOverlap(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "dddddddddddddddddddddddddddddddddddddddd",
	}
	// A resource pointing at the same repository as the primary, at the same
	// commit, collapses to one entry.
	run := &Run{
		FinalYAML: "resources:\n  repositories:\n  - repository: app_again\n    type: git\n    name: proj/app\n",
		Repositories: map[string]RepositoryResource{
			"app_again": {Repository: Repository{Type: "azureReposGit"}, Version: "dddddddddddddddddddddddddddddddddddddddd"},
		},
	}

	resolved, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if err != nil {
		t.Fatalf("resolveRepositories() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %+v, want deduplicated single repository", resolved)
	}
}

func TestResolveRepositoriesConflictingVersions(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{
		FinalYAML: "resources:\n  repositories:\n  - repository: app_again\n    type: git\n    name: proj/app\n",
		Repositories: map[string]RepositoryResource{
			"app_again": {Repository: Repository{Type: "azureReposGit"}, Version: "2222222222222222222222222222222222222222"},
		},
	}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if !errors.Is(err, ErrConflictingVersions) {
		t.Fatalf("resolveRepositories() error = %v, want ErrConflictingVersions", err)
	}
}

func TestResolveRepositoriesMissingVersion(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	run := &Run{
		FinalYAML: "resources:\n  repositories:\n  - repository: lib\n    type: git\n    name: proj/lib\n",
		Repositories: map[string]RepositoryResource{
			"lib": {Repository: Repository{Type: "azureReposGit"}, Version: ""},
		},
	}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if !errors.Is(err, ErrMissingVersion) {
		t.Fatalf("resolveRepositories() error = %v, want ErrMissingVersion", err)
	}
}

func TestResolveRepositoriesUnsupportedPrimaryType(t *testing.T) {
	// A TFVC primary repository carrying an HTTP URL must be rejected with an
	// actionable type error, not passed into the clone workflow.
	build := Build{
		Repository:    Repository{Type: "TfsVersionControl", Name: "$/app", URL: "https://dev.azure.com/org/proj/_versionControl"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if !errors.Is(err, ErrUnsupportedRepositoryType) {
		t.Fatalf("resolveRepositories() error = %v, want ErrUnsupportedRepositoryType", err)
	}
}

// TestResolveRepositoriesDeclarationWithoutRunVersion covers a repository that
// the pipeline YAML declares but the run did not resolve; it must fail with a
// missing-version error rather than being silently omitted.
func TestResolveRepositoriesDeclarationWithoutRunVersion(t *testing.T) {
	build := Build{
		Repository:    Repository{Type: "TfsGit", Name: "app", URL: "https://dev.azure.com/org/proj/_git/app"},
		SourceVersion: "1111111111111111111111111111111111111111",
	}
	// finalYaml declares both lib and tool, but the run resolved only lib.
	run := &Run{
		FinalYAML: documentedResourcesYAML,
		Repositories: map[string]RepositoryResource{
			"self": {Repository: Repository{Type: "azureReposGit"}, Version: "1111111111111111111111111111111111111111"},
			"lib":  {Repository: Repository{Type: "azureReposGit"}, Version: "2222222222222222222222222222222222222222"},
		},
	}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, run)
	if !errors.Is(err, ErrMissingVersion) {
		t.Fatalf("resolveRepositories() error = %v, want ErrMissingVersion", err)
	}
}

func TestResolveRepositoriesPrimaryMissingURL(t *testing.T) {
	build := Build{Repository: Repository{Type: "TfsGit", Name: "app"}, SourceVersion: "1"}

	_, err := resolveRepositories("https://dev.azure.com/org", "proj", build, nil)
	if !errors.Is(err, ErrUnresolvableRepository) {
		t.Fatalf("resolveRepositories() error = %v, want ErrUnresolvableRepository", err)
	}
}

// azureServer serves a YAML pipeline build with one repository resource resolved
// through finalYaml. Only a type is present on the run's resource repository.
func azureServer(t *testing.T, pat string) *httptest.Server {
	t.Helper()
	runJSON := func() string {
		payload := map[string]any{
			"finalYaml": libOnlyResourcesYAML,
			"resources": map[string]any{
				"repositories": map[string]any{
					"self": map[string]any{"repository": map[string]any{"type": "azureReposGit"}, "refName": "refs/heads/main", "version": "1111111111111111111111111111111111111111"},
					"lib":  map[string]any{"repository": map[string]any{"type": "azureReposGit"}, "refName": "refs/heads/release", "version": "2222222222222222222222222222222222222222"},
				},
			},
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pat != "" {
			if got, want := r.Header.Get("Authorization"), "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+pat)); got != want {
				t.Errorf("Authorization header = %q, want %q", got, want)
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1111111111111111111111111111111111111111","sourceBranch":"refs/heads/main","repository":{"type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/build/definitions/7"):
			fmt.Fprint(w, `{"id":7,"process":{"type":2}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/pipelines/7/runs/42"):
			fmt.Fprint(w, runJSON)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestResolveBuildRepositoriesEndToEnd(t *testing.T) {
	const pat = "secret-token"
	server := azureServer(t, pat)
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
	// The resource clone URL is constructed from the client's organization URL
	// and the YAML-declared name.
	wantLibURL := server.URL + "/org/proj/_git/lib"
	if resolved[1].Alias != "lib" || resolved[1].CloneURL != wantLibURL || resolved[1].Commit != "2222222222222222222222222222222222222222" {
		t.Errorf("resource = %+v, want lib %q resolved from finalYaml at its run version", resolved[1], wantLibURL)
	}
}

func TestClassicDefinitionSkipsRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1111111111111111111111111111111111111111","sourceBranch":"refs/heads/main","repository":{"type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/build/definitions/7"):
			fmt.Fprint(w, `{"id":7,"process":{"type":1}}`)
		default:
			// A run request would 404 here; a classic build must not make one.
			t.Errorf("unexpected request to %s for a classic build", r.URL.Path)
			http.NotFound(w, r)
		}
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

func TestYAMLRunNotFoundIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprint(w, `{"id":42,"definition":{"id":7},"sourceVersion":"1","repository":{"type":"TfsGit","name":"app","url":"https://dev.azure.com/org/proj/_git/app"}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/build/definitions/7"):
			fmt.Fprint(w, `{"id":7,"process":{"type":2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/org", "proj", "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	// A YAML pipeline whose run cannot be fetched must not silently fall back to
	// a primary-only workspace.
	if _, err := client.ResolveBuildRepositories(context.Background(), 42); err == nil {
		t.Fatal("ResolveBuildRepositories() error = nil, want run-not-found error")
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

func TestErrorBodyRedactsCredentials(t *testing.T) {
	const pat = "super-secret-token"
	basic := base64.StdEncoding.EncodeToString([]byte(":" + pat))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate a proxy/diagnostic endpoint that echoes the request
		// credentials in a 500 body.
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message":"upstream echoed Authorization: Basic %s and pat=%s"}`, basic, pat)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/org", "proj", pat, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ResolveBuildRepositories(context.Background(), 42)
	if err == nil {
		t.Fatal("ResolveBuildRepositories() error = nil, want request failure")
	}
	if strings.Contains(err.Error(), pat) {
		t.Errorf("error leaked the raw token: %v", err)
	}
	if strings.Contains(err.Error(), basic) {
		t.Errorf("error leaked the Basic credential: %v", err)
	}
	// The status should still be reported for actionability.
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error dropped the status context: %v", err)
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

func TestNewClientRequiresToken(t *testing.T) {
	if _, err := NewClient("https://dev.azure.com/org", "proj", "  ", nil); err == nil {
		t.Fatal("NewClient() error = nil, want missing token error")
	}
}
