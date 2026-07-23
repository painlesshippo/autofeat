package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/state"
)

func TestVersionCommand(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatalf("run(version) error = %v", err)
	}

	if err := run([]string{"version", "extra"}); err == nil {
		t.Error("run(version, extra) error = nil, want usage error")
	}
}

func TestReviewCommandDispatch(t *testing.T) {
	originalReviewCommand := reviewCommand
	t.Cleanup(func() {
		reviewCommand = originalReviewCommand
	})

	var gotSelectors []string
	var gotBase string
	reviewCommand = func(selectors []string, baseRef string) error {
		gotSelectors = selectors
		gotBase = baseRef
		return nil
	}

	if err := run([]string{"review"}); err != nil {
		t.Fatalf("run(review) error = %v", err)
	}
	if len(gotSelectors) != 1 || gotSelectors[0] != "*" || gotBase != "" {
		t.Errorf("run(review) = (%q, %q), want ([*], stored base)", gotSelectors, gotBase)
	}

	if err := run([]string{"review", "feature/*", "--base", "develop"}); err != nil {
		t.Fatalf("run(review feature/* --base develop) error = %v", err)
	}
	if len(gotSelectors) != 1 || gotSelectors[0] != "feature/*" || gotBase != "develop" {
		t.Errorf("review dispatch = (%q, %q), want ([feature/*], develop)", gotSelectors, gotBase)
	}

	if err := run([]string{"review", "--base"}); err == nil {
		t.Error("run(review --base) error = nil, want usage error")
	}
}

func TestStatusCommandDispatch(t *testing.T) {
	originalStatusCommand := statusCommand
	t.Cleanup(func() {
		statusCommand = originalStatusCommand
	})

	var gotSelectors []string
	statusCommand = func(selectors []string) error {
		gotSelectors = selectors
		return nil
	}

	if err := run([]string{"status"}); err != nil {
		t.Fatalf("run(status) error = %v", err)
	}
	if len(gotSelectors) != 1 || gotSelectors[0] != "*" {
		t.Errorf("run(status) selectors = %q, want [*]", gotSelectors)
	}

	if err := run([]string{"status", "feature/a", "feature/b"}); err != nil {
		t.Fatalf("run(status feature/a feature/b) error = %v", err)
	}
	if len(gotSelectors) != 2 || gotSelectors[0] != "feature/a" || gotSelectors[1] != "feature/b" {
		t.Errorf("run(status feature/a feature/b) selectors = %q, want exact selectors", gotSelectors)
	}

	if err := run([]string{"status", "--json"}); err == nil {
		t.Error("run(status --json) error = nil, want usage error")
	}
}

func TestReviewArgumentsRecognizesShellExpandedWildcard(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"bin", "cmd", "README.md"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	selectors, baseRef, err := reviewArguments([]string{"README.md", "bin", "cmd"})
	if err != nil {
		t.Fatalf("reviewArguments(shell expansion) error = %v", err)
	}
	if len(selectors) != 1 || selectors[0] != "*" || baseRef != "" {
		t.Errorf("reviewArguments(shell expansion) = (%q, %q), want ([*], empty base)", selectors, baseRef)
	}
}

func TestRunCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/a", "feature/b"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}
	originalRunFeatureCommand := runFeatureCommand
	t.Cleanup(func() {
		runFeatureCommand = originalRunFeatureCommand
	})

	var gotFeatureNames []string
	var gotTask string
	runFeatureCommand = func(featureName, task string) error {
		gotFeatureNames = append(gotFeatureNames, featureName)
		gotTask = task
		return nil
	}
	if err := run([]string{"run", "feature/a"}); err != nil {
		t.Fatalf("run(run feature/a) error = %v", err)
	}
	if len(gotFeatureNames) != 1 || gotFeatureNames[0] != "feature/a" || gotTask != "" {
		t.Errorf("run command = (%q, %q), want ([feature/a], empty task)", gotFeatureNames, gotTask)
	}

	gotFeatureNames = nil
	if err := run([]string{"run", "feature/*", "-task", "write tests"}); err != nil {
		t.Fatalf("run(run feature/* -task) error = %v", err)
	}
	if len(gotFeatureNames) != 2 || gotFeatureNames[0] != "feature/a" || gotFeatureNames[1] != "feature/b" || gotTask != "write tests" {
		t.Errorf("run wildcard = (%q, %q), want sorted features and task", gotFeatureNames, gotTask)
	}

	if err := run([]string{"run", "feature/a", "-task"}); err == nil {
		t.Error("run(run feature/a -task) error = nil, want usage error")
	}
}

func TestSyncCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := state.SaveSession("feature/sync", state.Session{}); err != nil {
		t.Fatal(err)
	}
	originalSyncFeatureCommand := syncFeatureCommand
	t.Cleanup(func() {
		syncFeatureCommand = originalSyncFeatureCommand
	})

	var gotFeatureName string
	syncFeatureCommand = func(featureName string) error {
		gotFeatureName = featureName
		return nil
	}

	if err := run([]string{"sync", "feature/sync"}); err != nil {
		t.Fatalf("run(sync feature/sync) error = %v", err)
	}
	if gotFeatureName != "feature/sync" {
		t.Errorf("sync feature = %q, want feature/sync", gotFeatureName)
	}
}

func TestTeardownCommandDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"feature/x", "feature/y"} {
		if err := state.SaveSession(name, state.Session{}); err != nil {
			t.Fatal(err)
		}
	}
	originalTeardownCommand := teardownCommand
	t.Cleanup(func() {
		teardownCommand = originalTeardownCommand
	})

	var gotFeatureNames []string
	var gotForce bool
	teardownCommand = func(featureName string, force bool) error {
		gotFeatureNames = append(gotFeatureNames, featureName)
		gotForce = force
		return nil
	}

	if err := run([]string{"teardown", "feature/y", "feature/x", "--force"}); err != nil {
		t.Fatalf("run(teardown feature/y feature/x --force) error = %v", err)
	}
	if len(gotFeatureNames) != 2 || gotFeatureNames[0] != "feature/x" || gotFeatureNames[1] != "feature/y" || !gotForce {
		t.Errorf("teardown dispatch = (%q, %t), want sorted features and force", gotFeatureNames, gotForce)
	}
	if err := run([]string{"feature/x", "teardown"}); err == nil {
		t.Error("run(feature/x teardown) error = nil, want command-first usage error")
	}
}

func TestSelectFeatureNames(t *testing.T) {
	sessions := map[string]state.Session{
		"bug/fix":             {},
		"feature/a21":         {},
		"feature/beta":        {},
		"feature/team/nested": {},
	}
	tests := []struct {
		name      string
		selectors []string
		want      []string
	}{
		{name: "exact", selectors: []string{"feature/a21"}, want: []string{"feature/a21"}},
		{name: "all", selectors: []string{"*"}, want: []string{"bug/fix", "feature/a21", "feature/beta", "feature/team/nested"}},
		{name: "prefix pattern", selectors: []string{"feature/*"}, want: []string{"feature/a21", "feature/beta", "feature/team/nested"}},
		{name: "overlapping selectors", selectors: []string{"feature/*", "feature/a21"}, want: []string{"feature/a21", "feature/beta", "feature/team/nested"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectFeatureNames(sessions, test.selectors)
			if err != nil {
				t.Fatalf("selectFeatureNames(%q) error = %v", test.selectors, err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("selectFeatureNames(%q) = %q, want %q", test.selectors, got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("selectFeatureNames(%q) = %q, want %q", test.selectors, got, test.want)
					break
				}
			}
		})
	}

	for _, selectors := range [][]string{nil, {"feature/missing"}, {"feature/["}} {
		if _, err := selectFeatureNames(sessions, selectors); err == nil {
			t.Errorf("selectFeatureNames(%q) error = nil, want error", selectors)
		}
	}
}

func TestSyncFeatureFetchesAndRebases(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath, remotePath := createRemoteMainRepository(t)
	worktreePath := filepath.Join(t.TempDir(), "clone")
	runMainGit(t, t.TempDir(), "clone", "-q", remotePath, worktreePath)
	runMainGit(t, worktreePath, "config", "user.email", "test@example.com")
	runMainGit(t, worktreePath, "config", "user.name", "Test User")
	runMainGit(t, worktreePath, "checkout", "-qb", "feature/sync", "origin/main")
	writeAndCommitMainFile(t, worktreePath, "feature.txt", "feature\n", "feature change")
	writeAndCommitMainFile(t, sourcePath, "base.txt", "base\n", "base change")
	runMainGit(t, sourcePath, "push", "-q", "origin", "main")

	if err := state.SaveSession("feature/sync", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: worktreePath, BaseBranch: "main",
	}}}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if err := syncFeature("feature/sync"); err != nil {
		t.Fatalf("syncFeature() error = %v", err)
	}

	status, err := gitcmd.CachedBaseStatus(worktreePath, "main")
	if err != nil {
		t.Fatalf("CachedBaseStatus() error = %v", err)
	}
	if status.Ahead != 1 || status.Behind != 0 {
		t.Errorf("status after sync = (%d ahead, %d behind), want (1, 0)", status.Ahead, status.Behind)
	}
}

func TestSyncFeatureDirtyPreflightDoesNotFetch(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath, remotePath := createRemoteMainRepository(t)
	worktreePath := filepath.Join(t.TempDir(), "clone")
	runMainGit(t, t.TempDir(), "clone", "-q", remotePath, worktreePath)
	runMainGit(t, worktreePath, "checkout", "-qb", "feature/dirty", "origin/main")
	originalBase := mainGitOutput(t, worktreePath, "rev-parse", "origin/main")
	writeAndCommitMainFile(t, sourcePath, "base.txt", "base\n", "base change")
	runMainGit(t, sourcePath, "push", "-q", "origin", "main")
	if err := os.WriteFile(filepath.Join(worktreePath, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := state.SaveSession("feature/dirty", state.Session{Repos: []state.Repository{{
		Name: "repository", WorktreePath: worktreePath, BaseBranch: "main",
	}}}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if err := syncFeature("feature/dirty"); err == nil {
		t.Fatal("syncFeature() error = nil, want dirty worktree error")
	}
	if got := mainGitOutput(t, worktreePath, "rev-parse", "origin/main"); got != originalBase {
		t.Errorf("origin/main changed during failed preflight: got %q, want %q", got, originalBase)
	}
}

func TestSessionDrift(t *testing.T) {
	requireMainGit(t)

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	runMainGit(t, repoPath, "checkout", "-qb", "feature/drift")
	writeAndCommitMainFile(t, repoPath, "feature.txt", "feature\n", "feature change")
	runMainGit(t, repoPath, "checkout", "main")
	writeAndCommitMainFile(t, repoPath, "base.txt", "base\n", "base change")
	runMainGit(t, repoPath, "checkout", "feature/drift")

	session := state.Session{Repos: []state.Repository{{Name: "repository", WorktreePath: repoPath, BaseBranch: "main"}}}
	if got := sessionDrift(session, "master"); got != "1 behind" {
		t.Errorf("sessionDrift() = %q, want 1 behind", got)
	}
	session.Repos[0].WorktreePath = filepath.Join(t.TempDir(), "missing")
	if got := sessionDrift(session, "master"); got != "unknown" {
		t.Errorf("sessionDrift() missing repository = %q, want unknown", got)
	}
}

func TestStatusStatePrecedence(t *testing.T) {
	tests := []struct {
		name   string
		status repositoryStatus
		want   string
	}{
		{name: "error", status: repositoryStatus{inspectionError: true, missing: true}, want: "error"},
		{name: "missing", status: repositoryStatus{missing: true, detached: true}, want: "missing"},
		{name: "detached", status: repositoryStatus{detached: true, wrongBranch: true}, want: "detached"},
		{name: "wrong branch", status: repositoryStatus{wrongBranch: true, rebasing: true}, want: "wrong-branch"},
		{name: "rebasing", status: repositoryStatus{rebasing: true, baseKnown: true, baseBehind: 1}, want: "rebasing"},
		{name: "behind base", status: repositoryStatus{baseKnown: true, baseBehind: 1, dirty: true}, want: "behind-base"},
		{name: "dirty", status: repositoryStatus{dirty: true, pushKnown: true, pushAhead: 1, pushBehind: 1}, want: "dirty"},
		{name: "remote diverged", status: repositoryStatus{pushKnown: true, pushAhead: 1, pushBehind: 1}, want: "remote-diverged"},
		{name: "remote ahead", status: repositoryStatus{pushKnown: true, pushBehind: 1}, want: "remote-ahead"},
		{name: "unpublished", status: repositoryStatus{pushKnown: true, pushHasOrigin: true}, want: "unpushed"},
		{name: "unpushed commits", status: repositoryStatus{pushKnown: true, pushHasOrigin: true, pushPublished: true, pushAhead: 1}, want: "unpushed"},
		{name: "ready without origin", status: repositoryStatus{baseKnown: true, pushKnown: true}, want: "ready"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := statusState(test.status); got != test.want {
				t.Errorf("statusState(%+v) = %q, want %q", test.status, got, test.want)
			}
		})
	}
}

func TestStatusSessionsRendersSortedRepositoryHealth(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	dirtyRepo := createMainRepository(t)
	runMainGit(t, dirtyRepo, "branch", "-M", "main")
	runMainGit(t, dirtyRepo, "checkout", "-qb", "feature/alpha")
	if err := os.WriteFile(filepath.Join(dirtyRepo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	readyRepo := createMainRepository(t)
	runMainGit(t, readyRepo, "branch", "-M", "main")
	runMainGit(t, readyRepo, "checkout", "-qb", "feature/beta")
	notRepository := t.TempDir()
	missingPath := filepath.Join(t.TempDir(), "missing")

	if err := state.SaveSession("feature/beta", state.Session{Repos: []state.Repository{{
		Name: "ready", WorktreePath: readyRepo, BaseBranch: "main",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSession("feature/alpha", state.Session{Repos: []state.Repository{
		{Name: "z-error", WorktreePath: notRepository, BaseBranch: "main"},
		{Name: "a-missing", WorktreePath: missingPath},
		{Name: "m-dirty", WorktreePath: dirtyRepo, BaseBranch: "main"},
	}}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := statusSessionsTo(&output, []string{"*"}); err != nil {
		t.Fatalf("statusSessionsTo() error = %v", err)
	}
	report := output.String()
	for _, want := range []string{
		"FEATURE", "REPOSITORY", "WORKTREE", "DRIFT", "PUSH", "STATE", "DETAIL",
		"feature/alpha", "a-missing", "master", "missing",
		"m-dirty", "feature/alpha", "dirty", "+0/-0", "n/a",
		"z-error", "error",
		"feature/beta", "ready", "feature/beta", "clean",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("status report does not contain %q:\n%s", want, report)
		}
	}

	positions := []int{
		strings.Index(report, "a-missing"),
		strings.Index(report, "m-dirty"),
		strings.Index(report, "z-error"),
		strings.Index(report, "feature/beta"),
	}
	for index := 1; index < len(positions); index++ {
		if positions[index-1] < 0 || positions[index] <= positions[index-1] {
			t.Fatalf("status report is not sorted as expected: positions %v\n%s", positions, report)
		}
	}

	output.Reset()
	if err := statusSessionsTo(&output, []string{"feature/beta"}); err != nil {
		t.Fatalf("statusSessionsTo(feature/beta) error = %v", err)
	}
	if selectedReport := output.String(); !strings.Contains(selectedReport, "feature/beta") || strings.Contains(selectedReport, "feature/alpha") {
		t.Errorf("selected status report contains the wrong features:\n%s", selectedReport)
	}
	if err := statusSessionsTo(&output, []string{"feature/missing"}); err == nil {
		t.Error("statusSessionsTo(feature/missing) error = nil, want no-match error")
	}
}

func TestStatusSessionsEmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output bytes.Buffer
	if err := statusSessionsTo(&output, []string{"*"}); err != nil {
		t.Fatalf("statusSessionsTo() empty state error = %v", err)
	}
	if fields := strings.Fields(output.String()); len(fields) != 9 {
		t.Errorf("empty status report fields = %q, want header only", fields)
	}
}

func TestInspectRepositoryStatusGitStates(t *testing.T) {
	requireMainGit(t)

	t.Run("wrong branch and detached", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "other")
		repository := state.Repository{Name: "repository", WorktreePath: repoPath, BaseBranch: "main"}

		status := inspectRepositoryStatus("feature/status", repository, "master")
		if got := statusState(status); got != "wrong-branch" {
			t.Errorf("wrong branch state = %q, want wrong-branch; status = %+v", got, status)
		}

		runMainGit(t, repoPath, "checkout", "--detach", "-q")
		status = inspectRepositoryStatus("feature/status", repository, "master")
		if got := statusState(status); got != "detached" {
			t.Errorf("detached state = %q, want detached; status = %+v", got, status)
		}
	})

	t.Run("behind base", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "feature/status")
		runMainGit(t, repoPath, "checkout", "main")
		writeAndCommitMainFile(t, repoPath, "base.txt", "base\n", "base change")
		runMainGit(t, repoPath, "checkout", "feature/status")

		status := inspectRepositoryStatus("feature/status", state.Repository{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}, "master")
		if got := statusState(status); got != "behind-base" || status.baseBehind != 1 {
			t.Errorf("behind base status = %+v, state %q; want one behind and behind-base", status, got)
		}
	})

	t.Run("rebasing", func(t *testing.T) {
		repoPath := createMainRepository(t)
		runMainGit(t, repoPath, "branch", "-M", "main")
		runMainGit(t, repoPath, "checkout", "-qb", "feature/status")
		rebasePath := strings.TrimSpace(mainGitOutput(t, repoPath, "rev-parse", "--git-path", "rebase-merge"))
		if !filepath.IsAbs(rebasePath) {
			rebasePath = filepath.Join(repoPath, rebasePath)
		}
		if err := os.MkdirAll(rebasePath, 0o755); err != nil {
			t.Fatal(err)
		}

		status := inspectRepositoryStatus("feature/status", state.Repository{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}, "master")
		if got := statusState(status); got != "rebasing" {
			t.Errorf("rebase state = %q, want rebasing; status = %+v", got, status)
		}
	})

	t.Run("unpublished", func(t *testing.T) {
		_, remotePath := createRemoteMainRepository(t)
		repoPath := filepath.Join(t.TempDir(), "clone")
		runMainGit(t, t.TempDir(), "clone", "-q", remotePath, repoPath)
		runMainGit(t, repoPath, "checkout", "-qb", "feature/status", "origin/main")

		status := inspectRepositoryStatus("feature/status", state.Repository{
			Name: "repository", WorktreePath: repoPath, BaseBranch: "main",
		}, "master")
		if got := statusState(status); got != "unpushed" || formatPushStatus(status) != "unpublished" {
			t.Errorf("unpublished status = %+v, state %q, push %q", status, got, formatPushStatus(status))
		}
	})
}

func requireMainGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is not available")
	}
}

func createMainRepository(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()
	runMainGit(t, repoPath, "init", "-q")
	runMainGit(t, repoPath, "config", "user.email", "test@example.com")
	runMainGit(t, repoPath, "config", "user.name", "Test User")
	writeAndCommitMainFile(t, repoPath, "README.md", "initial\n", "initial commit")
	return repoPath
}

func createRemoteMainRepository(t *testing.T) (string, string) {
	t.Helper()
	sourcePath := createMainRepository(t)
	runMainGit(t, sourcePath, "branch", "-M", "main")
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	runMainGit(t, sourcePath, "init", "--bare", "-q", remotePath)
	runMainGit(t, sourcePath, "remote", "add", "origin", remotePath)
	runMainGit(t, sourcePath, "push", "-qu", "origin", "main")
	return sourcePath, remotePath
}

func writeAndCommitMainFile(t *testing.T, repoPath, name, contents, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runMainGit(t, repoPath, "add", name)
	runMainGit(t, repoPath, "commit", "-qm", message)
}

func runMainGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func mainGitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

func TestAppendTask(t *testing.T) {
	featureDir := t.TempDir()
	if err := appendTask(featureDir, "first task"); err != nil {
		t.Fatalf("appendTask() first write error = %v", err)
	}
	if err := appendTask(featureDir, "second task"); err != nil {
		t.Fatalf("appendTask() second write error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(featureDir, "TASK.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "first task\nsecond task\n"; got != want {
		t.Errorf("TASK.md = %q, want %q", got, want)
	}
}

func TestFeatureBranchName(t *testing.T) {
	if got, want := featureBranchName("feature/potato"), "feature/potato"; got != want {
		t.Errorf("featureBranchName() = %q, want feature name", got)
	}
}

func TestHeadlessArgs(t *testing.T) {
	got := headlessArgs()
	if len(got) != 2 || got[0] != "-i" || got[1] != headlessPrompt {
		t.Errorf("headlessArgs() = %q, want [-i %q]", got, headlessPrompt)
	}
}

func TestIsWSLRelease(t *testing.T) {
	for release, want := range map[string]bool{
		"6.6.87.2-microsoft-standard-WSL2":   true,
		"5.15.153.1-microsoft-standard-WSL2": true,
		"6.8.0-31-generic":                   false,
	} {
		if got := isWSLRelease(release); got != want {
			t.Errorf("isWSLRelease(%q) = %t, want %t", release, got, want)
		}
	}
}

func TestIsRemoteURL(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"http://github.com/example/repo.git",
		"https://github.com/example/repo.git",
		"git@github.com:example/repo.git",
	} {
		if !isRemoteURL(value) {
			t.Errorf("isRemoteURL(%q) = false, want true", value)
		}
	}

	if isRemoteURL("ssh://git@github.com/example/repo.git") {
		t.Error("isRemoteURL() accepted an unsupported URL prefix")
	}
}

func TestRemoteRepositoryName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"http://github.com/example/repo-name.git":  "repo-name",
		"https://github.com/example/repo-name.git": "repo-name",
		"git@github.com:example/repo-name.git":     "repo-name",
		"https://github.com/example/repo-name/":    "repo-name",
	}
	for remoteURL, want := range tests {
		got, err := remoteRepositoryName(remoteURL)
		if err != nil {
			t.Errorf("remoteRepositoryName(%q) error = %v", remoteURL, err)
			continue
		}
		if got != want {
			t.Errorf("remoteRepositoryName(%q) = %q, want %q", remoteURL, got, want)
		}
	}
}

func TestValidateFeatureName(t *testing.T) {
	for _, featureName := range []string{
		"feature/potato",
		"bug/f321s-aaa",
		"feature/team/potato",
		"flat-feature",
	} {
		if err := validateFeatureName(featureName); err != nil {
			t.Errorf("validateFeatureName(%q) error = %v", featureName, err)
		}
	}

	for _, featureName := range []string{
		"",
		"/feature",
		"feature/",
		"feature//potato",
		"feature/.",
		"feature/..",
		"feature/potato.lock",
		"feature potato",
		"feature?potato",
	} {
		if err := validateFeatureName(featureName); err == nil {
			t.Errorf("validateFeatureName(%q) error = nil, want error", featureName)
		}
	}
}

func TestFeatureDirectoryName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"flat-feature":           "flat-feature",
		"feature/potato":         "feature%2Fpotato",
		"feature/team/potato":    "feature%2Fteam%2Fpotato",
		"feature%2Fpotato":       "feature%252Fpotato",
		"feature/potato%2Fextra": "feature%2Fpotato%252Fextra",
	}
	for featureName, want := range tests {
		if got := featureDirectoryName(featureName); got != want {
			t.Errorf("featureDirectoryName(%q) = %q, want %q", featureName, got, want)
		}
	}
}
