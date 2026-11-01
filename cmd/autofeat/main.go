// Command autofeat manages ephemeral Git worktrees for AI agent feature sessions.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/painlesshippo/autofeat/internal/config"
	"github.com/painlesshippo/autofeat/internal/devcontainer"
	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/hooks"
	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/templates"
	"github.com/painlesshippo/autofeat/internal/workspace"
)

// Build metadata is set with Go linker flags during builds.
var (
	version       = "dev"
	commit        = "unknown"
	buildDatetime = "unknown"
	goVersion     = "unknown"
)

// fallbackFeatureDirectoryName is used when normalization leaves nothing usable.
const fallbackFeatureDirectoryName = "feature"

var (
	statusCommand                 = statusSessions
	openConfigCommand             = openConfig
	openFeatureCommand            = openSession
	openCopilotCommand            = openCopilotSession
	openDevcontainerCommand       = openDevcontainerSession
	syncFeatureCommand            = syncFeature
	cancelSyncCommand             = cancelSync
	teardownCommand               = teardownSession
	removeRepositoryCommand       = removeRepositoryFromSession
	removeRepositoryByNameCommand = removeRepositoryByName
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "autofeat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := newRootCommand()
	command.SetArgs(args)
	return command.Execute()
}

func shellExpandedWildcard(selectors []string) bool {
	if len(selectors) < 2 {
		return false
	}
	for _, selector := range selectors {
		if strings.Contains(selector, "*") {
			return false
		}
		if _, err := os.Lstat(selector); err != nil {
			return false
		}
	}
	return true
}

func runSelectedFeatures(selectors []string, command func(string) error) error {
	sessions, err := state.ListSessions()
	if err != nil {
		return err
	}
	featureNames, err := selectFeatureNames(sessions, selectors)
	if err != nil {
		return err
	}
	for _, featureName := range featureNames {
		if err := command(featureName); err != nil {
			return err
		}
	}
	return nil
}

// runAllSelectedFeatures runs command for every selected feature, continuing
// past failures so a recovery command reports each one instead of stopping at
// the first.
func runAllSelectedFeatures(selectors []string, command func(string) error) error {
	sessions, err := state.ListSessions()
	if err != nil {
		return err
	}
	featureNames, err := selectFeatureNames(sessions, selectors)
	if err != nil {
		return err
	}
	failures := make([]error, 0, len(featureNames))
	for _, featureName := range featureNames {
		if err := command(featureName); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func selectFeatureNames(sessions map[string]state.Session, selectors []string) ([]string, error) {
	if len(selectors) == 0 {
		return nil, errors.New("feature selector is required")
	}
	selected := make(map[string]struct{})
	for _, selector := range selectors {
		if selector == "" {
			return nil, errors.New("feature selector is required")
		}
		matched := false
		for featureName := range sessions {
			if matchFeatureSelector(selector, featureName) {
				selected[featureName] = struct{}{}
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("no features match %q", selector)
		}
	}
	featureNames := make([]string, 0, len(selected))
	for featureName := range selected {
		featureNames = append(featureNames, featureName)
	}
	sort.Strings(featureNames)
	return featureNames, nil
}

func matchFeatureSelector(selector, featureName string) bool {
	parts := strings.Split(selector, "*")
	if len(parts) == 1 {
		return selector == featureName
	}

	position := 0
	if parts[0] != "" {
		if !strings.HasPrefix(featureName, parts[0]) {
			return false
		}
		position = len(parts[0])
	}
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		index := strings.Index(featureName[position:], part)
		if index < 0 {
			return false
		}
		position += index + len(part)
	}

	suffix := parts[len(parts)-1]
	return suffix == "" || strings.HasSuffix(featureName[position:], suffix)
}

func isRemoteURL(value string) bool {
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "git@")
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

type sessionListing struct {
	name         string
	session      state.Session
	lastActivity time.Time
	drift        string
}

func listSessionsTo(output io.Writer) error {
	currentState, err := state.Load()
	if err != nil {
		return err
	}

	listings := make([]sessionListing, 0, len(currentState.Sessions))
	for name, session := range currentState.Sessions {
		drift, lastActivity := inspectSessionListing(session, currentState.DefaultBaseBranch)
		listings = append(listings, sessionListing{name: name, session: session, lastActivity: lastActivity, drift: drift})
	}
	sort.Slice(listings, func(i, j int) bool {
		if !listings[i].lastActivity.Equal(listings[j].lastActivity) {
			return listings[i].lastActivity.After(listings[j].lastActivity)
		}
		return listings[i].name < listings[j].name
	})

	var table bytes.Buffer
	writer := tabwriter.NewWriter(&table, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "FEATURE\tCREATED\tLAST ACTIVITY\tREPOSITORIES\tDRIFT")
	for _, listing := range listings {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\n",
			listing.name,
			listing.session.CreatedAt.Format(time.RFC3339),
			listing.lastActivity.UTC().Format(time.RFC3339),
			len(listing.session.Repos),
			listing.drift,
		)
	}
	if err := writer.Flush(); err != nil {
		return err
	}

	// Colors are applied after alignment because tabwriter would count escape
	// sequences as cell width.
	lines := strings.SplitAfter(table.String(), "\n")
	color := colorEnabled(output)
	for index, line := range lines {
		if line == "" {
			continue
		}
		if color {
			text := strings.TrimSuffix(line, "\n")
			if index == 0 {
				text = colorize(true, ansiBold, text)
			} else {
				drift := listings[index-1].drift
				text = strings.TrimSuffix(text, drift) + colorize(true, driftColor(drift), drift)
			}
			line = text + "\n"
		}
		if _, err := io.WriteString(output, line); err != nil {
			return err
		}
	}

	return nil
}

func sessionDrift(session state.Session, defaultBaseBranch string) string {
	drift, _ := inspectSessionListing(session, defaultBaseBranch)
	return drift
}

// inspectSessionListing reports the cached base drift of session and its most
// recent local activity, which is never earlier than the session creation.
func inspectSessionListing(session state.Session, defaultBaseBranch string) (string, time.Time) {
	totalBehind := 0
	driftKnown := true
	lastActivity := session.CreatedAt
	for _, repository := range session.Repos {
		baseBranch := repository.BaseBranch
		if baseBranch == "" {
			baseBranch = defaultBaseBranch
		}
		baseRef, err := gitcmd.ResolveBaseRef(repository.WorktreePath, baseBranch)
		if err == nil {
			_, behind, aheadBehindErr := gitcmd.AheadBehind(repository.WorktreePath, baseRef)
			if aheadBehindErr != nil {
				driftKnown = false
			}
			totalBehind += behind
		} else {
			driftKnown = false
			baseRef = ""
		}
		if activity, err := gitcmd.LastActivity(repository.WorktreePath, baseRef); err == nil && activity.After(lastActivity) {
			lastActivity = activity
		}
	}

	switch {
	case !driftKnown:
		return "unknown", lastActivity
	case totalBehind == 0:
		return "current", lastActivity
	default:
		return fmt.Sprintf("%d behind", totalBehind), lastActivity
	}
}

func driftColor(drift string) string {
	switch {
	case drift == "current":
		return ansiGreen
	case drift == "unknown":
		return ansiRed
	default:
		return ansiYellow
	}
}

func colorize(enabled bool, code, text string) string {
	if !enabled {
		return text
	}
	return code + text + ansiReset
}

// colorEnabled reports whether ANSI colors should be written to output: only
// for terminals, and never when NO_COLOR is set (https://no-color.org).
func colorEnabled(output io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type repositoryStatus struct {
	featureName     string
	repositoryName  string
	branch          string
	worktree        string
	baseBranch      string
	baseAhead       int
	baseBehind      int
	baseKnown       bool
	pushAhead       int
	pushBehind      int
	pushHasOrigin   bool
	pushPublished   bool
	pushKnown       bool
	missing         bool
	detached        bool
	wrongBranch     bool
	rebasing        bool
	dirty           bool
	inspectionError bool
	detail          string
}

func statusSessions(selectors []string) error {
	return statusSessionsTo(os.Stdout, selectors)
}

func statusSessionsTo(output io.Writer, selectors []string) error {
	currentState, err := state.Load()
	if err != nil {
		return err
	}

	featureNames := []string(nil)
	if len(currentState.Sessions) != 0 || len(selectors) != 1 || selectors[0] != "*" {
		featureNames, err = selectFeatureNames(currentState.Sessions, selectors)
		if err != nil {
			return err
		}
	}

	statuses := make([]repositoryStatus, 0)
	for _, featureName := range featureNames {
		session := currentState.Sessions[featureName]
		repositories := append([]state.Repository(nil), session.Repos...)
		sort.Slice(repositories, func(i, j int) bool {
			if repositories[i].Name == repositories[j].Name {
				return repositories[i].WorktreePath < repositories[j].WorktreePath
			}
			return repositories[i].Name < repositories[j].Name
		})
		for _, repository := range repositories {
			statuses = append(statuses, inspectRepositoryStatus(featureName, repository, currentState.DefaultBaseBranch))
		}
	}

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "FEATURE\tREPOSITORY\tBRANCH\tWORKTREE\tBASE\tDRIFT\tPUSH\tSTATE\tDETAIL")
	for _, status := range statuses {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			status.featureName,
			status.repositoryName,
			status.branch,
			status.worktree,
			status.baseBranch,
			formatStatusRelation(status.baseKnown, status.baseAhead, status.baseBehind, "unknown"),
			formatPushStatus(status),
			statusState(status),
			statusDetail(status),
		)
	}

	return writer.Flush()
}

func inspectRepositoryStatus(featureName string, repository state.Repository, defaultBaseBranch string) repositoryStatus {
	status := repositoryStatus{
		featureName:    featureName,
		repositoryName: repository.Name,
		branch:         "unknown",
		worktree:       "unknown",
		baseBranch:     repository.BaseBranch,
	}
	if status.baseBranch == "" {
		status.baseBranch = defaultBaseBranch
	}

	info, err := os.Stat(repository.WorktreePath)
	if errors.Is(err, os.ErrNotExist) {
		status.missing = true
		status.worktree = "missing"
		status.detail = repository.WorktreePath
		return status
	}
	if err != nil {
		status.inspectionError = true
		status.detail = fmt.Sprintf("inspect worktree: %v", err)
		return status
	}
	if !info.IsDir() {
		status.inspectionError = true
		status.detail = fmt.Sprintf("worktree is not a directory: %s", repository.WorktreePath)
		return status
	}

	status.branch, err = gitcmd.CurrentBranch(repository.WorktreePath)
	if errors.Is(err, gitcmd.ErrDetachedHead) {
		status.detached = true
		status.branch = "detached"
	} else if err != nil {
		status.inspectionError = true
		status.detail = err.Error()
		return status
	} else {
		status.wrongBranch = status.branch != featureBranchName(featureName)
	}

	status.rebasing, err = gitcmd.IsRebaseInProgress(repository.WorktreePath)
	if err != nil {
		status.inspectionError = true
		status.detail = err.Error()
		return status
	}
	status.dirty, err = gitcmd.HasUncommittedChanges(repository.WorktreePath)
	if err != nil {
		status.inspectionError = true
		status.detail = err.Error()
		return status
	}
	if status.dirty {
		status.worktree = "dirty"
	} else {
		status.worktree = "clean"
	}

	baseStatus, err := gitcmd.CachedBaseStatus(repository.WorktreePath, status.baseBranch)
	if err != nil {
		status.inspectionError = true
		status.detail = err.Error()
		return status
	}
	status.baseKnown = true
	status.baseAhead = baseStatus.Ahead
	status.baseBehind = baseStatus.Behind

	if status.detached || status.wrongBranch {
		return status
	}
	pushStatus, err := gitcmd.CachedRemoteBranchStatus(repository.WorktreePath, status.branch)
	if err != nil {
		status.inspectionError = true
		status.detail = err.Error()
		return status
	}
	status.pushHasOrigin = pushStatus.HasOrigin
	status.pushPublished = pushStatus.Published
	status.pushKnown = true
	status.pushAhead = pushStatus.Ahead
	status.pushBehind = pushStatus.Behind

	return status
}

func statusState(status repositoryStatus) string {
	if status.inspectionError {
		return "error"
	}
	if status.missing {
		return "missing"
	}
	if status.detached {
		return "detached"
	}
	if status.wrongBranch {
		return "wrong-branch"
	}
	if status.rebasing {
		return "rebasing"
	}
	if status.baseKnown && status.baseBehind > 0 {
		return "behind-base"
	}
	if status.dirty {
		return "dirty"
	}
	if status.pushKnown && status.pushAhead > 0 && status.pushBehind > 0 {
		return "remote-diverged"
	}
	if status.pushKnown && status.pushBehind > 0 {
		return "remote-ahead"
	}
	if status.pushKnown && status.pushHasOrigin && (!status.pushPublished || status.pushAhead > 0) {
		return "unpushed"
	}
	return "ready"
}

func formatStatusRelation(known bool, ahead, behind int, unknown string) string {
	if !known {
		return unknown
	}
	return fmt.Sprintf("+%d/-%d", ahead, behind)
}

func formatPushStatus(status repositoryStatus) string {
	if !status.pushKnown {
		return "unknown"
	}
	if !status.pushHasOrigin {
		return "n/a"
	}
	if !status.pushPublished {
		return "unpublished"
	}
	return formatStatusRelation(true, status.pushAhead, status.pushBehind, "unknown")
}

func statusDetail(status repositoryStatus) string {
	if status.detail != "" {
		return status.detail
	}
	if status.wrongBranch {
		return "expected " + status.featureName
	}
	return "-"
}

func addRepository(featureName string) error {
	return addRepositoryWithRef(featureName, "")
}

func addRepositoryWithRef(featureName, requestedBaseBranch string) error {
	repoRoot, err := gitcmd.GetRepoRoot()
	if err != nil {
		return err
	}
	_, err = addRepositoryAtRef(featureName, repoRoot, requestedBaseBranch)
	return err
}

func addLocalRepositoryWithRef(featureName, localPath, requestedBaseBranch string) error {
	repoRoot, err := gitcmd.GetRepoRootAt(localPath)
	if err != nil {
		return err
	}
	_, err = addRepositoryAtRef(featureName, repoRoot, requestedBaseBranch)
	return err
}

func addRepositoryAt(featureName, repoRoot string) (bool, error) {
	return addRepositoryAtRef(featureName, repoRoot, "")
}

func addRepositoryAtRef(featureName, repoRoot, requestedBaseBranch string) (bool, error) {
	configuration, err := config.Load()
	if err != nil {
		return false, err
	}

	currentState, err := state.Load()
	if err != nil {
		return false, err
	}
	repositoryKey := filepath.Clean(repoRoot)
	baseBranch, err := selectBaseBranch(repoRoot, repositoryKey, requestedBaseBranch, currentState)
	if err != nil {
		return false, err
	}
	baseRef, err := gitcmd.ResolveBaseRef(repoRoot, baseBranch)
	if err != nil {
		return false, err
	}
	sessionBaseRef := sessionBaseReference(baseBranch, baseRef)
	repoName := filepath.Base(filepath.Clean(repoRoot))
	featureDir, workspaceFile, err := featureDirectoryPaths(featureName, currentState, configuration.WorkspaceBaseDir)
	if err != nil {
		return false, err
	}

	session, sessionExists := currentState.Sessions[featureName]
	newSession := !sessionExists
	if newSession {
		session = state.Session{
			CreatedAt:     time.Now().UTC(),
			FeatureDir:    featureDir,
			WorkspaceFile: workspaceFile,
			Repos:         make([]state.Repository, 0, 1),
		}
	}
	parentName := filepath.Base(filepath.Dir(repoRoot))
	worktreePath := filepath.Join(featureDir, repositoryDirectoryName(repoName, parentName, session.Repos))

	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return false, fmt.Errorf("create feature directory: %w", err)
	}
	branchCreated, err := gitcmd.AddWorktreeAt(repoRoot, featureBranchName(featureName), worktreePath, baseRef)
	if err != nil {
		return false, err
	}
	if err := hooks.Run(configuration.Hooks, hooks.PostAdd, worktreePath); err != nil {
		removeErr := gitcmd.RemoveWorktree(worktreePath, true)
		if removeErr == nil && branchCreated {
			removeErr = gitcmd.DeleteBranch(repoRoot, featureBranchName(featureName))
		}
		return false, errors.Join(err, removeErr)
	}

	session.Repos = append(session.Repos, state.Repository{
		Name:         repoName,
		OriginalPath: repoRoot,
		WorktreePath: worktreePath,
		BaseBranch:   sessionBaseRef,
	})
	currentState.Sessions[featureName] = session
	if requestedBaseBranch != "" {
		currentState.RepositoryBaseBranches[repositoryKey] = requestedBaseBranch
	}
	if err := state.Save(currentState); err != nil {
		return branchCreated, err
	}

	if err := workspace.Write(session.WorkspaceFile, repositoryDirectoryNames(session.Repos)); err != nil {
		return branchCreated, err
	}

	fmt.Printf("Added %s to feature %s\n", repoName, featureName)
	return branchCreated, nil
}

func addRemoteRepository(featureName, remoteURL string) error {
	return addRemoteRepositoryWithRef(featureName, remoteURL, "")
}

func addRemoteRepositoryWithRef(featureName, remoteURL, requestedBaseBranch string) error {
	repoName, err := remoteRepositoryName(remoteURL)
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
	repositoryKey := strings.TrimSuffix(strings.TrimSpace(remoteURL), "/")
	featureDir, workspaceFile, err := featureDirectoryPaths(featureName, currentState, configuration.WorkspaceBaseDir)
	if err != nil {
		return err
	}
	session, sessionExists := currentState.Sessions[featureName]
	newSession := !sessionExists
	if newSession {
		session = state.Session{
			CreatedAt:     time.Now().UTC(),
			FeatureDir:    featureDir,
			WorkspaceFile: workspaceFile,
			Repos:         make([]state.Repository, 0, 1),
		}
	}
	worktreePath := filepath.Join(featureDir, repositoryDirectoryName(repoName, repositoryParentName(remoteURL), session.Repos))

	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return fmt.Errorf("create feature directory: %w", err)
	}
	if err := gitcmd.Clone(remoteURL, worktreePath); err != nil {
		return err
	}
	cloneSucceeded := false
	defer func() {
		if cloneSucceeded {
			return
		}
		_ = os.RemoveAll(worktreePath)
	}()
	baseBranch, err := selectBaseBranch(worktreePath, repositoryKey, requestedBaseBranch, currentState)
	if err != nil {
		return err
	}
	baseRef, err := gitcmd.ResolveBaseRef(worktreePath, baseBranch)
	if err != nil {
		return err
	}
	sessionBaseRef := sessionBaseReference(baseBranch, baseRef)

	if _, err := gitcmd.CheckoutBranch(worktreePath, featureBranchName(featureName), baseRef); err != nil {
		return err
	}
	if err := hooks.Run(configuration.Hooks, hooks.PostAdd, worktreePath); err != nil {
		return err
	}

	session.Repos = append(session.Repos, state.Repository{
		Name:          repoName,
		OriginalPath:  remoteURL,
		WorktreePath:  worktreePath,
		IsRemoteClone: true,
		BaseBranch:    sessionBaseRef,
	})

	if err := workspace.Write(session.WorkspaceFile, repositoryDirectoryNames(session.Repos)); err != nil {
		return err
	}

	currentState.Sessions[featureName] = session
	if requestedBaseBranch != "" {
		currentState.RepositoryBaseBranches[repositoryKey] = requestedBaseBranch
	}
	if err := state.Save(currentState); err != nil {
		return err
	}
	cloneSucceeded = true

	fmt.Printf("Cloned remote repo '%s' to feature '%s'\n", repoName, featureName)
	return nil
}

func openSession(featureName string) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}

	command := exec.Command(configuration.EditorCmd, session.WorkspaceFile)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("open feature %q with %q: %w", featureName, configuration.EditorCmd, err)
	}

	return nil
}

// openDevcontainerSession opens the session's generated multi-root workspace
// inside the Dev Container defined by the developer-provided configuration at
// configPath. autofeat passes that configuration through untouched — it neither
// parses nor modifies it, and the configuration owns the container's mounts,
// Git behavior, and workspace semantics. A relative configPath is resolved
// against the current working directory. A missing, non-file, or unreadable
// path fails early without falling back to a host window.
func openDevcontainerSession(featureName, configPath string) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.WorkspaceFile) == "" {
		return fmt.Errorf("feature %q has no workspace file to open in a dev container", featureName)
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}

	resolvedConfigPath, err := validateDevcontainerConfig(configPath)
	if err != nil {
		return err
	}
	uri, err := devcontainer.WorkspaceURI(filepath.Dir(session.WorkspaceFile), resolvedConfigPath, session.WorkspaceFile)
	if err != nil {
		return err
	}

	command := exec.Command(configuration.EditorCmd, "--file-uri", uri)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("open feature %q in dev container with %q: %w", featureName, configuration.EditorCmd, err)
	}

	return nil
}

// validateDevcontainerConfig resolves configPath against the current working
// directory and confirms it is a readable regular file, without reading or
// interpreting its contents.
func validateDevcontainerConfig(configPath string) (string, error) {
	resolved, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve dev container configuration path %q: %w", configPath, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("dev container configuration %q: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("dev container configuration %q is not a file", resolved)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("dev container configuration %q: %w", resolved, err)
	}
	_ = file.Close()
	return resolved, nil
}

func openConfig() error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if err := config.Save(configuration); err != nil {
		return err
	}
	configPath, err := config.Path()
	if err != nil {
		return err
	}

	command := exec.Command(configuration.EditorCmd, configPath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("open config with %q: %w", configuration.EditorCmd, err)
	}

	return nil
}

func openCopilotSession(featureName string) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(configuration.HeadlessCmd); err != nil {
		return fmt.Errorf("find Copilot command %q: %w", configuration.HeadlessCmd, err)
	}

	command := exec.Command(configuration.HeadlessCmd)
	command.Dir = session.FeatureDir
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("open feature %q with Copilot command %q: %w", featureName, configuration.HeadlessCmd, err)
	}

	return nil
}

func saveTemplateFromSession(templateName, featureName string) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}

	repositories := make([]templates.Repository, 0, len(session.Repos))
	for _, repository := range session.Repos {
		kind := templates.LocalRepository
		if repository.IsRemoteClone {
			kind = templates.RemoteRepository
		}
		repositories = append(repositories, templates.Repository{Kind: kind, Source: repository.OriginalPath})
	}
	if err := templates.Put(templateName, templates.Template{Repositories: repositories}); err != nil {
		return err
	}
	fmt.Printf("Saved template %s from feature %s\n", templateName, featureName)
	return nil
}

func listTemplatesTo(output io.Writer) error {
	store, err := templates.Load()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(store.Templates))
	for name := range store.Templates {
		names = append(names, name)
	}
	sort.Strings(names)

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tREPOSITORIES")
	for _, name := range names {
		fmt.Fprintf(writer, "%s\t%d\n", name, len(store.Templates[name].Repositories))
	}
	return writer.Flush()
}

func showTemplateTo(output io.Writer, templateName string) error {
	template, err := templates.Get(templateName)
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "KIND\tSOURCE")
	for _, repository := range template.Repositories {
		fmt.Fprintf(writer, "%s\t%s\n", repository.Kind, repository.Source)
	}
	return writer.Flush()
}

func instantiateTemplate(featureName, templateName string) error {
	if _, err := state.GetSession(featureName); err == nil {
		return fmt.Errorf("feature session already exists: %s", featureName)
	} else if !errors.Is(err, state.ErrSessionNotFound) {
		return err
	}
	template, err := templates.Get(templateName)
	if err != nil {
		return err
	}

	localRoots := make(map[string]string)
	for _, repository := range template.Repositories {
		switch repository.Kind {
		case templates.LocalRepository:
			root, err := gitcmd.GetRepoRootAt(repository.Source)
			if err != nil {
				return err
			}
			if filepath.Clean(root) != filepath.Clean(repository.Source) {
				return fmt.Errorf("template %q local source is not a repository root: %s", templateName, repository.Source)
			}
			localRoots[repository.Source] = root
		case templates.RemoteRepository:
			if !isRemoteURL(repository.Source) {
				return fmt.Errorf("template %q has invalid remote URL %q", templateName, repository.Source)
			}
		}
	}

	createdBranches := make(map[string]bool)
	for _, repository := range template.Repositories {
		if repository.Kind == templates.LocalRepository {
			var branchCreated bool
			branchCreated, err = addRepositoryAt(featureName, localRoots[repository.Source])
			createdBranches[filepath.Clean(localRoots[repository.Source])] = branchCreated
		} else {
			err = addRemoteRepository(featureName, repository.Source)
		}
		if err != nil {
			return errors.Join(err, rollbackTemplateSession(featureName, createdBranches))
		}
	}
	return nil
}

func rollbackTemplateSession(featureName string, createdBranches map[string]bool) error {
	session, err := state.GetSession(featureName)
	if errors.Is(err, state.ErrSessionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	var cleanupErr error
	for index := len(session.Repos) - 1; index >= 0; index-- {
		repository := session.Repos[index]
		if repository.IsRemoteClone {
			cleanupErr = errors.Join(cleanupErr, os.RemoveAll(repository.WorktreePath))
			continue
		}
		removeErr := gitcmd.RemoveWorktree(repository.WorktreePath, true)
		cleanupErr = errors.Join(cleanupErr, removeErr)
		if removeErr == nil && createdBranches[filepath.Clean(repository.OriginalPath)] {
			cleanupErr = errors.Join(cleanupErr, gitcmd.DeleteBranch(repository.OriginalPath, featureBranchName(featureName)))
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("roll back feature %q: %w", featureName, cleanupErr)
	}
	if err := os.RemoveAll(session.FeatureDir); err != nil {
		return fmt.Errorf("roll back feature directory: %w", err)
	}
	return state.DeleteSession(featureName)
}

type syncRepository struct {
	repository  state.Repository
	baseRef     string
	fetchBranch string
}

// syncFeature fetches every repository's base and rebases onto it. Unless
// keepConflicts is set, a failure rolls the whole session back to its
// pre-synchronization commits instead of leaving it partially rebased.
func syncFeature(featureName string, keepConflicts bool) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}

	repositories := make([]syncRepository, 0, len(session.Repos))
	for _, repository := range session.Repos {
		info, err := os.Stat(repository.WorktreePath)
		if err != nil {
			return fmt.Errorf("inspect worktree for repository %q: %w", repository.Name, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("worktree for repository %q is not a directory: %s", repository.Name, repository.WorktreePath)
		}

		branchName, err := gitcmd.CurrentBranch(repository.WorktreePath)
		if err != nil {
			return err
		}
		if branchName != featureBranchName(featureName) {
			return fmt.Errorf("repository %q is on branch %q, expected %q", repository.Name, branchName, featureBranchName(featureName))
		}
		dirty, err := gitcmd.HasUncommittedChanges(repository.WorktreePath)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("repository %q has uncommitted changes; commit or discard them before syncing", repository.Name)
		}
		rebasing, err := gitcmd.IsRebaseInProgress(repository.WorktreePath)
		if err != nil {
			return err
		}
		if rebasing {
			return fmt.Errorf("repository %q already has a rebase in progress", repository.Name)
		}
		if repository.BaseBranch == "" {
			repository.BaseBranch, err = detectBaseBranch(repository.WorktreePath)
			if err != nil {
				return err
			}
		}
		baseRef, err := gitcmd.ResolveBaseRef(repository.WorktreePath, repository.BaseBranch)
		if err != nil {
			return err
		}
		fetchBranch := ""
		const originPrefix = "refs/remotes/origin/"
		if strings.HasPrefix(baseRef, originPrefix) {
			fetchBranch = strings.TrimPrefix(baseRef, originPrefix)
		}
		repositories = append(repositories, syncRepository{
			repository:  repository,
			baseRef:     baseRef,
			fetchBranch: fetchBranch,
		})
	}

	for _, syncRepo := range repositories {
		if syncRepo.fetchBranch != "" {
			if err := fetchBaseWithRetry(syncRepo.repository.WorktreePath, syncRepo.fetchBranch); err != nil {
				return err
			}
		}
	}

	var rebased []rebasedRepository
	for _, syncRepo := range repositories {
		repository := syncRepo.repository
		baseRef := syncRepo.baseRef
		ahead, behind, err := gitcmd.AheadBehind(repository.WorktreePath, baseRef)
		if err != nil {
			return rollBackSync(repository, rebased, keepConflicts, err)
		}
		fmt.Printf("%s: %d ahead, %d behind %s\n", repository.Name, ahead, behind, baseRef)
		if behind == 0 {
			continue
		}
		originalHead, err := gitcmd.HeadCommit(repository.WorktreePath)
		if err != nil {
			return rollBackSync(repository, rebased, keepConflicts, err)
		}
		if err := gitcmd.EnableRerere(repository.WorktreePath); err != nil {
			return rollBackSync(repository, rebased, keepConflicts, err)
		}
		if err := gitcmd.Rebase(repository.WorktreePath, baseRef); err != nil {
			conflicts, _ := gitcmd.UnmergedPaths(repository.WorktreePath)
			if len(conflicts) != 0 {
				err = fmt.Errorf("repository %q conflicts with %s in %s: %w", repository.Name, baseRef, strings.Join(conflicts, ", "), err)
			}
			if keepConflicts {
				fmt.Fprintf(os.Stderr, "Resolve conflicts in %s, then run `git -C %s rebase --continue`, or cancel the synchronization with `autofeat sync %s --cancel`.\n", repository.Name, repository.WorktreePath, featureName)
				return err
			}
			if len(conflicts) != 0 {
				fmt.Fprintf(os.Stderr, "Rerun `autofeat sync %s --keep-conflicts` to resolve the conflicts in %s manually.\n", featureName, repository.Name)
			}
			return rollBackSync(repository, rebased, keepConflicts, err)
		}
		rebasedHead, err := gitcmd.HeadCommit(repository.WorktreePath)
		if err != nil {
			return rollBackSync(repository, rebased, keepConflicts, err)
		}
		rebased = append(rebased, rebasedRepository{
			repository:   repository,
			branch:       featureBranchName(featureName),
			originalHead: originalHead,
			rebasedHead:  rebasedHead,
		})
		ahead, behind, err = gitcmd.AheadBehind(repository.WorktreePath, baseRef)
		if err != nil {
			return rollBackSync(repository, rebased, keepConflicts, err)
		}
		fmt.Printf("%s: synchronized (%d ahead, %d behind)\n", repository.Name, ahead, behind)
	}

	return nil
}

type rebasedRepository struct {
	repository   state.Repository
	branch       string
	originalHead string
	rebasedHead  string
}

// rollBackSync keeps a failed synchronization all-or-nothing: it aborts the
// rebase left in failing and resets every repository already rebased to its
// pre-synchronization commit. A repository that moved since it was rebased,
// for example because an agent committed to it, is left untouched and reported
// so that work is never discarded. With keepConflicts, cause is returned
// unchanged.
func rollBackSync(failing state.Repository, rebased []rebasedRepository, keepConflicts bool, cause error) error {
	if keepConflicts {
		return cause
	}

	failures := []error{cause}
	rebasing, err := gitcmd.IsRebaseInProgress(failing.WorktreePath)
	if err != nil {
		failures = append(failures, err)
	} else if rebasing {
		if err := gitcmd.AbortRebase(failing.WorktreePath); err != nil {
			failures = append(failures, err)
		} else {
			fmt.Printf("%s: rebase aborted\n", failing.Name)
		}
	}
	for index := len(rebased) - 1; index >= 0; index-- {
		if err := restoreRebasedRepository(rebased[index]); err != nil {
			failures = append(failures, fmt.Errorf("restore repository %q to %s: %w", rebased[index].repository.Name, rebased[index].originalHead, err))
			continue
		}
		fmt.Printf("%s: restored to %s\n", rebased[index].repository.Name, rebased[index].originalHead)
	}
	if len(failures) != 1 {
		return fmt.Errorf("synchronization failed and could not be fully rolled back: %w", errors.Join(failures...))
	}

	return fmt.Errorf("%w; synchronization rolled back", cause)
}

func restoreRebasedRepository(rebased rebasedRepository) error {
	worktreePath := rebased.repository.WorktreePath
	branchName, err := gitcmd.CurrentBranch(worktreePath)
	if err != nil {
		return err
	}
	if branchName != rebased.branch {
		return fmt.Errorf("worktree switched to branch %q since it was synchronized; left unchanged", branchName)
	}
	head, err := gitcmd.HeadCommit(worktreePath)
	if err != nil {
		return err
	}
	if head != rebased.rebasedHead {
		return fmt.Errorf("branch moved from synchronized commit %s to %s; left unchanged to preserve new work", rebased.rebasedHead, head)
	}

	return gitcmd.ResetKeep(worktreePath, rebased.originalHead)
}

// fetchBaseRetryDelay is the wait before the first fetch retry; later retries
// wait proportionally longer.
var fetchBaseRetryDelay = 2 * time.Second

const fetchBaseAttempts = 3

func fetchBaseWithRetry(worktreePath, baseBranch string) error {
	var err error
	for attempt := 1; attempt <= fetchBaseAttempts; attempt++ {
		if err = gitcmd.FetchBase(worktreePath, baseBranch); err == nil {
			return nil
		}
		if attempt < fetchBaseAttempts {
			fmt.Fprintf(os.Stderr, "Fetch attempt %d of %d failed, retrying: %v\n", attempt, fetchBaseAttempts, err)
			time.Sleep(time.Duration(attempt) * fetchBaseRetryDelay)
		}
	}

	return err
}

// syncSelectedFeatures synchronizes every selected feature, continuing past
// failures, and summarizes the outcome when more than one feature was selected.
func syncSelectedFeatures(selectors []string, keepConflicts bool) error {
	var synchronized, failed []string
	err := runAllSelectedFeatures(selectors, func(featureName string) error {
		if err := syncFeatureCommand(featureName, keepConflicts); err != nil {
			failed = append(failed, featureName)
			return fmt.Errorf("sync feature %q: %w", featureName, err)
		}
		synchronized = append(synchronized, featureName)
		return nil
	})
	if total := len(synchronized) + len(failed); total > 1 {
		fmt.Printf("Synchronized %d of %d sessions", len(synchronized), total)
		if len(failed) != 0 {
			fmt.Printf("; failed: %s", strings.Join(failed, ", "))
		}
		fmt.Println()
	}

	return err
}

// cancelSync abandons an interrupted synchronization by aborting the rebase
// left in progress in each of the session's repositories. Repositories without
// a rebase in progress are reported and left untouched.
func cancelSync(featureName string) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}

	failures := make([]error, 0, len(session.Repos))
	for _, repository := range session.Repos {
		if err := cancelRepositorySync(repository); err != nil {
			failures = append(failures, fmt.Errorf("repository %q at %s: %w", repository.Name, repository.WorktreePath, err))
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("cancel synchronization of feature %q: %w", featureName, errors.Join(failures...))
	}

	return nil
}

func cancelRepositorySync(repository state.Repository) error {
	info, err := os.Stat(repository.WorktreePath)
	if err != nil {
		return fmt.Errorf("inspect worktree: %w", err)
	}
	if !info.IsDir() {
		return errors.New("worktree is not a directory")
	}

	rebasing, err := gitcmd.IsRebaseInProgress(repository.WorktreePath)
	if err != nil {
		return err
	}
	if !rebasing {
		fmt.Printf("%s: no rebase in progress\n", repository.Name)
		return nil
	}
	if err := gitcmd.AbortRebase(repository.WorktreePath); err != nil {
		return err
	}

	fmt.Printf("%s: rebase aborted\n", repository.Name)
	return nil
}

func detectBaseBranch(repositoryPath string) (string, error) {
	currentState, err := state.Load()
	if err != nil {
		return "", err
	}
	return selectBaseBranch(repositoryPath, filepath.Clean(repositoryPath), "", currentState)
}

func selectBaseBranch(repositoryPath, repositoryKey, requestedBaseBranch string, currentState state.State) (string, error) {
	if requestedBaseBranch != "" {
		return requestedBaseBranch, nil
	}
	if baseBranch := currentState.RepositoryBaseBranches[repositoryKey]; baseBranch != "" {
		return baseBranch, nil
	}

	baseBranch, err := gitcmd.DetectBaseBranch(repositoryPath)
	if err != nil {
		return "", err
	}
	if baseBranch != "" {
		return baseBranch, nil
	}
	return currentState.DefaultBaseBranch, nil
}

func sessionBaseReference(requestedRef, resolvedRef string) string {
	if strings.HasPrefix(resolvedRef, "refs/tags/") {
		return resolvedRef
	}
	if strings.HasPrefix(resolvedRef, "refs/") {
		return requestedRef
	}
	return resolvedRef
}

func teardownSession(featureName string, force bool) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}

	if !force {
		for _, repo := range session.Repos {
			missing, err := worktreeMissing(repo.WorktreePath)
			if err != nil {
				return err
			}
			if missing {
				if _, err := inspectMissingWorktree(repo, force); err != nil {
					return err
				}
				continue
			}
			dirty, err := gitcmd.HasUncommittedChanges(repo.WorktreePath)
			if err != nil {
				return err
			}
			if dirty {
				fmt.Fprintf(os.Stderr, "Warning: %s has uncommitted changes; teardown aborted. Use --force to discard them.\n", repo.Name)
				return errors.New("cannot teardown dirty worktree")
			}
		}
	}

	for _, repo := range session.Repos {
		missing, err := worktreeMissing(repo.WorktreePath)
		if err != nil {
			return err
		}
		if missing {
			if err := dropMissingWorktree(repo, force); err != nil {
				return err
			}
			continue
		}
		if repo.IsRemoteClone {
			branchName, err := gitcmd.CurrentBranch(repo.WorktreePath)
			if err != nil {
				return err
			}
			unpushed, err := gitcmd.HasUnpushedCommits(repo.WorktreePath, branchName)
			if err != nil {
				return err
			}
			if unpushed {
				fmt.Fprintf(os.Stderr, "Warning: %s has unpushed feature commits that will be deleted.\n", repo.Name)
			}
			if err := os.RemoveAll(repo.WorktreePath); err != nil {
				return fmt.Errorf("remove cloned repository %q: %w", repo.WorktreePath, err)
			}
			continue
		}

		if err := gitcmd.RemoveWorktree(repo.WorktreePath, force); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(session.FeatureDir); err != nil {
		return fmt.Errorf("remove feature directory: %w", err)
	}
	if err := state.DeleteSession(featureName); err != nil {
		return err
	}
	if err := hooks.Run(configuration.Hooks, hooks.PostTeardown, configuration.WorkspaceBaseDir); err != nil {
		return err
	}

	fmt.Printf("Tore down feature %s\n", featureName)
	return nil
}

func removeRepositoryFromSession(featureName, repositorySource string, remote, force bool) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}

	repositoryIndex := -1
	for index, repository := range session.Repos {
		if repository.IsRemoteClone != remote {
			continue
		}
		candidateSource := filepath.Clean(repository.OriginalPath)
		if remote {
			candidateSource = normalizeRemoteRepositoryURL(repository.OriginalPath)
		}
		if candidateSource == repositorySource {
			repositoryIndex = index
			break
		}
	}
	if repositoryIndex < 0 {
		return fmt.Errorf("repository %q is not attached to feature %q", repositorySource, featureName)
	}
	return removeSessionRepository(featureName, session, repositoryIndex, force)
}

// attachedLocalRepositoryRoot resolves value as a path inside a local Git
// repository and reports its root only when that repository is attached to the
// feature, so plain names that happen to be directories fall back to name lookup.
func attachedLocalRepositoryRoot(featureName, value string) (string, bool) {
	if info, err := os.Stat(value); err != nil || !info.IsDir() {
		return "", false
	}
	repositoryRoot, err := gitcmd.GetRepoRootAt(value)
	if err != nil {
		return "", false
	}
	repositoryRoot = filepath.Clean(repositoryRoot)
	session, err := state.GetSession(featureName)
	if err != nil {
		return "", false
	}
	for _, repository := range session.Repos {
		if !repository.IsRemoteClone && filepath.Clean(repository.OriginalPath) == repositoryRoot {
			return repositoryRoot, true
		}
	}
	return "", false
}

// removeRepositoryByName removes the repository whose worktree directory name,
// or otherwise whose repository name, matches name.
func removeRepositoryByName(featureName, name string, force bool) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}
	repositoryIndex, err := findRepositoryByName(featureName, session, name)
	if err != nil {
		return err
	}
	return removeSessionRepository(featureName, session, repositoryIndex, force)
}

func findRepositoryByName(featureName string, session state.Session, name string) (int, error) {
	for index, repository := range session.Repos {
		if filepath.Base(repository.WorktreePath) == name {
			return index, nil
		}
	}
	matches := make([]int, 0, 1)
	for index, repository := range session.Repos {
		if repository.Name == name {
			matches = append(matches, index)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("repository %q is not attached to feature %q", name, featureName)
	case 1:
		return matches[0], nil
	}
	candidates := make([]string, 0, len(matches))
	for _, index := range matches {
		candidates = append(candidates, filepath.Base(session.Repos[index].WorktreePath))
	}
	return -1, fmt.Errorf("repository name %q is ambiguous in feature %q; use one of: %s", name, featureName, strings.Join(candidates, ", "))
}

func removeSessionRepository(featureName string, session state.Session, repositoryIndex int, force bool) error {
	if len(session.Repos) == 1 {
		return teardownSession(featureName, force)
	}

	repository := session.Repos[repositoryIndex]
	missing, err := worktreeMissing(repository.WorktreePath)
	if err != nil {
		return err
	}
	if missing {
		err = dropMissingWorktree(repository, force)
	} else {
		err = removeRepositoryWorktree(repository, force)
	}
	if err != nil {
		return err
	}

	session.Repos = append(session.Repos[:repositoryIndex], session.Repos[repositoryIndex+1:]...)
	if err := state.UpdateSession(featureName, session); err != nil {
		return err
	}
	if err := workspace.Write(session.WorkspaceFile, repositoryDirectoryNames(session.Repos)); err != nil {
		return err
	}

	fmt.Printf("Removed %s from feature %s\n", repository.Name, featureName)
	return nil
}

func worktreeMissing(worktreePath string) (bool, error) {
	_, err := os.Stat(worktreePath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect worktree %q: %w", worktreePath, err)
	}
	return false, nil
}

// inspectMissingWorktree returns the source repository's record of a local
// worktree deleted outside autofeat. Locked records are refused unless force is
// set. Remote clones and local repositories whose source is gone have no record.
func inspectMissingWorktree(repository state.Repository, force bool) (gitcmd.WorktreeRecord, error) {
	if repository.IsRemoteClone {
		return gitcmd.WorktreeRecord{}, nil
	}
	sourceMissing, err := worktreeMissing(repository.OriginalPath)
	if err != nil {
		return gitcmd.WorktreeRecord{}, err
	}
	if sourceMissing {
		return gitcmd.WorktreeRecord{}, nil
	}
	record, err := gitcmd.FindWorktreeRecord(repository.OriginalPath, repository.WorktreePath)
	if err != nil {
		return gitcmd.WorktreeRecord{}, err
	}
	if record.Locked && !force {
		return record, fmt.Errorf("%s worktree is missing at %s but locked in %s; unlock it or use --force", repository.Name, repository.WorktreePath, repository.OriginalPath)
	}
	return record, nil
}

// dropMissingWorktree removes the source repository's record of a worktree
// deleted outside autofeat, leaving other worktree records untouched.
func dropMissingWorktree(repository state.Repository, force bool) error {
	record, err := inspectMissingWorktree(repository, force)
	if err != nil {
		return err
	}
	if record.Registered {
		if err := gitcmd.RemoveMissingWorktree(repository.OriginalPath, repository.WorktreePath, record.Locked); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Warning: %s worktree is missing at %s; dropping it from the feature.\n", repository.Name, repository.WorktreePath)
	return nil
}

func removeRepositoryWorktree(repository state.Repository, force bool) error {
	if !force {
		dirty, err := gitcmd.HasUncommittedChanges(repository.WorktreePath)
		if err != nil {
			return err
		}
		if dirty {
			fmt.Fprintf(os.Stderr, "Warning: %s has uncommitted changes; removal aborted. Use --force to discard them.\n", repository.Name)
			return errors.New("cannot remove dirty worktree")
		}
	}

	if repository.IsRemoteClone {
		branchName, err := gitcmd.CurrentBranch(repository.WorktreePath)
		if err != nil {
			return err
		}
		unpushed, err := gitcmd.HasUnpushedCommits(repository.WorktreePath, branchName)
		if err != nil {
			return err
		}
		if unpushed {
			fmt.Fprintf(os.Stderr, "Warning: %s has unpushed feature commits that will be deleted.\n", repository.Name)
		}
		if err := os.RemoveAll(repository.WorktreePath); err != nil {
			return fmt.Errorf("remove cloned repository %q: %w", repository.WorktreePath, err)
		}
	} else if err := gitcmd.RemoveWorktree(repository.WorktreePath, force); err != nil {
		return err
	}
	return nil
}

func validateFeatureName(name string) error {
	if name == "" {
		return fmt.Errorf("invalid feature name %q", name)
	}
	if err := gitcmd.ValidateBranchName(name); err != nil {
		return fmt.Errorf("invalid feature name %q: %w", name, err)
	}

	return nil
}

func featureBranchName(featureName string) string {
	return featureName
}

// featureDirectoryName flattens a feature name into a single directory name that
// external tooling can handle: only ASCII letters, digits, '.', '_' and '-'
// survive. The mapping is lossy, so callers must reject collisions.
func featureDirectoryName(featureName string) string {
	var builder strings.Builder
	previousWasDash := false
	for _, character := range featureName {
		switch {
		case character >= 'A' && character <= 'Z',
			character >= 'a' && character <= 'z',
			character >= '0' && character <= '9',
			character == '.', character == '_':
			builder.WriteRune(character)
			previousWasDash = false
		default:
			if !previousWasDash {
				builder.WriteByte('-')
				previousWasDash = true
			}
		}
	}

	// Leading dots would hide the directory and trailing dots are rejected by
	// Windows, so neither may bound the name.
	directoryName := strings.Trim(builder.String(), "-.")
	if directoryName == "" {
		return fallbackFeatureDirectoryName
	}
	return directoryName
}

// featureDirectoryPaths resolves the feature directory and workspace file for a
// session. Sessions already recorded in state keep the paths they were created
// with, including names produced by earlier naming schemes.
func featureDirectoryPaths(featureName string, currentState state.State, workspaceBaseDir string) (string, string, error) {
	if session, exists := currentState.Sessions[featureName]; exists {
		return session.FeatureDir, session.WorkspaceFile, nil
	}

	directoryName := featureDirectoryName(featureName)
	featureDir := filepath.Join(workspaceBaseDir, directoryName)
	existingNames := make([]string, 0, len(currentState.Sessions))
	for existingName := range currentState.Sessions {
		existingNames = append(existingNames, existingName)
	}
	sort.Strings(existingNames)
	for _, existingName := range existingNames {
		if currentState.Sessions[existingName].FeatureDir == featureDir {
			return "", "", fmt.Errorf(
				"feature %q maps to directory %s, which is already used by feature %q; pick a different feature name",
				featureName, featureDir, existingName)
		}
	}

	return featureDir, filepath.Join(featureDir, directoryName+".code-workspace"), nil
}

func repositoryDirectoryName(repoName, parentName string, repositories []state.Repository) string {
	usedNames := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		usedNames[filepath.Base(repository.WorktreePath)] = struct{}{}
	}

	candidates := []string{repoName}
	if parentName != "" && parentName != "." && parentName != ".." && parentName != string(filepath.Separator) {
		candidates = append(candidates, repoName+"-"+parentName)
	}
	for _, candidate := range candidates {
		if _, exists := usedNames[candidate]; !exists {
			return candidate
		}
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", repoName, suffix)
		if _, exists := usedNames[candidate]; !exists {
			return candidate
		}
	}
}

func repositoryDirectoryNames(repositories []state.Repository) []string {
	directoryNames := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		directoryNames = append(directoryNames, filepath.Base(repository.WorktreePath))
	}
	return directoryNames
}

func repositoryParentName(repositoryPath string) string {
	trimmedPath := strings.TrimSuffix(strings.TrimSpace(repositoryPath), "/")
	separator := strings.LastIndex(trimmedPath, "/")
	if separator < 0 {
		return ""
	}
	parentPath := trimmedPath[:separator]
	separator = strings.LastIndexAny(parentPath, "/:")
	return parentPath[separator+1:]
}

func remoteRepositoryName(remoteURL string) (string, error) {
	trimmedURL := normalizeRemoteRepositoryURL(remoteURL)
	repoName := strings.TrimSuffix(filepath.Base(trimmedURL), ".git")
	if repoName == "" || repoName == "." || repoName == ".." || filepath.Base(repoName) != repoName {
		return "", fmt.Errorf("invalid remote repository URL %q", remoteURL)
	}

	return repoName, nil
}

func normalizeRemoteRepositoryURL(remoteURL string) string {
	return strings.TrimSuffix(strings.TrimSpace(remoteURL), "/")
}

func usageError() error {
	return errors.New("usage: autofeat <new|remove|open|sync|status|teardown|list|template|config|version|completion> [feature-selector ...] [options]")
}
