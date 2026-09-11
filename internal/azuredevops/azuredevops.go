// Package azuredevops resolves the Git repositories and commit versions used by
// an Azure DevOps build (pipeline run) through the Azure DevOps REST API.
//
// Commit versions come from the pipeline run, which is authoritative for the
// exact revision each repository was built at. Repository identity (name and
// clone URL) comes from sources that supply it under the documented contract:
// the Build API's repository object for the primary ("self") repository, and
// the run's expanded pipeline definition (finalYaml) for repository resources,
// whose run entries expose only a type. Each run resource alias is correlated
// with its declaration in finalYaml to determine the repository name and type,
// while the exact commit still comes from the run.
//
// The package never logs, embeds, or returns the personal access token it is
// given. Authentication uses HTTP Basic with an empty username, matching the
// Azure DevOps convention for PAT authentication. Every endpoint used here is
// covered by the Build (Read) scope.
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

	"gopkg.in/yaml.v3"
)

// Sentinel errors let callers translate REST failures into actionable messages
// without inspecting HTTP status codes directly.
var (
	// ErrUnauthorized indicates the credentials were rejected (HTTP 401/403).
	ErrUnauthorized = errors.New("azure devops access was denied")
	// ErrNotFound indicates the requested resource does not exist (HTTP 404).
	ErrNotFound = errors.New("azure devops resource was not found")
	// ErrUnsupportedRepositoryType indicates a resolved repository is not a Git
	// repository autofeat can clone.
	ErrUnsupportedRepositoryType = errors.New("unsupported repository type")
	// ErrUnresolvableRepository indicates a repository whose identity could not
	// be determined from any documented source.
	ErrUnresolvableRepository = errors.New("could not resolve repository identity")
	// ErrMissingVersion indicates a repository was resolved without a commit.
	ErrMissingVersion = errors.New("build did not resolve a commit version")
	// ErrConflictingVersions indicates the same repository was resolved to two
	// different commits within one build.
	ErrConflictingVersions = errors.New("repository resolved to conflicting commits")
)

const apiVersion = "7.1"

// Build process types reported by the Build Definitions API.
const (
	processTypeDesigner = 1 // classic, single-repository builds
	processTypeYAML     = 2 // YAML pipelines, which may declare repository resources
)

// Repository identifies a repository referenced by a build.
type Repository struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// RepositoryResource is a repository resource resolved for a pipeline run. Under
// the documented contract its Repository carries only a type; the exact commit
// is Version.
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

// Definition is the subset of a build definition used to tell classic builds
// from YAML pipelines.
type Definition struct {
	ID          int
	ProcessType int
}

// Run holds the repository resources resolved for a pipeline run and the run's
// expanded pipeline definition.
type Run struct {
	Repositories map[string]RepositoryResource
	FinalYAML    string
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

// ResolveBuildRepositories resolves every Git repository the build used. The
// primary repository always comes from the build. YAML pipelines additionally
// contribute their resolved repository resources; classic (designer) builds
// have only the primary repository.
func (c *Client) ResolveBuildRepositories(ctx context.Context, buildID int) ([]ResolvedRepository, error) {
	build, err := c.GetBuild(ctx, buildID)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("Azure DevOps build %d was not found: %w", buildID, err)
	}
	if err != nil {
		return nil, err
	}

	if build.PipelineID == 0 {
		return resolveRepositories(c.orgURL, c.project, build, nil)
	}

	definition, err := c.GetDefinition(ctx, build.PipelineID)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("Azure DevOps pipeline definition %d for build %d was not found: %w", build.PipelineID, buildID, err)
	}
	if err != nil {
		return nil, err
	}
	if definition.ProcessType == processTypeDesigner {
		// Classic builds cannot declare repository resources.
		return resolveRepositories(c.orgURL, c.project, build, nil)
	}

	run, err := c.GetRun(ctx, build.PipelineID, buildID)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("Azure DevOps pipeline run for build %d was not found; its repository resources cannot be resolved", buildID)
	}
	if err != nil {
		return nil, err
	}
	return resolveRepositories(c.orgURL, c.project, build, &run)
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

// GetDefinition fetches a build definition's process type.
func (c *Client) GetDefinition(ctx context.Context, definitionID int) (Definition, error) {
	endpoint := fmt.Sprintf("%s/%s/_apis/build/definitions/%d", c.orgURL, url.PathEscape(c.project), definitionID)
	var payload struct {
		ID      int `json:"id"`
		Process struct {
			Type int `json:"type"`
		} `json:"process"`
	}
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return Definition{}, err
	}
	return Definition{ID: payload.ID, ProcessType: payload.Process.Type}, nil
}

// GetRun fetches the repository resources and expanded YAML of a pipeline run.
func (c *Client) GetRun(ctx context.Context, pipelineID, runID int) (Run, error) {
	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines/%d/runs/%d", c.orgURL, url.PathEscape(c.project), pipelineID, runID)
	var payload struct {
		FinalYAML string `json:"finalYaml"`
		Resources struct {
			Repositories map[string]RepositoryResource `json:"repositories"`
		} `json:"resources"`
	}
	if err := c.get(ctx, endpoint, &payload); err != nil {
		return Run{}, err
	}
	return Run{Repositories: payload.Resources.Repositories, FinalYAML: payload.FinalYAML}, nil
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

// repositoryDeclaration is a repository resource declaration parsed from a run's
// expanded pipeline YAML.
type repositoryDeclaration struct {
	Alias string `yaml:"repository"`
	Type  string `yaml:"type"`
	Name  string `yaml:"name"`
	Ref   string `yaml:"ref"`
}

// parseRepositoryDeclarations extracts resources.repositories declarations from
// a run's expanded pipeline YAML, keyed by alias.
func parseRepositoryDeclarations(finalYAML string) (map[string]repositoryDeclaration, error) {
	declarations := make(map[string]repositoryDeclaration)
	if strings.TrimSpace(finalYAML) == "" {
		return declarations, nil
	}
	var document struct {
		Resources struct {
			Repositories []repositoryDeclaration `yaml:"repositories"`
		} `yaml:"resources"`
	}
	if err := yaml.Unmarshal([]byte(finalYAML), &document); err != nil {
		return nil, fmt.Errorf("parse pipeline YAML repository resources: %w", err)
	}
	for _, declaration := range document.Resources.Repositories {
		if alias := strings.TrimSpace(declaration.Alias); alias != "" {
			declarations[alias] = declaration
		}
	}
	return declarations, nil
}

// resolveRepositories combines a build's primary repository with the resolved
// repository resources of its run, returning one entry per distinct repository.
// The primary repository is always first. Commit versions come from the run (or
// the build's source version for the primary). Resource identity comes from the
// run's expanded pipeline YAML declarations. Repositories that appear both as
// the primary and as a resource, or as repeated resources, are collapsed when
// they share a commit and rejected when they resolve to conflicting commits.
func resolveRepositories(orgURL, project string, build Build, run *Run) ([]ResolvedRepository, error) {
	var declarations map[string]repositoryDeclaration
	if run != nil {
		parsed, err := parseRepositoryDeclarations(run.FinalYAML)
		if err != nil {
			return nil, err
		}
		declarations = parsed
	}

	primaryCommit := build.SourceVersion
	primaryRef := build.SourceBranch
	if run != nil {
		if self, ok := run.Repositories["self"]; ok {
			if strings.TrimSpace(self.Version) != "" {
				primaryCommit = self.Version
			}
			if strings.TrimSpace(self.RefName) != "" {
				primaryRef = self.RefName
			}
		}
	}

	resolved := make([]ResolvedRepository, 0)
	indexByURL := make(map[string]int)

	addEntry := func(alias, name, repoType, cloneURL, commit, refName string) error {
		if strings.TrimSpace(commit) == "" {
			return fmt.Errorf("%w: repository resource %q", ErrMissingVersion, alias)
		}
		key := normalizeCloneURL(cloneURL)
		if index, ok := indexByURL[key]; ok {
			if !strings.EqualFold(resolved[index].Commit, commit) {
				return fmt.Errorf("%w: %s resolves to %s and %s", ErrConflictingVersions, cloneURL, resolved[index].Commit, commit)
			}
			return nil
		}
		indexByURL[key] = len(resolved)
		resolved = append(resolved, ResolvedRepository{
			Alias:    alias,
			Name:     name,
			Type:     repoType,
			CloneURL: cloneURL,
			Commit:   commit,
			RefName:  refName,
		})
		return nil
	}

	// Primary repository: identity from the Build API. Validate its type before
	// accepting the URL, so an unsupported primary (for example TFVC) fails with
	// an actionable error rather than a generic clone failure, consistent with
	// how resource declarations are validated.
	if !isSupportedGitType(build.Repository.Type) {
		return nil, fmt.Errorf("%w: %q (primary repository %q)", ErrUnsupportedRepositoryType, build.Repository.Type, build.Repository.Name)
	}
	primaryURL := cloneURLFromBuildRepository(build.Repository)
	if primaryURL == "" {
		return nil, fmt.Errorf("%w: primary repository %q has no clone URL", ErrUnresolvableRepository, build.Repository.Name)
	}
	if err := addEntry("self", build.Repository.Name, build.Repository.Type, primaryURL, primaryCommit, primaryRef); err != nil {
		return nil, err
	}

	if run == nil {
		return resolved, nil
	}

	// Reconcile in both directions over the union of run resource aliases and
	// finalYaml declarations (excluding the implicit self): every run resource
	// must have a declaration to be identified, and every declared repository
	// must have a run-resolved version, so a declared repository the run did not
	// resolve fails with a missing-version error instead of being dropped.
	for _, alias := range unionAliases(run.Repositories, declarations) {
		declaration, hasDeclaration := declarations[alias]
		if !hasDeclaration {
			return nil, fmt.Errorf("%w: repository resource %q has no declaration in the pipeline definition", ErrUnresolvableRepository, alias)
		}
		cloneURL, err := cloneURLFromDeclaration(orgURL, project, declaration)
		if err != nil {
			return nil, fmt.Errorf("repository resource %q: %w", alias, err)
		}
		resource := run.Repositories[alias]
		if err := addEntry(alias, declaration.Name, declaration.Type, cloneURL, resource.Version, resource.RefName); err != nil {
			return nil, err
		}
	}

	return resolved, nil
}

// unionAliases returns the sorted set of resource aliases across the run's
// resolved repositories and the pipeline declarations, excluding the implicit
// self repository.
func unionAliases(repositories map[string]RepositoryResource, declarations map[string]repositoryDeclaration) []string {
	seen := make(map[string]struct{}, len(repositories)+len(declarations))
	for alias := range repositories {
		if alias != "self" {
			seen[alias] = struct{}{}
		}
	}
	for alias := range declarations {
		if alias != "self" {
			seen[alias] = struct{}{}
		}
	}
	aliases := make([]string, 0, len(seen))
	for alias := range seen {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

// isSupportedGitType reports whether a Build API repository type is a Git
// repository autofeat can clone. TFVC and Subversion are not.
func isSupportedGitType(repoType string) bool {
	switch strings.ToLower(strings.TrimSpace(repoType)) {
	case "tfsgit", "azurereposgit", "git", "externalgit",
		"github", "githubenterprise", "bitbucket":
		return true
	default:
		return false
	}
}

// cloneURLFromBuildRepository returns the clone URL the Build API reports for
// the primary repository.
func cloneURLFromBuildRepository(repo Repository) string {
	if isCloneURL(strings.TrimSpace(repo.URL)) {
		return strings.TrimSpace(repo.URL)
	}
	return ""
}

// cloneURLFromDeclaration derives an HTTPS clone URL for a repository resource
// from its pipeline YAML declaration. The declared name is authoritative: for
// Azure Repos Git it may name another project as "Project/Repository".
func cloneURLFromDeclaration(orgURL, project string, declaration repositoryDeclaration) (string, error) {
	name := strings.TrimSpace(declaration.Name)
	if name == "" {
		return "", fmt.Errorf("%w: declaration has no repository name", ErrUnresolvableRepository)
	}
	switch strings.ToLower(strings.TrimSpace(declaration.Type)) {
	case "git":
		// Azure Repos Git in the same organization; the project defaults to the
		// build's project and may be overridden by a "Project/Repository" name.
		repoProject := project
		repoName := name
		if slash := strings.Index(name, "/"); slash >= 0 {
			repoProject = name[:slash]
			repoName = name[slash+1:]
		}
		if strings.TrimSpace(repoName) == "" {
			return "", fmt.Errorf("%w: azure repos declaration has no repository name", ErrUnresolvableRepository)
		}
		return orgURL + "/" + url.PathEscape(repoProject) + "/_git/" + url.PathEscape(repoName), nil
	case "github":
		return "https://github.com/" + name + ".git", nil
	case "bitbucket":
		return "https://bitbucket.org/" + name + ".git", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedRepositoryType, declaration.Type)
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
