// Package azuredevops resolves the Git repositories and commit versions used by
// an Azure DevOps build (pipeline run) through the Azure DevOps REST API.
//
// Commit versions come from the pipeline run, which is authoritative for the
// exact revision each repository was built at. Repository identity (name and
// clone URL) comes from sources that actually supply it: the Build API's
// repository object for the primary ("self") repository, and the Git
// Repositories API for Azure Repos Git resources that the run identifies only
// by id. The run response's repository object is not assumed to carry a name or
// clone URL, matching the documented Pipelines Runs contract.
//
// The package never logs, embeds, or returns the personal access token it is
// given. Authentication uses HTTP Basic with an empty username, matching the
// Azure DevOps convention for PAT authentication.
package azuredevops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors let callers translate REST failures into actionable messages
// without inspecting HTTP status codes directly.
var (
	// ErrUnauthorized indicates the credentials were rejected (HTTP 401/403).
	ErrUnauthorized = errors.New("azure devops access was denied")
	// ErrNotFound indicates the requested build or run does not exist (HTTP 404).
	ErrNotFound = errors.New("azure devops resource was not found")
	// ErrUnsupportedRepositoryType indicates a resolved repository is not a Git
	// repository autofeat can clone.
	ErrUnsupportedRepositoryType = errors.New("unsupported repository type")
	// ErrUnresolvableRepository indicates a repository whose clone URL could not
	// be determined from any available source.
	ErrUnresolvableRepository = errors.New("could not resolve repository clone URL")
	// ErrMissingVersion indicates a repository was resolved without a commit.
	ErrMissingVersion = errors.New("build did not resolve a commit version")
	// ErrConflictingVersions indicates the same repository was resolved to two
	// different commits within one build.
	ErrConflictingVersions = errors.New("repository resolved to conflicting commits")
)

const apiVersion = "7.1"

// Repository identifies a repository referenced by a build, run, or the Git
// Repositories API. Different sources populate different fields: the Build API
// supplies Type/Name/URL, a run resource may supply only ID and Type, and the
// Git Repositories API supplies Name and RemoteURL.
type Repository struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	RemoteURL string `json:"remoteUrl"`
}

// RepositoryResource is a repository resource resolved for a pipeline run,
// including the exact commit (Version) the run used.
type RepositoryResource struct {
	Repository Repository `json:"repository"`
	RefName    string     `json:"refName"`
	Version    string     `json:"version"`
}

// Build is the subset of an Azure DevOps build used to resolve repositories.
type Build struct {
	ID            int
	PipelineID    int
	Repository    Repository
	SourceVersion string
	SourceBranch  string
}

// Run holds the repository resources resolved for a pipeline run.
type Run struct {
	Repositories map[string]RepositoryResource
}

// ResolvedRepository is a repository the build used, paired with its exact
// clone URL and commit. Alias is the pipeline resource alias ("self" for the
// primary repository).
type ResolvedRepository struct {
	Alias    string
	Name     string
	Type     string
	CloneURL string
	Commit   string
	RefName  string
}

// repositoryLookup resolves a repository's identity from its id. It is injected
// so resolution can be unit-tested without HTTP.
type repositoryLookup func(ctx context.Context, id string) (Repository, error)

// Client calls the Azure DevOps REST API for one organization and project.
type Client struct {
	orgURL     string
	project    string
	pat        string
	httpClient *http.Client
}

// NewClient builds a client for orgURL (for example
// https://dev.azure.com/myorg) and project. The personal access token is
// required and is only ever sent as an Authorization header.
func NewClient(orgURL, project, pat string, httpClient *http.Client) (*Client, error) {
	orgURL = strings.TrimRight(strings.TrimSpace(orgURL), "/")
	if orgURL == "" {
		return nil, errors.New("azure devops organization URL is required")
	}
	if strings.TrimSpace(project) == "" {
		return nil, errors.New("azure devops project is required")
	}
	if strings.TrimSpace(pat) == "" {
		return nil, errors.New("azure devops personal access token is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{orgURL: orgURL, project: project, pat: pat, httpClient: httpClient}, nil
}

// ResolveBuildRepositories resolves every Git repository the build used,
// starting from its primary repository and adding any repository resources.
func (c *Client) ResolveBuildRepositories(ctx context.Context, buildID int) ([]ResolvedRepository, error) {
	build, err := c.GetBuild(ctx, buildID)
	if err != nil {
		return nil, err
	}

	var run *Run
	if build.PipelineID != 0 {
		fetched, err := c.GetRun(ctx, build.PipelineID, buildID)
		if errors.Is(err, ErrNotFound) {
			// Classic (non-YAML) builds expose no pipeline run resources; the
			// primary repository from the build is authoritative.
			run = nil
		} else if err != nil {
			return nil, err
		} else {
			run = &fetched
		}
	}

	return resolveRepositories(ctx, build, run, c.GetRepository)
}

// GetBuild fetches the build identified by buildID.
func (c *Client) GetBuild(ctx context.Context, buildID int) (Build, error) {
	endpoint := fmt.Sprintf("%s/%s/_apis/build/builds/%d", c.orgURL, url.PathEscape(c.project), buildID)
	var payload struct {
		ID         int `json:"id"`
		Definition struct {
			ID int `json:"id"`
		} `json:"definition"`
		Repository    Repository `json:"repository"`
		SourceVersion string     `json:"sourceVersion"`
		SourceBranch  string     `json:"sourceBranch"`
	}
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return Build{}, err
	}
	return Build{
		ID:            payload.ID,
		PipelineID:    payload.Definition.ID,
		Repository:    payload.Repository,
		SourceVersion: payload.SourceVersion,
		SourceBranch:  payload.SourceBranch,
	}, nil
}

// GetRun fetches the repository resources resolved for a pipeline run.
func (c *Client) GetRun(ctx context.Context, pipelineID, runID int) (Run, error) {
	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines/%d/runs/%d", c.orgURL, url.PathEscape(c.project), pipelineID, runID)
	var payload struct {
		Resources struct {
			Repositories map[string]RepositoryResource `json:"repositories"`
		} `json:"resources"`
	}
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return Run{}, err
	}
	return Run{Repositories: payload.Resources.Repositories}, nil
}

// GetRepository fetches an Azure Repos Git repository's authoritative identity,
// including its clone (remote) URL, by id.
func (c *Client) GetRepository(ctx context.Context, id string) (Repository, error) {
	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories/%s", c.orgURL, url.PathEscape(c.project), url.PathEscape(id))
	var payload struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		RemoteURL string `json:"remoteUrl"`
	}
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return Repository{}, err
	}
	return Repository{
		ID:        payload.ID,
		Type:      "azureReposGit",
		Name:      payload.Name,
		URL:       payload.URL,
		RemoteURL: payload.RemoteURL,
	}, nil
}

func (c *Client) get(ctx context.Context, endpoint string, out any) error {
	requestURL := endpoint + "?api-version=" + apiVersion
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build azure devops request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	// Azure DevOps accepts a PAT as the HTTP Basic password with an empty user.
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+c.pat)))

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call azure devops %s: %w", endpoint, err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			return fmt.Errorf("decode azure devops response from %s: %w", endpoint, err)
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNonAuthoritativeInfo:
		return fmt.Errorf("%w: %s (%s)", ErrUnauthorized, endpoint, response.Status)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, endpoint)
	default:
		snippet := errorSnippet(response.Body)
		if snippet != "" {
			return fmt.Errorf("azure devops request to %s failed: %s: %s", endpoint, response.Status, snippet)
		}
		return fmt.Errorf("azure devops request to %s failed: %s", endpoint, response.Status)
	}
}

// errorSnippet returns a short, single-line excerpt of an error body. It never
// contains credentials because the token is only sent in request headers.
func errorSnippet(body io.Reader) string {
	const limit = 512
	data, err := io.ReadAll(io.LimitReader(body, limit))
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(data)), " ")
}

// resolveRepositories combines a build's primary repository with the resolved
// repository resources of its run, returning one entry per distinct repository.
// The primary repository is always first. Commit versions come from the run
// (or the build's source version for the primary); repository identity comes
// from the build repository object or, for run resources identified only by id,
// from the injected lookup. Repositories that appear both as the primary and as
// a resource, or as repeated resources, are collapsed when they share a commit
// and rejected when they resolve to conflicting commits.
func resolveRepositories(ctx context.Context, build Build, run *Run, lookup repositoryLookup) ([]ResolvedRepository, error) {
	primaryRepo := build.Repository
	primaryCommit := build.SourceVersion
	primaryRef := build.SourceBranch
	if run != nil {
		if self, ok := run.Repositories["self"]; ok {
			// The run is authoritative for the commit and ref. The build's
			// repository object carries identity (URL); only fall back to the
			// run's self repository when the build omits it.
			if strings.TrimSpace(self.Version) != "" {
				primaryCommit = self.Version
			}
			if strings.TrimSpace(self.RefName) != "" {
				primaryRef = self.RefName
			}
			if strings.TrimSpace(primaryRepo.ID) == "" && cloneURLFromRepository(primaryRepo) == "" {
				primaryRepo = self.Repository
			}
		}
	}

	resolved := make([]ResolvedRepository, 0)
	indexByIdentity := make(map[string]int)

	addEntry := func(alias string, repo Repository, commit, refName string) error {
		if strings.TrimSpace(commit) == "" {
			return fmt.Errorf("%w: repository resource %q", ErrMissingVersion, alias)
		}
		if !isSupportedGitType(repo.Type) {
			return fmt.Errorf("%w: %q (repository resource %q)", ErrUnsupportedRepositoryType, repo.Type, alias)
		}

		cloneURL := cloneURLFromRepository(repo)
		if cloneURL == "" && isAzureReposType(repo.Type) && strings.TrimSpace(repo.ID) != "" {
			fetched, err := lookup(ctx, repo.ID)
			if err != nil {
				return fmt.Errorf("resolve repository for resource %q (id %s): %w", alias, repo.ID, err)
			}
			if strings.TrimSpace(repo.Name) == "" {
				repo.Name = fetched.Name
			}
			cloneURL = cloneURLFromRepository(fetched)
		}
		if cloneURL == "" {
			return fmt.Errorf("%w: repository resource %q (type %q) does not expose a clone URL and could not be identified", ErrUnresolvableRepository, alias, repo.Type)
		}

		identity := repositoryIdentity(repo.ID, cloneURL)
		if index, ok := indexByIdentity[identity]; ok {
			if !strings.EqualFold(resolved[index].Commit, commit) {
				return fmt.Errorf("%w: %s resolves to %s and %s", ErrConflictingVersions, cloneURL, resolved[index].Commit, commit)
			}
			return nil
		}
		indexByIdentity[identity] = len(resolved)
		resolved = append(resolved, ResolvedRepository{
			Alias:    alias,
			Name:     repo.Name,
			Type:     repo.Type,
			CloneURL: cloneURL,
			Commit:   commit,
			RefName:  refName,
		})
		return nil
	}

	if err := addEntry("self", primaryRepo, primaryCommit, primaryRef); err != nil {
		return nil, err
	}
	if run != nil {
		aliases := make([]string, 0, len(run.Repositories))
		for alias := range run.Repositories {
			if alias == "self" {
				continue
			}
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			resource := run.Repositories[alias]
			if err := addEntry(alias, resource.Repository, resource.Version, resource.RefName); err != nil {
				return nil, err
			}
		}
	}

	return resolved, nil
}

// cloneURLFromRepository returns the first field that looks like a Git clone
// URL, preferring the Git Repositories API's remoteUrl over a build url.
func cloneURLFromRepository(repo Repository) string {
	for _, candidate := range []string{repo.RemoteURL, repo.URL} {
		if trimmed := strings.TrimSpace(candidate); isCloneURL(trimmed) {
			return trimmed
		}
	}
	return ""
}

// repositoryIdentity returns a stable dedup key. A repository id is preferred
// because it is stable across clone-URL spellings; otherwise the normalized
// clone URL is used.
func repositoryIdentity(id, cloneURL string) string {
	if trimmed := strings.TrimSpace(id); trimmed != "" {
		return "id:" + strings.ToLower(trimmed)
	}
	return "url:" + normalizeCloneURL(cloneURL)
}

func isSupportedGitType(repoType string) bool {
	switch strings.ToLower(strings.TrimSpace(repoType)) {
	case "tfsgit", "azurereposgit", "azurereposgithyphenated",
		"github", "githubenterprise", "bitbucket", "git", "externalgit":
		return true
	default:
		return false
	}
}

func isAzureReposType(repoType string) bool {
	switch strings.ToLower(strings.TrimSpace(repoType)) {
	case "tfsgit", "azurereposgit", "azurereposgithyphenated":
		return true
	default:
		return false
	}
}

func isCloneURL(value string) bool {
	return strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "git@") ||
		strings.HasPrefix(value, "ssh://")
}

func normalizeCloneURL(value string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	return strings.ToLower(trimmed)
}

// ParseBuildID parses a positive build identifier.
func ParseBuildID(value string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid azure devops build id %q", value)
	}
	return id, nil
}
