// Package artifact records external things an agent produced during a feature
// session (builds, work items, pull requests, cloud resources, links) and
// infers their type offline from the reference the agent supplies.
package artifact

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/painlesshippo/autofeat/internal/azuredevops"
)

// Type classifies an artifact.
type Type string

// Supported artifact types.
const (
	ADOBuild          Type = "ado-build"
	ADOWorkItem       Type = "ado-work-item"
	ADOPullRequest    Type = "ado-pull-request"
	ADORepo           Type = "ado-repo"
	ADOCommit         Type = "ado-commit"
	ADOBranch         Type = "ado-branch"
	AzureResource     Type = "azure-resource"
	GitHubPullRequest Type = "github-pull-request"
	GitHubIssue       Type = "github-issue"
	GitHubCommit      Type = "github-commit"
	GitHubRun         Type = "github-run"
	Link              Type = "link"
)

var types = []Type{
	ADOBuild, ADOWorkItem, ADOPullRequest, ADORepo, ADOCommit, ADOBranch,
	AzureResource,
	GitHubPullRequest, GitHubIssue, GitHubCommit, GitHubRun,
	Link,
}

// ErrUnrecognized indicates a reference whose type cannot be inferred.
var ErrUnrecognized = errors.New("cannot infer artifact type")

var (
	workItemShorthand    = regexp.MustCompile(`(?i)^AB#([0-9]+)$`)
	pullRequestShorthand = regexp.MustCompile(`^!([0-9]+)$`)
)

// Artifact is an external item recorded on a feature session.
type Artifact struct {
	Type      Type      `json:"type"`
	Value     string    `json:"value"`
	ID        string    `json:"id,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Types returns every supported artifact type.
func Types() []Type {
	return append([]Type(nil), types...)
}

// ParseType validates a type name.
func ParseType(value string) (Type, error) {
	for _, candidate := range types {
		if string(candidate) == value {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("unknown artifact type %q (valid: %s)", value, typeList())
}

// Parse builds an artifact from value. When forced is empty the type is
// inferred; otherwise forced is used and must not contradict an inferred
// specific type.
func Parse(value string, forced Type) (Artifact, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Artifact{}, errors.New("artifact reference is required")
	}

	inferredType, id, err := Infer(value)
	if forced == "" {
		if err != nil {
			return Artifact{}, err
		}
		return Artifact{Type: inferredType, Value: value, ID: id}, nil
	}

	if _, typeErr := ParseType(string(forced)); typeErr != nil {
		return Artifact{}, typeErr
	}
	if err == nil && inferredType != Link && forced != Link && inferredType != forced {
		return Artifact{}, fmt.Errorf("artifact %q looks like %s, not %s", value, inferredType, forced)
	}
	if err != nil || inferredType != forced {
		id = ""
		if forced != Link {
			id = value
		}
	}
	return Artifact{Type: forced, Value: value, ID: id}, nil
}

// Infer returns the type of value and the identifier extracted from it.
func Infer(value string) (Type, string, error) {
	value = strings.TrimSpace(value)

	if match := workItemShorthand.FindStringSubmatch(value); match != nil {
		return ADOWorkItem, match[1], nil
	}
	if match := pullRequestShorthand.FindStringSubmatch(value); match != nil {
		return ADOPullRequest, match[1], nil
	}
	if id, ok := armID(value); ok {
		return AzureResource, id, nil
	}

	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", "", fmt.Errorf("%w from %q; pass --type (valid: %s)", ErrUnrecognized, value, typeList())
	}

	if artifactType, id, ok := inferAzurePortal(parsed); ok {
		return artifactType, id, nil
	}
	if artifactType, id, ok := inferAzureDevOps(parsed); ok {
		return artifactType, id, nil
	}
	if artifactType, id, ok := inferGitHub(parsed); ok {
		return artifactType, id, nil
	}
	return Link, "", nil
}

// Key identifies an artifact for duplicate detection and removal. URLs are
// compared with a case-insensitive scheme and host only; references that are
// not URLs (plain ids, shorthands) are scoped to the artifact's repository.
func (a Artifact) Key() string {
	if a.Type == AzureResource && a.ID != "" {
		return string(a.Type) + "\x00" + strings.ToLower(a.ID)
	}
	if reference, ok := normalizeURL(a.Value); ok {
		return string(a.Type) + "\x00" + reference
	}
	value := strings.TrimSpace(a.Value)
	if match := workItemShorthand.FindStringSubmatch(value); match != nil {
		value = "AB#" + match[1]
	}
	return string(a.Type) + "\x00" + a.Repo + "\x00" + value
}

func normalizeURL(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return parsed.String(), true
}

// Validate verifies that a persisted artifact is well formed.
func (a Artifact) Validate() error {
	if _, err := ParseType(string(a.Type)); err != nil {
		return err
	}
	if strings.TrimSpace(a.Value) == "" {
		return errors.New("value is required")
	}
	return nil
}

func inferAzureDevOps(parsed *url.URL) (Type, string, bool) {
	orgURL, rest, ok := azuredevops.SplitWebURL(parsed)
	if !ok || orgURL == "" || len(rest) < 2 {
		return "", "", false
	}

	tail := rest[1:]
	switch tail[0] {
	case "_build":
		if id, err := azuredevops.ParseBuildID(parsed.Query().Get("buildId")); err == nil {
			return ADOBuild, strconv.Itoa(id), true
		}
	case "_workitems":
		if len(tail) >= 3 && tail[1] == "edit" && isPositiveInteger(tail[2]) {
			return ADOWorkItem, tail[2], true
		}
	case "_git":
		if len(tail) < 2 {
			return "", "", false
		}
		version := parsed.Query().Get("version")
		switch {
		case len(tail) >= 4 && strings.EqualFold(tail[2], "pullrequest") && isPositiveInteger(tail[3]):
			return ADOPullRequest, tail[3], true
		case len(tail) >= 4 && tail[2] == "commit":
			return ADOCommit, tail[3], true
		case len(tail) == 2 && strings.HasPrefix(version, "GB") && len(version) > 2:
			return ADOBranch, strings.TrimPrefix(version, "GB"), true
		case len(tail) == 2:
			return ADORepo, decode(tail[1]), true
		}
	}
	return "", "", false
}

func inferGitHub(parsed *url.URL) (Type, string, bool) {
	if !strings.EqualFold(parsed.Host, "github.com") && !strings.EqualFold(parsed.Host, "www.github.com") {
		return "", "", false
	}
	segments := splitPath(parsed.Path)
	if len(segments) < 4 {
		return "", "", false
	}

	switch {
	case segments[2] == "pull" && isPositiveInteger(segments[3]):
		return GitHubPullRequest, segments[3], true
	case segments[2] == "issues" && isPositiveInteger(segments[3]):
		return GitHubIssue, segments[3], true
	case segments[2] == "commit":
		return GitHubCommit, segments[3], true
	case len(segments) >= 5 && segments[2] == "actions" && segments[3] == "runs" && isPositiveInteger(segments[4]):
		return GitHubRun, segments[4], true
	}
	return "", "", false
}

func inferAzurePortal(parsed *url.URL) (Type, string, bool) {
	host := strings.ToLower(parsed.Host)
	if host != "portal.azure.com" && !strings.HasSuffix(host, ".portal.azure.com") {
		return "", "", false
	}
	fragment := parsed.Fragment
	index := strings.Index(strings.ToLower(fragment), "/subscriptions/")
	if index < 0 {
		return "", "", false
	}
	if id, ok := armID(fragment[index:]); ok {
		return AzureResource, id, true
	}
	return "", "", false
}

// armID extracts an Azure Resource Manager id of at least resource group scope
// from value. Segments after the resource group or after the last complete
// type/name pair (such as a portal blade) are dropped.
func armID(value string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(value), "/subscriptions/") {
		return "", false
	}
	segments := splitPath(value)
	if len(segments) < 4 || !strings.EqualFold(segments[2], "resourceGroups") {
		return "", false
	}

	length := 4
	if len(segments) >= 8 && strings.EqualFold(segments[4], "providers") {
		length = 6 + (len(segments)-6)/2*2
	}
	return "/" + strings.Join(segments[:length], "/"), true
}

func splitPath(path string) []string {
	segments := make([]string, 0, 8)
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func isPositiveInteger(value string) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number > 0
}

func decode(segment string) string {
	if decoded, err := url.PathUnescape(segment); err == nil {
		return decoded
	}
	return segment
}

func typeList() string {
	names := make([]string, len(types))
	for index, candidate := range types {
		names[index] = string(candidate)
	}
	return strings.Join(names, ", ")
}
