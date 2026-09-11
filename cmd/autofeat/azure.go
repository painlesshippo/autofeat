package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/painlesshippo/autofeat/internal/azuredevops"
	"github.com/painlesshippo/autofeat/internal/config"
	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/hooks"
	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/workspace"
)

// azureDefaultInstance is the public Azure DevOps Services base URL.
const azureDefaultInstance = "https://dev.azure.com"

// azureTokenEnvVars are the environment variables consulted for the Azure
// DevOps personal access token, in priority order. The first matches the
// Azure DevOps CLI convention.
var azureTokenEnvVars = []string{"AZURE_DEVOPS_EXT_PAT", "AZURE_DEVOPS_PAT"}

// azureBuildTarget identifies a build to resolve.
type azureBuildTarget struct {
	orgURL  string
	project string
	buildID int
}

// azureDevOpsToken reads the personal access token from the environment. The
// token is never persisted or printed.
func azureDevOpsToken() (string, error) {
	for _, name := range azureTokenEnvVars {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("set %s to an Azure DevOps personal access token with build (read) scope", azureTokenEnvVars[0])
}

// parseAzureBuildTarget resolves org/project/build identity from a build id or
// a build results URL, with flag overrides for the numeric form.
func parseAzureBuildTarget(buildRef, org, project, instanceURL string) (azureBuildTarget, error) {
	buildRef = strings.TrimSpace(buildRef)
	if buildRef == "" {
		return azureBuildTarget{}, errors.New("--azure-build requires a build id or a build results URL")
	}

	if isRemoteURL(buildRef) {
		return parseAzureBuildURL(buildRef, project)
	}

	buildID, err := azuredevops.ParseBuildID(buildRef)
	if err != nil {
		return azureBuildTarget{}, err
	}
	if strings.TrimSpace(org) == "" || strings.TrimSpace(project) == "" {
		return azureBuildTarget{}, errors.New("--azure-org and --azure-project are required with a numeric --azure-build")
	}
	base := strings.TrimRight(strings.TrimSpace(instanceURL), "/")
	if base == "" {
		base = azureDefaultInstance
	}
	return azureBuildTarget{
		orgURL:  base + "/" + url.PathEscape(strings.TrimSpace(org)),
		project: strings.TrimSpace(project),
		buildID: buildID,
	}, nil
}

// parseAzureBuildURL extracts org/project/build identity from an Azure DevOps
// build results URL. projectOverride, when set, replaces the parsed project.
func parseAzureBuildURL(rawURL, projectOverride string) (azureBuildTarget, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return azureBuildTarget{}, fmt.Errorf("parse Azure DevOps build URL %q: %w", rawURL, err)
	}
	buildIDValue := parsed.Query().Get("buildId")
	if buildIDValue == "" {
		return azureBuildTarget{}, fmt.Errorf("Azure DevOps build URL %q is missing a buildId query parameter", rawURL)
	}
	buildID, err := azuredevops.ParseBuildID(buildIDValue)
	if err != nil {
		return azureBuildTarget{}, err
	}

	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(strings.Trim(parsed.Path, "/"), "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}

	host := strings.ToLower(parsed.Host)
	var orgURL, project string
	switch {
	case host == "dev.azure.com":
		// https://dev.azure.com/{org}/{project}/_build/results
		if len(segments) < 2 {
			return azureBuildTarget{}, fmt.Errorf("Azure DevOps build URL %q does not include an organization and project", rawURL)
		}
		orgURL = parsed.Scheme + "://" + parsed.Host + "/" + segments[0]
		project = decodePathSegment(segments[1])
	case strings.HasSuffix(host, ".visualstudio.com"):
		// https://{org}.visualstudio.com/{project}/_build/results
		if len(segments) < 1 {
			return azureBuildTarget{}, fmt.Errorf("Azure DevOps build URL %q does not include a project", rawURL)
		}
		orgURL = parsed.Scheme + "://" + parsed.Host
		project = decodePathSegment(segments[0])
	default:
		return azureBuildTarget{}, fmt.Errorf("unrecognized Azure DevOps host %q; pass --azure-build <id> with --azure-org, --azure-project, and --azure-url instead", parsed.Host)
	}

	if override := strings.TrimSpace(projectOverride); override != "" {
		project = override
	}
	if project == "" {
		return azureBuildTarget{}, fmt.Errorf("Azure DevOps build URL %q does not include a project", rawURL)
	}
	return azureBuildTarget{orgURL: orgURL, project: project, buildID: buildID}, nil
}

func decodePathSegment(segment string) string {
	if decoded, err := url.PathUnescape(segment); err == nil {
		return decoded
	}
	return segment
}

// addAzureBuildWorkspace creates a feature session containing every Git
// repository the Azure DevOps build resolved, each checked out at the exact
// commit the build used.
func addAzureBuildWorkspace(featureName, buildRef, org, project, instanceURL string) error {
	target, err := parseAzureBuildTarget(buildRef, org, project, instanceURL)
	if err != nil {
		return err
	}
	token, err := azureDevOpsToken()
	if err != nil {
		return err
	}
	client, err := azuredevops.NewClient(target.orgURL, target.project, token, nil)
	if err != nil {
		return err
	}

	repositories, err := client.ResolveBuildRepositories(context.Background(), target.buildID)
	if err != nil {
		return azureBuildError(target, err)
	}
	if len(repositories) == 0 {
		return fmt.Errorf("Azure DevOps build %d resolved no Git repositories", target.buildID)
	}

	if _, err := state.GetSession(featureName); err == nil {
		return fmt.Errorf("feature session already exists: %s", featureName)
	} else if !errors.Is(err, state.ErrSessionNotFound) {
		return err
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}
	currentState, err := state.Load()
	if err != nil {
		return err
	}
	featureDir, _, err := featureDirectoryPaths(featureName, currentState, configuration.WorkspaceBaseDir)
	if err != nil {
		return err
	}
	// Never mutate a path that already exists: it may hold unrelated user files,
	// and rollback would otherwise remove it. Require a clean feature path so
	// every path this operation touches is one it created.
	if _, err := os.Lstat(featureDir); err == nil {
		return fmt.Errorf("feature workspace path already exists: %s; remove it or choose another feature name", featureDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect feature workspace path %q: %w", featureDir, err)
	}

	for _, repository := range repositories {
		if err := addAzureRepository(featureName, repository.CloneURL, repository.Commit, repository.RefName); err != nil {
			return errors.Join(err, rollbackAzureWorkspace(featureName, featureDir))
		}
	}
	return nil
}

// rollbackAzureWorkspace removes the workspace state this operation created.
// Once a session is persisted it defers to the shared session rollback, which
// removes cloned repositories, the feature directory, and the session record.
// Before the first repository is persisted no session exists, so it removes the
// feature directory autofeat created. The caller guarantees the feature path did
// not exist beforehand, so removing it cannot delete pre-existing user files.
func rollbackAzureWorkspace(featureName, featureDir string) error {
	if _, err := state.GetSession(featureName); err == nil {
		return rollbackTemplateSession(featureName, nil)
	} else if !errors.Is(err, state.ErrSessionNotFound) {
		return err
	}
	if err := os.RemoveAll(featureDir); err != nil {
		return fmt.Errorf("roll back feature directory: %w", err)
	}
	return nil
}

// azureBuildError converts REST sentinel errors into actionable messages
// without exposing credentials. Errors that already carry endpoint context from
// the client (not-found, decode) pass through unchanged.
func azureBuildError(target azureBuildTarget, err error) error {
	if errors.Is(err, azuredevops.ErrUnauthorized) {
		return fmt.Errorf("Azure DevOps denied access to build %d in %s/%s; verify %s has Build (Read) scope for the organization: %w",
			target.buildID, target.orgURL, target.project, azureTokenEnvVars[0], err)
	}
	return err
}

// addAzureRepository clones cloneURL into the feature session and creates the
// feature branch at the exact build commit. It mirrors the remote-clone path
// but pins the checkout to a commit and never records the one-off commit as a
// remembered base reference.
func addAzureRepository(featureName, cloneURL, commit, refName string) error {
	repoName, err := remoteRepositoryName(cloneURL)
	if err != nil {
		return err
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}
	currentState, err := state.Load()
	if err != nil {
		return err
	}
	featureDir, workspaceFile, err := featureDirectoryPaths(featureName, currentState, configuration.WorkspaceBaseDir)
	if err != nil {
		return err
	}

	session, sessionExists := currentState.Sessions[featureName]
	if !sessionExists {
		session = state.Session{
			CreatedAt:     time.Now().UTC(),
			FeatureDir:    featureDir,
			WorkspaceFile: workspaceFile,
			Repos:         make([]state.Repository, 0, 1),
		}
	}
	worktreePath := filepath.Join(featureDir, repositoryDirectoryName(repoName, repositoryParentName(cloneURL), session.Repos))

	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return fmt.Errorf("create feature directory: %w", err)
	}
	// Arm cleanup before cloning so a failed or partial clone directory is
	// removed too. It stays armed until the session is durably persisted.
	repositoryPersisted := false
	defer func() {
		if repositoryPersisted {
			return
		}
		_ = os.RemoveAll(worktreePath)
	}()
	if err := gitcmd.Clone(cloneURL, worktreePath); err != nil {
		return err
	}

	if err := ensureBuildCommitAvailable(worktreePath, commit, refName); err != nil {
		return fmt.Errorf("check out build revision for repository %q: %w", repoName, err)
	}
	if err := gitcmd.CheckoutCommitAsBranch(worktreePath, featureBranchName(featureName), commit); err != nil {
		return err
	}
	if err := hooks.Run(configuration.Hooks, hooks.PostAdd, worktreePath); err != nil {
		return err
	}

	session.Repos = append(session.Repos, state.Repository{
		Name:          repoName,
		OriginalPath:  cloneURL,
		WorktreePath:  worktreePath,
		IsRemoteClone: true,
		BaseBranch:    commit,
	})
	// Persist state before writing the workspace file, so a state failure leaves
	// no orphaned workspace file, and mark the clone kept only once state is
	// durable. A later workspace-write failure is recovered by session rollback.
	currentState.Sessions[featureName] = session
	if err := state.Save(currentState); err != nil {
		return err
	}
	repositoryPersisted = true
	if err := workspace.Write(session.WorkspaceFile, repositoryDirectoryNames(session.Repos)); err != nil {
		return err
	}

	fmt.Printf("Cloned %s at %s into feature %s\n", repoName, shortCommit(commit), featureName)
	return nil
}

// ensureBuildCommitAvailable makes commit resolvable in the clone, fetching the
// build's ref (and then the commit itself) when a default clone did not include
// it, such as a pull request merge ref.
func ensureBuildCommitAvailable(worktreePath, commit, refName string) error {
	present, err := gitcmd.CommitPresent(worktreePath, commit)
	if err != nil {
		return err
	}
	if present {
		return nil
	}

	if strings.TrimSpace(refName) != "" {
		if err := gitcmd.FetchRef(worktreePath, refName); err == nil {
			present, err = gitcmd.CommitPresent(worktreePath, commit)
			if err != nil {
				return err
			}
			if present {
				return nil
			}
		}
	}

	// Fetching by object name only succeeds when the server allows it; ignore a
	// failure and fall through to the availability check.
	_ = gitcmd.FetchRef(worktreePath, commit)
	present, err = gitcmd.CommitPresent(worktreePath, commit)
	if err != nil {
		return err
	}
	if present {
		return nil
	}

	return fmt.Errorf("build commit %s is not available after cloning and fetching %q; it may reference a ref that is not fetched, such as a pull request merge", commit, refName)
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
