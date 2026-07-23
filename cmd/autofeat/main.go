// Command autofeat manages ephemeral Git worktrees for AI agent feature sessions.
package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/painlesshippo/autofeat/internal/config"
	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/preview"
	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/workspace"
)

// Build metadata is set with Go linker flags by scripts/build.sh.
var (
	version       = "dev"
	commit        = "unknown"
	buildDatetime = "unknown"
	goVersion     = "unknown"
)

const headlessPrompt = "Please execute the objectives defined in TASK.md"

var (
	reviewCommand        = reviewAllSessions
	runFeatureCommand    = runFeature
	reviewFeatureCommand = reviewFeatureSession
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "autofeat:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	if args[0] == "list" {
		if len(args) != 1 {
			return usageError()
		}
		return listSessions()
	}
	if args[0] == "version" {
		if len(args) != 1 {
			return usageError()
		}
		fmt.Printf("autofeat %s\ncommit: %s\nbuilt: %s\ngo: %s\n", version, commit, buildDatetime, goVersion)
		return nil
	}
	if args[0] == "review" || args[0] == "preview" {
		baseRef, err := reviewBase(args[1:])
		if err != nil {
			return usageError()
		}
		return reviewCommand(baseRef)
	}

	featureName := args[0]
	if err := validateFeatureName(featureName); err != nil {
		return err
	}

	switch len(args) {
	case 1:
		insideWorkTree, err := gitcmd.IsInsideWorkTree()
		if err != nil {
			return err
		}
		if insideWorkTree {
			return addRepository(featureName)
		}
		return openSession(featureName)
	case 2:
		if isRemoteURL(args[1]) {
			return addRemoteRepository(featureName, args[1])
		}
		switch args[1] {
		case "open":
			return openSession(featureName)
		case "run":
			return runFeatureCommand(featureName, "")
		case "review":
			return reviewFeatureCommand(featureName, "")
		case "teardown":
			return teardownSession(featureName, false)
		default:
			return usageError()
		}
	case 3:
		if args[1] == "teardown" && args[2] == "--force" {
			return teardownSession(featureName, true)
		}
		return usageError()
	case 4:
		if args[1] == "run" && args[2] == "-task" {
			return runFeatureCommand(featureName, args[3])
		}
		if args[1] == "review" {
			baseRef, err := reviewBase(args[2:])
			if err != nil {
				return usageError()
			}
			return reviewFeatureCommand(featureName, baseRef)
		}
		return usageError()
	default:
		return usageError()
	}
}

func isRemoteURL(value string) bool {
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "git@")
}

func listSessions() error {
	sessions, err := state.ListSessions()
	if err != nil {
		return err
	}

	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)

	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "FEATURE\tCREATED\tREPOSITORIES")
	for _, name := range names {
		session := sessions[name]
		fmt.Fprintf(writer, "%s\t%s\t%d\n", name, session.CreatedAt.Format(time.RFC3339), len(session.Repos))
	}

	return writer.Flush()
}

func addRepository(featureName string) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}

	repoRoot, err := gitcmd.GetRepoRoot()
	if err != nil {
		return err
	}
	baseBranch, err := detectBaseBranch(repoRoot)
	if err != nil {
		return err
	}
	repoName := filepath.Base(filepath.Clean(repoRoot))
	featureDirName := featureDirectoryName(featureName)
	featureDir := filepath.Join(configuration.WorkspaceBaseDir, featureDirName)
	worktreePath := filepath.Join(featureDir, repoName)
	workspaceFile := filepath.Join(featureDir, featureDirName+".code-workspace")

	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return fmt.Errorf("create feature directory: %w", err)
	}
	if err := gitcmd.AddWorktree(featureBranchName(featureName), worktreePath); err != nil {
		return err
	}

	session, err := state.GetSession(featureName)
	newSession := errors.Is(err, state.ErrSessionNotFound)
	if err != nil && !newSession {
		return err
	}
	if newSession {
		session = state.Session{
			CreatedAt:     time.Now().UTC(),
			FeatureDir:    featureDir,
			WorkspaceFile: workspaceFile,
			Repos:         make([]state.Repository, 0, 1),
		}
	}

	session.Repos = append(session.Repos, state.Repository{
		Name:         repoName,
		OriginalPath: repoRoot,
		WorktreePath: worktreePath,
		BaseBranch:   baseBranch,
	})
	if newSession {
		err = state.SaveSession(featureName, session)
	} else {
		err = state.UpdateSession(featureName, session)
	}
	if err != nil {
		return err
	}

	repoNames := make([]string, 0, len(session.Repos))
	for _, repo := range session.Repos {
		repoNames = append(repoNames, repo.Name)
	}
	if err := workspace.Write(session.WorkspaceFile, repoNames); err != nil {
		return err
	}

	fmt.Printf("Added %s to feature %s\n", repoName, featureName)
	return nil
}

func addRemoteRepository(featureName, remoteURL string) error {
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
	worktreePath := filepath.Join(featureDir, repoName)
	workspaceFile := filepath.Join(featureDir, featureDirName+".code-workspace")

	session, err := state.GetSession(featureName)
	newSession := errors.Is(err, state.ErrSessionNotFound)
	if err != nil && !newSession {
		return err
	}
	if newSession {
		session = state.Session{
			CreatedAt:     time.Now().UTC(),
			FeatureDir:    featureDir,
			WorkspaceFile: workspaceFile,
			Repos:         make([]state.Repository, 0, 1),
		}
	}

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
	baseBranch, err := detectBaseBranch(worktreePath)
	if err != nil {
		return err
	}

	if err := gitcmd.CheckoutNewBranch(worktreePath, featureBranchName(featureName)); err != nil {
		return err
	}

	session.Repos = append(session.Repos, state.Repository{
		Name:          repoName,
		OriginalPath:  remoteURL,
		WorktreePath:  worktreePath,
		IsRemoteClone: true,
		BaseBranch:    baseBranch,
	})

	repoNames := make([]string, 0, len(session.Repos))
	for _, repo := range session.Repos {
		repoNames = append(repoNames, repo.Name)
	}
	if err := workspace.Write(session.WorkspaceFile, repoNames); err != nil {
		return err
	}

	if newSession {
		err = state.SaveSession(featureName, session)
	} else {
		err = state.UpdateSession(featureName, session)
	}
	if err != nil {
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

func reviewAllSessions(baseRef string) error {
	reviewState, err := loadReviewState()
	if err != nil {
		return err
	}

	return openReview(reviewState.Sessions, reviewState.DefaultBaseBranch, baseRef)
}

func reviewFeatureSession(featureName, baseRef string) error {
	reviewState, err := loadReviewState()
	if err != nil {
		return err
	}
	session, ok := reviewState.Sessions[featureName]
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrSessionNotFound, featureName)
	}

	return openReview(map[string]state.Session{featureName: session}, reviewState.DefaultBaseBranch, baseRef)
}

func reviewBase(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) != 2 || args[0] != "--base" || args[1] == "" {
		return "", errors.New("invalid review arguments")
	}
	if err := gitcmd.ValidateBranchName(args[1]); err != nil {
		return "", fmt.Errorf("invalid review base %q: %w", args[1], err)
	}

	return args[1], nil
}

func openReview(sessions map[string]state.Session, defaultBaseBranch, overrideBaseBranch string) error {
	generationStarted := time.Now()
	report := preview.Build(sessions, defaultBaseBranch, overrideBaseBranch, time.Now())
	contents, err := preview.Render(report)
	if err != nil {
		return fmt.Errorf("render review: %w", err)
	}

	configPath, err := config.Path()
	if err != nil {
		return err
	}
	snapshotPath := filepath.Join(filepath.Dir(configPath), "review.html")
	if err := preview.WriteSnapshot(snapshotPath, contents); err != nil {
		return err
	}
	generationDuration := time.Since(generationStarted)

	if err := openReviewInBrowser(snapshotPath); err != nil {
		return err
	}

	fmt.Printf("Opened review snapshot %s (generated in %s)\n", snapshotPath, generationDuration)
	return nil
}

func loadReviewState() (state.State, error) {
	reviewState, err := state.Load()
	if err != nil {
		return state.State{}, err
	}

	changed := false
	for featureName, session := range reviewState.Sessions {
		for index := range session.Repos {
			if session.Repos[index].BaseBranch != "" {
				continue
			}

			baseBranch, err := gitcmd.DetectBaseBranch(session.Repos[index].WorktreePath)
			if err != nil || baseBranch == "" {
				baseBranch = reviewState.DefaultBaseBranch
			}
			session.Repos[index].BaseBranch = baseBranch
			changed = true
		}
		reviewState.Sessions[featureName] = session
	}
	if changed {
		if err := state.Save(reviewState); err != nil {
			return state.State{}, err
		}
	}

	return reviewState, nil
}

func detectBaseBranch(repositoryPath string) (string, error) {
	baseBranch, err := gitcmd.DetectBaseBranch(repositoryPath)
	if err != nil {
		return "", err
	}
	if baseBranch != "" {
		return baseBranch, nil
	}

	currentState, err := state.Load()
	if err != nil {
		return "", err
	}
	return currentState.DefaultBaseBranch, nil
}

func openReviewInBrowser(snapshotPath string) error {
	opener := "xdg-open"
	target := (&url.URL{Scheme: "file", Path: snapshotPath}).String()
	if isWSL() {
		windowsPath, err := wslWindowsPath(snapshotPath)
		if err != nil {
			return err
		}
		opener = "explorer.exe"
		target = windowsPath
	}

	if _, err := exec.LookPath(opener); err != nil {
		return fmt.Errorf("find browser opener %q: %w", opener, err)
	}

	command := exec.Command(opener, target)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("open preview with %q: %w", opener, err)
	}
	go func() {
		_ = command.Wait()
	}()

	return nil
}

func isWSL() bool {
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && isWSLRelease(string(release))
}

func isWSLRelease(release string) bool {
	return strings.Contains(strings.ToLower(release), "microsoft")
}

func wslWindowsPath(path string) (string, error) {
	output, err := exec.Command("wslpath", "-w", path).Output()
	if err != nil {
		return "", fmt.Errorf("convert preview path for Windows: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func teardownSession(featureName string, force bool) error {
	session, err := state.GetSession(featureName)
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

func remoteRepositoryName(remoteURL string) (string, error) {
	trimmedURL := strings.TrimSuffix(strings.TrimSpace(remoteURL), "/")
	repoName := strings.TrimSuffix(filepath.Base(trimmedURL), ".git")
	if repoName == "" || repoName == "." || repoName == ".." || filepath.Base(repoName) != repoName {
		return "", fmt.Errorf("invalid remote repository URL %q", remoteURL)
	}

	return repoName, nil
}

func usageError() error {
	return errors.New("usage: autofeat list | autofeat version | autofeat review [--base <branch>] | autofeat <feature-name> [<remote-url>|open|run [-task <prompt>]|review [--base <branch>]|teardown [--force]]")
}
