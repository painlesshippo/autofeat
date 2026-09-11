package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/state"
)

func TestParseAzureBuildTarget(t *testing.T) {
	tests := []struct {
		name        string
		buildRef    string
		org         string
		project     string
		instance    string
		wantOrgURL  string
		wantProject string
		wantBuild   int
		wantErr     bool
	}{
		{
			name:        "dev azure url",
			buildRef:    "https://dev.azure.com/contoso/My%20Project/_build/results?buildId=1234&view=results",
			wantOrgURL:  "https://dev.azure.com/contoso",
			wantProject: "My Project",
			wantBuild:   1234,
		},
		{
			name:        "visualstudio url",
			buildRef:    "https://contoso.visualstudio.com/Widgets/_build/results?buildId=77",
			wantOrgURL:  "https://contoso.visualstudio.com",
			wantProject: "Widgets",
			wantBuild:   77,
		},
		{
			name:        "numeric with flags",
			buildRef:    "42",
			org:         "contoso",
			project:     "Widgets",
			wantOrgURL:  "https://dev.azure.com/contoso",
			wantProject: "Widgets",
			wantBuild:   42,
		},
		{
			name:        "numeric with custom instance",
			buildRef:    "42",
			org:         "contoso",
			project:     "Widgets",
			instance:    "https://azure.example.com/",
			wantOrgURL:  "https://azure.example.com/contoso",
			wantProject: "Widgets",
			wantBuild:   42,
		},
		{
			// Azure DevOps Server: the collection is the organization segment
			// appended to the server URL.
			name:        "on-premises collection",
			buildRef:    "55",
			org:         "DefaultCollection",
			project:     "Widgets",
			instance:    "https://server/tfs",
			wantOrgURL:  "https://server/tfs/DefaultCollection",
			wantProject: "Widgets",
			wantBuild:   55,
		},
		{
			name:        "url project override",
			buildRef:    "https://dev.azure.com/contoso/Ignored/_build/results?buildId=9",
			project:     "Chosen",
			wantOrgURL:  "https://dev.azure.com/contoso",
			wantProject: "Chosen",
			wantBuild:   9,
		},
		{name: "numeric without flags", buildRef: "42", wantErr: true},
		{name: "url missing build id", buildRef: "https://dev.azure.com/contoso/Widgets/_build/results", wantErr: true},
		{name: "unknown host", buildRef: "https://example.com/contoso/Widgets/_build/results?buildId=1", wantErr: true},
		{name: "empty", buildRef: "", wantErr: true},
		{name: "non-numeric", buildRef: "abc", org: "o", project: "p", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := parseAzureBuildTarget(test.buildRef, test.org, test.project, test.instance)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseAzureBuildTarget() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAzureBuildTarget() error = %v", err)
			}
			if target.orgURL != test.wantOrgURL || target.project != test.wantProject || target.buildID != test.wantBuild {
				t.Errorf("parseAzureBuildTarget() = %+v, want orgURL %q project %q build %d", target, test.wantOrgURL, test.wantProject, test.wantBuild)
			}
		})
	}
}

func TestAddAzureBuildWorkspaceCreatesPinnedSession(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appBare, appFirst, appSecond := createAzureBareRepo(t, "app")
	libBare, _, libSecond := createAzureBareRepo(t, "lib")

	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureYAMLServer(t, appURL, appFirst, libSecond)
	// The primary URL comes from the Build API; the lib resource URL is
	// constructed from the organization URL and the YAML-declared name.
	libURL := server.URL + "/org/proj/_git/lib"
	redirectCloneURL(t, appURL, appBare)
	redirectCloneURL(t, libURL, libBare)

	if err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL}); err != nil {
		t.Fatalf("run(new --azure-build) error = %v", err)
	}

	session, err := state.GetSession("feature/az")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Repos) != 2 {
		t.Fatalf("session repos = %+v, want two repositories", session.Repos)
	}

	byName := make(map[string]state.Repository, len(session.Repos))
	for _, repository := range session.Repos {
		byName[repository.Name] = repository
	}
	assertPinnedRepository(t, byName["app"], appURL, appFirst)
	assertPinnedRepository(t, byName["lib"], libURL, libSecond)

	if appFirst == appSecond {
		t.Fatal("test setup did not create distinct app commits")
	}
	if branch, err := gitcmd.CurrentBranch(byName["app"].WorktreePath); err != nil || branch != "feature/az" {
		t.Errorf("app branch = %q (err %v), want feature/az", branch, err)
	}
}

func assertPinnedRepository(t *testing.T, repository state.Repository, wantURL, wantCommit string) {
	t.Helper()
	if !repository.IsRemoteClone {
		t.Errorf("repository %q IsRemoteClone = false, want true", repository.Name)
	}
	if repository.OriginalPath != wantURL {
		t.Errorf("repository %q OriginalPath = %q, want %q (no credentials)", repository.Name, repository.OriginalPath, wantURL)
	}
	if repository.BaseBranch != wantCommit {
		t.Errorf("repository %q BaseBranch = %q, want pinned commit %q", repository.Name, repository.BaseBranch, wantCommit)
	}
	head := strings.TrimSpace(mainGitOutput(t, repository.WorktreePath, "rev-parse", "HEAD"))
	if head != wantCommit {
		t.Errorf("repository %q HEAD = %q, want %q", repository.Name, head, wantCommit)
	}
}

// TestAddAzureBuildWorkspacePinsOverExistingRemoteBranch proves the checkout is
// pinned to the build commit even when the clone already carries a remote
// feature branch that advanced past it.
func TestAddAzureBuildWorkspacePinsOverExistingRemoteBranch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appBare, appFirst, appSecond := createAzureBareRepo(t, "app")
	// origin/feature/az sits at the newer commit; the build resolved the older.
	runMainGit(t, appBare, "update-ref", "refs/heads/feature/az", appSecond)

	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureClassicServer(t, appURL, appFirst, "refs/heads/main")
	redirectCloneURL(t, appURL, appBare)

	if err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL}); err != nil {
		t.Fatalf("run(new --azure-build) error = %v", err)
	}
	session, err := state.GetSession("feature/az")
	if err != nil {
		t.Fatal(err)
	}
	if head := strings.TrimSpace(mainGitOutput(t, session.Repos[0].WorktreePath, "rev-parse", "HEAD")); head != appFirst {
		t.Errorf("HEAD = %q, want build commit %q, not remote feature tip %q", head, appFirst, appSecond)
	}
}

// TestAddAzureBuildWorkspacePinsWhenFeatureNameIsDefaultBranch proves the pin
// holds when the feature name equals the clone's default branch.
func TestAddAzureBuildWorkspacePinsWhenFeatureNameIsDefaultBranch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appBare, appFirst, appSecond := createAzureBareRepo(t, "app")
	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureClassicServer(t, appURL, appFirst, "refs/heads/main")
	redirectCloneURL(t, appURL, appBare)

	if err := run([]string{"new", "main", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL}); err != nil {
		t.Fatalf("run(new --azure-build) error = %v", err)
	}
	session, err := state.GetSession("main")
	if err != nil {
		t.Fatal(err)
	}
	worktree := session.Repos[0].WorktreePath
	if branch, err := gitcmd.CurrentBranch(worktree); err != nil || branch != "main" {
		t.Errorf("branch = %q (err %v), want main", branch, err)
	}
	if head := strings.TrimSpace(mainGitOutput(t, worktree, "rev-parse", "HEAD")); head != appFirst {
		t.Errorf("HEAD = %q, want build commit %q, not default branch tip %q", head, appFirst, appSecond)
	}
}

func TestAddAzureBuildWorkspaceRequiresToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "")
	t.Setenv("AZURE_DEVOPS_PAT", "")

	err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", "https://dev.azure.com"})
	if err == nil {
		t.Fatal("run(new --azure-build) error = nil, want missing token error")
	}
	if !strings.Contains(err.Error(), "AZURE_DEVOPS_EXT_PAT") {
		t.Errorf("error = %v, want it to name the token environment variable", err)
	}
}

func TestAddAzureBuildWorkspaceUnauthorizedLeavesNoSession(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL})
	if err == nil {
		t.Fatal("run(new --azure-build) error = nil, want authorization error")
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Errorf("error leaked the token: %v", err)
	}
	assertNoWorkspaceRemains(t, "feature/az")
}

func TestAddAzureBuildWorkspaceRejectsExistingSession(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")
	if err := state.SaveSession("feature/az", state.Session{}); err != nil {
		t.Fatal(err)
	}

	appBare, appFirst, _ := createAzureBareRepo(t, "app")
	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureClassicServer(t, appURL, appFirst, "refs/heads/main")
	redirectCloneURL(t, appURL, appBare)

	err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("run(new --azure-build) error = %v, want already-exists error", err)
	}
}

func TestAddAzureBuildWorkspaceRejectsPreexistingDirectory(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appBare, appFirst, _ := createAzureBareRepo(t, "app")
	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureClassicServer(t, appURL, appFirst, "refs/heads/main")
	redirectCloneURL(t, appURL, appBare)

	// A pre-existing feature directory with user content must be left intact.
	featureDir := filepath.Join(mainWorkspaceDir(t), featureDirectoryName("feature/az"))
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(featureDir, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("user data\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("run(new --azure-build) error = %v, want pre-existing-path error", err)
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Errorf("sentinel file was removed: %v", statErr)
	}
	if _, err := state.GetSession("feature/az"); err == nil {
		t.Error("session was created despite a pre-existing feature directory")
	}
}

func TestAddAzureBuildWorkspaceRejectsPreexistingFile(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appBare, appFirst, _ := createAzureBareRepo(t, "app")
	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureClassicServer(t, appURL, appFirst, "refs/heads/main")
	redirectCloneURL(t, appURL, appBare)

	// A regular file already sitting at the feature path must be preserved.
	featureDir := filepath.Join(mainWorkspaceDir(t), featureDirectoryName("feature/az"))
	if err := os.MkdirAll(mainWorkspaceDir(t), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(featureDir, []byte("not a workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("run(new --azure-build) error = %v, want pre-existing-path error", err)
	}
	info, statErr := os.Stat(featureDir)
	if statErr != nil || info.IsDir() {
		t.Errorf("pre-existing file was replaced (stat err = %v, isDir = %v)", statErr, statErr == nil && info.IsDir())
	}
}

func TestAddAzureBuildWorkspaceCleansUpOnCloneFailure(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appURL := "https://dev.azure.example/org/proj/_git/app"
	server := newAzureClassicServer(t, appURL, "1111111111111111111111111111111111111111", "refs/heads/main")
	// Redirect the clone URL to a path that does not exist so the clone fails.
	redirectCloneURL(t, appURL, filepath.Join(t.TempDir(), "missing.git"))

	if err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL}); err == nil {
		t.Fatal("run(new --azure-build) error = nil, want clone failure")
	}
	assertNoWorkspaceRemains(t, "feature/az")
}

func TestAddAzureBuildWorkspaceCleansUpOnCheckoutFailure(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())
	writeMainConfig(t, "code", "copilot")
	t.Setenv("AZURE_DEVOPS_EXT_PAT", "test-token")

	appBare, _, _ := createAzureBareRepo(t, "app")
	appURL := "https://dev.azure.example/org/proj/_git/app"
	// The build resolves a commit that the clone cannot obtain from any ref.
	server := newAzureClassicServer(t, appURL, "0123456789012345678901234567890123456789", "refs/heads/does-not-exist")
	redirectCloneURL(t, appURL, appBare)

	if err := run([]string{"new", "feature/az", "--azure-build", "42", "--azure-org", "org", "--azure-project", "proj", "--azure-url", server.URL}); err == nil {
		t.Fatal("run(new --azure-build) error = nil, want checkout failure")
	}
	assertNoWorkspaceRemains(t, "feature/az")
}

// assertNoWorkspaceRemains verifies neither the session nor its feature
// directory survives a failed creation.
func assertNoWorkspaceRemains(t *testing.T, featureName string) {
	t.Helper()
	if _, err := state.GetSession(featureName); err == nil {
		t.Errorf("session %q was created despite failure", featureName)
	}
	featureDir := filepath.Join(mainWorkspaceDir(t), featureDirectoryName(featureName))
	if _, err := os.Stat(featureDir); !os.IsNotExist(err) {
		t.Errorf("feature directory %q survived failure (stat err = %v)", featureDir, err)
	}
}

// newAzureClassicServer serves a classic (designer) single-repository build. Its
// primary clone URL comes from the Build API and it has no run resources.
func newAzureClassicServer(t *testing.T, appURL, appCommit, sourceBranch string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprintf(w, `{"id":42,"definition":{"id":7},"sourceVersion":%q,"sourceBranch":%q,"repository":{"type":"TfsGit","name":"app","url":%q}}`, appCommit, sourceBranch, appURL)
		case strings.HasSuffix(r.URL.Path, "/_apis/build/definitions/7"):
			fmt.Fprint(w, `{"id":7,"process":{"type":1}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// newAzureYAMLServer serves a YAML pipeline build: primary repository app (clone
// URL from the Build API) plus a repository resource lib, whose identity is
// declared in the run's finalYaml and whose run entry carries only a type.
func newAzureYAMLServer(t *testing.T, appURL, appCommit, libCommit string) *httptest.Server {
	t.Helper()
	finalYAML := "resources:\n  repositories:\n  - repository: lib\n    type: git\n    name: proj/lib\n    ref: refs/heads/main\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_apis/build/builds/42"):
			fmt.Fprintf(w, `{"id":42,"definition":{"id":7},"sourceVersion":%q,"sourceBranch":"refs/heads/main","repository":{"type":"TfsGit","name":"app","url":%q}}`, appCommit, appURL)
		case strings.HasSuffix(r.URL.Path, "/_apis/build/definitions/7"):
			fmt.Fprint(w, `{"id":7,"process":{"type":2}}`)
		case strings.HasSuffix(r.URL.Path, "/_apis/pipelines/7/runs/42"):
			fmt.Fprintf(w, `{"finalYaml":%q,"resources":{"repositories":{"self":{"repository":{"type":"azureReposGit"},"refName":"refs/heads/main","version":%q},"lib":{"repository":{"type":"azureReposGit"},"refName":"refs/heads/main","version":%q}}}}`, finalYAML, appCommit, libCommit)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// createAzureBareRepo builds a bare repository with two commits on main and
// returns its path and both commit ids.
func createAzureBareRepo(t *testing.T, name string) (barePath, first, second string) {
	t.Helper()
	source := createMainRepository(t)
	runMainGit(t, source, "branch", "-M", "main")
	first = strings.TrimSpace(mainGitOutput(t, source, "rev-parse", "HEAD"))
	writeAndCommitMainFile(t, source, name+".txt", "content\n", "second commit")
	second = strings.TrimSpace(mainGitOutput(t, source, "rev-parse", "HEAD"))

	barePath = filepath.Join(t.TempDir(), name+".git")
	runMainGit(t, source, "init", "--bare", "-q", barePath)
	runMainGit(t, barePath, "symbolic-ref", "HEAD", "refs/heads/main")
	runMainGit(t, source, "remote", "add", "origin", barePath)
	runMainGit(t, source, "push", "-qu", "origin", "main")
	return barePath, first, second
}

func redirectCloneURL(t *testing.T, cloneURL, barePath string) {
	t.Helper()
	fileURL := "file://" + filepath.ToSlash(barePath)
	runMainGit(t, t.TempDir(), "config", "--global", "url."+fileURL+".insteadOf", cloneURL)
}
