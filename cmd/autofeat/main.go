// Command autofeat manages ephemeral Git worktrees for AI agent feature sessions.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/painlesshippo/autofeat/internal/config"
	gitcmd "github.com/painlesshippo/autofeat/internal/git"
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
	repoName := filepath.Base(filepath.Clean(repoRoot))
	featureDir := filepath.Join(configuration.WorkspaceBaseDir, featureName)
	worktreePath := filepath.Join(featureDir, repoName)
	workspaceFile := filepath.Join(featureDir, featureName+".code-workspace")

	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return fmt.Errorf("create feature directory: %w", err)
	}
	if err := gitcmd.AddWorktree("agent/"+featureName, worktreePath); err != nil {
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

	featureDir := filepath.Join(configuration.WorkspaceBaseDir, featureName)
	worktreePath := filepath.Join(featureDir, repoName)
	workspaceFile := filepath.Join(featureDir, featureName+".code-workspace")

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

	if err := gitcmd.CheckoutNewBranch(worktreePath, "agent/"+featureName); err != nil {
		return err
	}

	session.Repos = append(session.Repos, state.Repository{
		Name:          repoName,
		OriginalPath:  remoteURL,
		WorktreePath:  worktreePath,
		IsRemoteClone: true,
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
			unpushed, err := gitcmd.HasUnpushedCommits(repo.WorktreePath, "agent/"+featureName)
			if err != nil {
				return err
			}
			if unpushed {
				fmt.Fprintf(os.Stderr, "Warning: %s has unpushed agent commits that will be deleted.\n", repo.Name)
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
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("invalid feature name %q", name)
	}

	return nil
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
	return errors.New("usage: autofeat list | autofeat version | autofeat <feature-name> [<remote-url>|open|teardown [--force]]")
}
