// Command autofeat manages ephemeral Git worktrees for AI agent feature sessions.
package main

import (
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

const headlessPrompt = "Please execute the objectives defined in TASK.md"

var (
	statusCommand      = statusSessions
	openConfigCommand  = openConfig
	openFeatureCommand = openSession
	openCopilotCommand = openCopilotSession
	runFeatureCommand  = runFeature
	syncFeatureCommand = syncFeature
	teardownCommand    = teardownSession
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

type newArguments struct {
	featureName  string
	remoteURL    string
	templateName string
	baseBranch   string
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

func listSessionsTo(output io.Writer) error {
	currentState, err := state.Load()
	if err != nil {
		return err
	}
	sessions := currentState.Sessions

	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "FEATURE\tCREATED\tREPOSITORIES\tDRIFT")
	for _, name := range names {
		session := sessions[name]
		fmt.Fprintf(writer, "%s\t%s\t%d\t%s\n", name, session.CreatedAt.Format(time.RFC3339), len(session.Repos), sessionDrift(session, currentState.DefaultBaseBranch))
	}

	return writer.Flush()
}

func sessionDrift(session state.Session, defaultBaseBranch string) string {
	totalBehind := 0
	for _, repository := range session.Repos {
		baseBranch := repository.BaseBranch
		if baseBranch == "" {
			baseBranch = defaultBaseBranch
		}
		status, err := gitcmd.CachedBaseStatus(repository.WorktreePath, baseBranch)
		if err != nil {
			return "unknown"
		}
		totalBehind += status.Behind
	}
	if totalBehind == 0 {
		return "current"
	}

	return fmt.Sprintf("%d behind", totalBehind)
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
	return addRepositoryAtRef(featureName, repoRoot, requestedBaseBranch)
}

func addRepositoryAt(featureName, repoRoot string) error {
	return addRepositoryAtRef(featureName, repoRoot, "")
}

func addRepositoryAtRef(featureName, repoRoot, requestedBaseBranch string) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}

	currentState, err := state.Load()
	if err != nil {
		return err
	}
	repositoryKey := filepath.Clean(repoRoot)
	baseBranch, err := selectBaseBranch(repoRoot, repositoryKey, requestedBaseBranch, currentState)
	if err != nil {
		return err
	}
	baseRef, err := gitcmd.ResolveBaseRef(repoRoot, baseBranch)
	if err != nil {
		return err
	}
	sessionBaseRef := sessionBaseReference(baseBranch, baseRef)
	repoName := filepath.Base(filepath.Clean(repoRoot))
	featureDirName := featureDirectoryName(featureName)
	featureDir := filepath.Join(configuration.WorkspaceBaseDir, featureDirName)
	workspaceFile := filepath.Join(featureDir, featureDirName+".code-workspace")

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
		return fmt.Errorf("create feature directory: %w", err)
	}
	if err := gitcmd.AddWorktreeAt(repoRoot, featureBranchName(featureName), worktreePath, baseRef); err != nil {
		return err
	}
	if err := hooks.Run(configuration.Hooks, hooks.PostAdd, worktreePath); err != nil {
		removeErr := gitcmd.RemoveWorktree(worktreePath, true)
		if removeErr == nil {
			removeErr = gitcmd.DeleteBranch(repoRoot, featureBranchName(featureName))
		}
		return errors.Join(err, removeErr)
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
		return err
	}

	if err := workspace.Write(session.WorkspaceFile, repositoryDirectoryNames(session.Repos)); err != nil {
		return err
	}

	fmt.Printf("Added %s to feature %s\n", repoName, featureName)
	return nil
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

	featureDirName := featureDirectoryName(featureName)
	featureDir := filepath.Join(configuration.WorkspaceBaseDir, featureDirName)
	workspaceFile := filepath.Join(featureDir, featureDirName+".code-workspace")

	currentState, err := state.Load()
	if err != nil {
		return err
	}
	repositoryKey := strings.TrimSuffix(strings.TrimSpace(remoteURL), "/")
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

	if err := gitcmd.CheckoutNewBranch(worktreePath, featureBranchName(featureName), baseRef); err != nil {
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

func runFeature(featureName, task string) error {
	insideWorkTree, err := gitcmd.IsInsideWorkTree()
	if err != nil {
		return err
	}
	if insideWorkTree {
		if err := ensureRepositoryAdded(featureName); err != nil {
			return err
		}
	}

	return runHeadless(featureName, task)
}

func ensureRepositoryAdded(featureName string) error {
	repoRoot, err := gitcmd.GetRepoRoot()
	if err != nil {
		return err
	}

	session, err := state.GetSession(featureName)
	if errors.Is(err, state.ErrSessionNotFound) {
		return addRepository(featureName)
	}
	if err != nil {
		return err
	}

	for _, repository := range session.Repos {
		if !repository.IsRemoteClone && filepath.Clean(repository.OriginalPath) == filepath.Clean(repoRoot) {
			return nil
		}
	}

	return addRepository(featureName)
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

	for _, repository := range template.Repositories {
		if repository.Kind == templates.LocalRepository {
			err = addRepositoryAt(featureName, localRoots[repository.Source])
		} else {
			err = addRemoteRepository(featureName, repository.Source)
		}
		if err != nil {
			return errors.Join(err, rollbackTemplateSession(featureName))
		}
	}
	return nil
}

func rollbackTemplateSession(featureName string) error {
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
		if removeErr == nil {
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

func runHeadless(featureName, task string) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}
	if task != "" {
		if err := appendTask(session.FeatureDir, task); err != nil {
			return err
		}
	}

	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(configuration.HeadlessCmd); err != nil {
		return fmt.Errorf("find headless command %q: %w", configuration.HeadlessCmd, err)
	}

	command := exec.Command(configuration.HeadlessCmd, headlessArgs()...)
	command.Dir = session.FeatureDir
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run feature %q with %q: %w", featureName, configuration.HeadlessCmd, err)
	}

	return nil
}

type syncRepository struct {
	repository  state.Repository
	baseRef     string
	fetchBranch string
}

func syncFeature(featureName string) error {
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
		repository := syncRepo.repository
		if syncRepo.fetchBranch != "" {
			if err := gitcmd.FetchBase(repository.WorktreePath, syncRepo.fetchBranch); err != nil {
				return err
			}
		}
		baseRef := syncRepo.baseRef
		ahead, behind, err := gitcmd.AheadBehind(repository.WorktreePath, baseRef)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d ahead, %d behind %s\n", repository.Name, ahead, behind, baseRef)
		if behind == 0 {
			continue
		}
		if err := gitcmd.Rebase(repository.WorktreePath, baseRef); err != nil {
			fmt.Fprintf(os.Stderr, "Resolve conflicts in %s, then run `git -C %s rebase --continue`, or abort with `git -C %s rebase --abort`.\n", repository.Name, repository.WorktreePath, repository.WorktreePath)
			return err
		}
		ahead, behind, err = gitcmd.AheadBehind(repository.WorktreePath, baseRef)
		if err != nil {
			return err
		}
		fmt.Printf("%s: synchronized (%d ahead, %d behind)\n", repository.Name, ahead, behind)
	}

	return nil
}

func headlessArgs() []string {
	return []string{"-i", headlessPrompt}
}

func appendTask(featureDir, task string) error {
	taskFile, err := os.OpenFile(filepath.Join(featureDir, "TASK.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open task file: %w", err)
	}

	if _, err := fmt.Fprintln(taskFile, task); err != nil {
		_ = taskFile.Close()
		return fmt.Errorf("append task: %w", err)
	}
	if err := taskFile.Close(); err != nil {
		return fmt.Errorf("close task file: %w", err)
	}

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

func featureDirectoryName(featureName string) string {
	return strings.NewReplacer("%", "%25", "/", "%2F").Replace(featureName)
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
	trimmedURL := strings.TrimSuffix(strings.TrimSpace(remoteURL), "/")
	repoName := strings.TrimSuffix(filepath.Base(trimmedURL), ".git")
	if repoName == "" || repoName == "." || repoName == ".." || filepath.Base(repoName) != repoName {
		return "", fmt.Errorf("invalid remote repository URL %q", remoteURL)
	}

	return repoName, nil
}

func usageError() error {
	return errors.New("usage: autofeat <new|open|run|sync|status|teardown|list|template|config|version|completion> [feature-selector ...] [options]")
}
