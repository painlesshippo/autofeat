// Package azuredevops resolves the Git repositories and commit versions used by
// an Azure DevOps build (pipeline run) through the Azure DevOps REST API.
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
	// ErrMissingVersion indicates a repository was resolved without a commit.
	ErrMissingVersion = errors.New("build did not resolve a commit version")
	// ErrConflictingVersions indicates the same repository was resolved to two
	// different commits within one build.
	ErrConflictingVersions = errors.New("repository resolved to conflicting commits")
)

const apiVersion = "7.1"

// Repository identifies a repository referenced by a build or run.
type Repository struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	URL  string `json:"url"`
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

	return ResolveRepositories(c.orgURL, c.project, build, run)
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

// ResolveRepositories combines a build's primary repository with the resolved
// repository resources of its run, returning one entry per distinct repository.
// The primary repository is always first. Repositories that appear both as the
// primary and as a resource, or as repeated resources, are collapsed when they
// share a commit and rejected when they resolve to conflicting commits.
func ResolveRepositories(orgURL, project string, build Build, run *Run) ([]ResolvedRepository, error) {
	type entry struct {
		alias   string
		repo    Repository
		commit  string
		refName string
	}

	primaryRepo := build.Repository
	primaryCommit := build.SourceVersion
	primaryRef := build.SourceBranch
	if run != nil {
		if self, ok := run.Repositories["self"]; ok {
			// The build's repository carries the clone URL, so it is preferred;
			// the run's self entry is the fallback when the build omits it.
			if strings.TrimSpace(primaryRepo.Name) == "" && strings.TrimSpace(primaryRepo.URL) == "" {
				primaryRepo = self.Repository
			}
			if strings.TrimSpace(self.Version) != "" {
				primaryCommit = self.Version
			}
			if strings.TrimSpace(self.RefName) != "" {
				primaryRef = self.RefName
			}
		}
	}

	entries := []entry{{alias: "self", repo: primaryRepo, commit: primaryCommit, refName: primaryRef}}
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
			entries = append(entries, entry{alias: alias, repo: resource.Repository, commit: resource.Version, refName: resource.RefName})
		}
	}

	resolved := make([]ResolvedRepository, 0, len(entries))
	indexByURL := make(map[string]int, len(entries))
	for _, item := range entries {
		if strings.TrimSpace(item.commit) == "" {
			return nil, fmt.Errorf("%w: repository %q (alias %q)", ErrMissingVersion, item.repo.Name, item.alias)
		}
		cloneURL, err := repositoryCloneURL(orgURL, project, item.repo)
		if err != nil {
			return nil, fmt.Errorf("repository %q (alias %q): %w", item.repo.Name, item.alias, err)
		}
		key := normalizeCloneURL(cloneURL)
		if index, ok := indexByURL[key]; ok {
			if !strings.EqualFold(resolved[index].Commit, item.commit) {
				return nil, fmt.Errorf("%w: %s resolves to %s and %s", ErrConflictingVersions, cloneURL, resolved[index].Commit, item.commit)
			}
			continue
		}
		indexByURL[key] = len(resolved)
		resolved = append(resolved, ResolvedRepository{
			Alias:    item.alias,
			Name:     item.repo.Name,
			Type:     item.repo.Type,
			CloneURL: cloneURL,
			Commit:   item.commit,
			RefName:  item.refName,
		})
	}

	return resolved, nil
}

// repositoryCloneURL derives an HTTPS clone URL for a resolved repository. An
// explicit URL from the API is preferred; otherwise the URL is constructed from
// the repository type and name for the common hosted providers.
func repositoryCloneURL(orgURL, project string, repo Repository) (string, error) {
	if explicit := strings.TrimSpace(repo.URL); isCloneURL(explicit) {
		return explicit, nil
	}

	name := strings.TrimSpace(repo.Name)
	if name == "" {
		return "", fmt.Errorf("%w: repository has no name or clone URL", ErrUnsupportedRepositoryType)
	}

	switch strings.ToLower(strings.TrimSpace(repo.Type)) {
	case "tfsgit", "azurereposgit":
		repoProject := project
		repoName := name
		// A resource may reference another project as "Project/Repository".
		if slash := strings.Index(name, "/"); slash >= 0 {
			repoProject = name[:slash]
			repoName = name[slash+1:]
		}
		if strings.TrimSpace(repoName) == "" {
			return "", fmt.Errorf("%w: azure repos resource has no repository name", ErrUnsupportedRepositoryType)
		}
		return orgURL + "/" + url.PathEscape(repoProject) + "/_git/" + url.PathEscape(repoName), nil
	case "github", "githubenterprise":
		// GitHub resources name the repository as "owner/repository". Enterprise
		// hosts are only supported when the API supplies an explicit clone URL.
		if strings.ToLower(repo.Type) == "githubenterprise" {
			return "", fmt.Errorf("%w: %q requires an explicit clone URL", ErrUnsupportedRepositoryType, repo.Type)
		}
		return "https://github.com/" + name + ".git", nil
	case "bitbucket":
		return "https://bitbucket.org/" + name + ".git", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedRepositoryType, repo.Type)
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
