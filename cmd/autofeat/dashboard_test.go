package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/painlesshippo/autofeat/internal/artifact"
	"github.com/painlesshippo/autofeat/internal/state"
)

func TestClassifyStale(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	clean := func(ahead int, merged bool) dashboardRepository {
		return dashboardRepository{status: repositoryStatus{baseKnown: true, baseAhead: ahead}, merged: merged}
	}
	tests := []struct {
		name         string
		repos        []dashboardRepository
		lastActivity time.Time
		staleAfter   time.Duration
		want         string
	}{
		{name: "fresh work", repos: []dashboardRepository{clean(2, false)}, lastActivity: now.Add(-time.Hour), staleAfter: defaultStaleAfter, want: ""},
		{name: "fresh empty session", repos: []dashboardRepository{clean(0, false)}, lastActivity: now, staleAfter: defaultStaleAfter, want: ""},
		{name: "merged", repos: []dashboardRepository{clean(2, true)}, lastActivity: now, staleAfter: defaultStaleAfter, want: "merged"},
		{name: "merged with empty repository", repos: []dashboardRepository{clean(2, true), clean(0, false)}, lastActivity: now, want: "merged"},
		{name: "merged but other repository has work", repos: []dashboardRepository{clean(2, true), clean(1, false)}, lastActivity: now, want: ""},
		{name: "merged but dirty", repos: []dashboardRepository{{status: repositoryStatus{baseKnown: true, baseAhead: 1, dirty: true}, merged: true}}, lastActivity: now, want: ""},
		{name: "merged but rebasing", repos: []dashboardRepository{{status: repositoryStatus{baseKnown: true, baseAhead: 1, rebasing: true}, merged: true}}, lastActivity: now, want: ""},
		{name: "all worktrees missing", repos: []dashboardRepository{{status: repositoryStatus{missing: true}}}, lastActivity: now, want: "missing"},
		{name: "idle", repos: []dashboardRepository{clean(1, false)}, lastActivity: now.Add(-20 * 24 * time.Hour), staleAfter: defaultStaleAfter, want: "idle 20d"},
		{name: "idle rule disabled", repos: []dashboardRepository{clean(1, false)}, lastActivity: now.Add(-20 * 24 * time.Hour), staleAfter: 0, want: ""},
		{name: "no repositories idle", lastActivity: now.Add(-15 * 24 * time.Hour), staleAfter: defaultStaleAfter, want: "idle 15d"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyStale(test.repos, test.lastActivity, now, test.staleAfter); got != test.want {
				t.Errorf("classifyStale() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDashCommandDispatch(t *testing.T) {
	originalDashboardCommand := dashboardCommand
	t.Cleanup(func() {
		dashboardCommand = originalDashboardCommand
	})

	var gotStaleAfter time.Duration
	dashboardCommand = func(staleAfter time.Duration) error {
		gotStaleAfter = staleAfter
		return nil
	}

	if err := run([]string{"dash"}); err != nil {
		t.Fatalf("run(dash) error = %v", err)
	}
	if gotStaleAfter != defaultStaleAfter {
		t.Errorf("run(dash) staleAfter = %v, want %v", gotStaleAfter, defaultStaleAfter)
	}
	if err := run([]string{"dash", "--stale-after", "72h"}); err != nil {
		t.Fatalf("run(dash --stale-after 72h) error = %v", err)
	}
	if gotStaleAfter != 72*time.Hour {
		t.Errorf("run(dash --stale-after 72h) staleAfter = %v, want 72h", gotStaleAfter)
	}
	if err := run([]string{"dash", "feature/a"}); err == nil {
		t.Error("run(dash feature/a) error = nil, want argument error")
	}
}

func TestDashboardModelNavigationFilterAndTeardownConfirmation(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	model := newDashboardModel("autofeat", defaultStaleAfter)
	model.now = func() time.Time { return now }
	sessions := []dashboardSession{
		{name: "feature/alpha", lastActivity: now, drift: "current", repos: []dashboardRepository{{
			status:   repositoryStatus{repositoryName: "api", branch: "feature/alpha", baseBranch: "main", baseKnown: true, baseAhead: 1, worktree: "clean"},
			diffStat: " api.go | 2 +-\n",
		}}},
		{name: "feature/beta", lastActivity: now.Add(-30 * 24 * time.Hour), drift: "2 behind", stale: "idle 30d"},
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	updated, _ = updated.Update(dashboardLoadedMsg{sessions: sessions})
	model = updated.(dashboardModel)
	rendered := model.render()
	for _, want := range []string{"2 sessions · 1 stale", "feature/alpha", "feature/beta", "api.go | 2 +-", "base main +1/-0"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("render() missing %q:\n%s", want, rendered)
		}
	}

	model = pressKeys(t, model, "j")
	if session, _ := model.current(); session.name != "feature/beta" {
		t.Fatalf("selection after j = %q, want feature/beta", session.name)
	}
	if rendered := model.render(); !strings.Contains(rendered, "stale: idle 30d") {
		t.Errorf("render() for stale session missing stale reason:\n%s", rendered)
	}

	model = pressKeys(t, model, "t")
	if model.mode != dashboardConfirmTeardown || !strings.Contains(model.render(), "Tear down feature/beta?") {
		t.Fatalf("t did not request teardown confirmation:\n%s", model.render())
	}
	model = pressKeys(t, model, "n")
	if model.mode != dashboardList || model.message != "teardown cancelled" {
		t.Errorf("n after t: mode = %v, message = %q; want list mode and cancellation", model.mode, model.message)
	}
	model = pressKeys(t, model, "t")
	updated, cmd := model.Update(keyPress("y"))
	model = updated.(dashboardModel)
	if cmd == nil || model.mode != dashboardList {
		t.Error("y after t did not start teardown")
	}

	model = pressKeys(t, model, "/", "a", "l", "enter")
	if len(model.visible) != 1 || model.sessions[model.visible[0]].name != "feature/alpha" {
		t.Errorf("filter al visible = %v, want only feature/alpha", model.visible)
	}
	model = pressKeys(t, model, "/", "esc")
	if len(model.visible) != 2 {
		t.Errorf("esc in filter left %d visible sessions, want 2", len(model.visible))
	}

	model = pressKeys(t, model, "g")
	updated, cmd = model.Update(keyPress("enter"))
	model = updated.(dashboardModel)
	if model.mode != dashboardDiff || cmd == nil {
		t.Fatal("enter did not open the diff view")
	}
	updated, _ = model.Update(dashboardDiffMsg{feature: "feature/alpha", repository: 0, content: "+added line\n"})
	model = updated.(dashboardModel)
	if rendered := model.render(); !strings.Contains(rendered, "+added line") || !strings.Contains(rendered, "api (1/1) vs merge-base with main") {
		t.Errorf("diff view missing content or header:\n%s", rendered)
	}
	model = pressKeys(t, model, "esc")
	if model.mode != dashboardList {
		t.Errorf("esc in diff mode = %v, want list mode", model.mode)
	}
}

func TestDashboardModelReloadKeepsSelectionWhenSessionsDisappear(t *testing.T) {
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/a"}, {name: "feature/b"}, {name: "feature/c"}}})
	model = pressKeys(t, updated.(dashboardModel), "G")

	updated, _ = model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/a"}}})
	model = updated.(dashboardModel)
	if got := model.currentName(); got != "feature/a" {
		t.Errorf("selection after feature/c disappeared = %q, want feature/a", got)
	}

	updated, _ = model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/b"}, {name: "feature/a"}}})
	model = updated.(dashboardModel)
	if got := model.currentName(); got != "feature/a" {
		t.Errorf("selection after reorder = %q, want feature/a", got)
	}

	updated, _ = model.Update(dashboardLoadedMsg{})
	model = updated.(dashboardModel)
	if _, ok := model.current(); ok || !strings.Contains(model.render(), "No sessions") {
		t.Errorf("empty reload: current ok = %t, render:\n%s", ok, model.render())
	}
}

func TestLoadDashboardSessionsInspectsChangesAndMergedWork(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	sourcePath, remotePath := createRemoteMainRepository(t)
	clone := func(name, branch string) string {
		path := filepath.Join(t.TempDir(), name)
		runMainGit(t, t.TempDir(), "clone", "-q", remotePath, path)
		runMainGit(t, path, "config", "user.email", "test@example.com")
		runMainGit(t, path, "config", "user.name", "Test User")
		runMainGit(t, path, "checkout", "-qb", branch, "origin/main")
		return path
	}

	activePath := clone("active", "feature/active")
	writeAndCommitMainFile(t, activePath, "active.txt", "active\n", "active change")
	writeMainFile(t, activePath, "scratch.txt", "untracked\n")

	mergedPath := clone("merged", "feature/merged")
	writeAndCommitMainFile(t, mergedPath, "merged.txt", "merged\n", "merged change")
	runMainGit(t, sourcePath, "fetch", "-q", mergedPath, "feature/merged")
	runMainGit(t, sourcePath, "merge", "-q", "--squash", "FETCH_HEAD")
	runMainGit(t, sourcePath, "commit", "-qm", "squash merge")

	mergeCommitPath := clone("merge-commit", "feature/merge-commit")
	writeAndCommitMainFile(t, mergeCommitPath, "merge-commit.txt", "merge commit\n", "merge commit change")
	runMainGit(t, sourcePath, "fetch", "-q", mergeCommitPath, "feature/merge-commit")
	runMainGit(t, sourcePath, "merge", "-q", "--no-ff", "-m", "merge feature", "FETCH_HEAD")
	runMainGit(t, sourcePath, "push", "-q", "origin", "main")
	runMainGit(t, mergedPath, "fetch", "-q", "origin")
	runMainGit(t, mergeCommitPath, "fetch", "-q", "origin")

	for name, path := range map[string]string{"feature/active": activePath, "feature/merged": mergedPath, "feature/merge-commit": mergeCommitPath} {
		if err := state.SaveSession(name, state.Session{CreatedAt: time.Now(), Repos: []state.Repository{{
			Name: "repository", WorktreePath: path, BaseBranch: "main",
		}}}); err != nil {
			t.Fatalf("SaveSession(%s) error = %v", name, err)
		}
	}

	sessions, err := loadDashboardSessions(defaultStaleAfter, time.Now())
	if err != nil {
		t.Fatalf("loadDashboardSessions() error = %v", err)
	}
	byName := map[string]dashboardSession{}
	for _, session := range sessions {
		byName[session.name] = session
	}

	active := byName["feature/active"]
	if active.stale != "" || len(active.repos) != 1 {
		t.Fatalf("active session = stale %q with %d repositories, want fresh with 1", active.stale, len(active.repos))
	}
	if repo := active.repos[0]; !strings.Contains(repo.diffStat, "active.txt") || len(repo.untracked) != 1 || repo.merged {
		t.Errorf("active repository = stat %q, untracked %v, merged %t", repo.diffStat, repo.untracked, repo.merged)
	}

	for _, name := range []string{"feature/merged", "feature/merge-commit"} {
		if merged := byName[name]; merged.stale != "merged" {
			t.Errorf("%s stale = %q, want merged", name, merged.stale)
		}
	}
}

func TestDashboardModelTeardownConfirmationKeepsItsTarget(t *testing.T) {
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/a"}, {name: "feature/b"}}})
	model = pressKeys(t, updated.(dashboardModel), "t")

	updated, _ = model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/c"}, {name: "feature/a"}}})
	model = updated.(dashboardModel)
	if model.mode != dashboardConfirmTeardown || !strings.Contains(model.render(), "Tear down feature/a?") {
		t.Fatalf("reload with target present: mode = %v, render:\n%s", model.mode, model.render())
	}

	updated, _ = model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/b"}}})
	model = updated.(dashboardModel)
	if model.mode != dashboardList || !strings.Contains(model.message, "teardown cancelled") {
		t.Fatalf("reload without target: mode = %v, message = %q; want cancelled", model.mode, model.message)
	}
	if updated, cmd := model.Update(keyPress("y")); cmd != nil {
		t.Errorf("y after the target disappeared returned a command; model = %+v", updated)
	}
}

func TestDashboardModelFilterAcceptsPaste(t *testing.T) {
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/alpha"}, {name: "feature/beta"}}})
	model = pressKeys(t, updated.(dashboardModel), "/")

	updated, _ = model.Update(tea.PasteMsg{Content: "beta"})
	model = updated.(dashboardModel)
	if model.filter.Value() != "beta" || len(model.visible) != 1 || model.currentName() != "feature/beta" {
		t.Errorf("paste filter = %q with %d visible, selected %q; want beta, 1, feature/beta",
			model.filter.Value(), len(model.visible), model.currentName())
	}
}

func TestDashboardModelDetailScrollsAndFitsWidth(t *testing.T) {
	repos := make([]dashboardRepository, 0, 8)
	for index := range 8 {
		repos = append(repos, dashboardRepository{
			status:   repositoryStatus{repositoryName: fmt.Sprintf("repo-%d", index+1), branch: "feature/wide", baseBranch: "main", baseKnown: true, worktree: "clean"},
			diffStat: strings.Repeat("x", 200) + " | 1 +\n",
		})
	}
	sessions := []dashboardSession{{name: "feature/wide", lastActivity: time.Now(), drift: "current", repos: repos}, {name: "feature/other", drift: "current"}}

	for _, width := range []int{40, 80, 160} {
		model := newDashboardModel("autofeat", defaultStaleAfter)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		updated, _ = updated.Update(dashboardLoadedMsg{sessions: sessions})
		model = updated.(dashboardModel)
		for _, line := range strings.Split(model.render(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: rendered line is %d columns wide: %q", width, got, line)
			}
		}
	}

	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	updated, _ = updated.Update(dashboardLoadedMsg{sessions: sessions})
	model = updated.(dashboardModel)
	if rendered := model.render(); strings.Contains(rendered, "repo-8") || !strings.Contains(rendered, "pgup/pgdn scroll") {
		t.Fatalf("initial render shows repo-8 or lacks the scroll hint:\n%s", rendered)
	}
	model = pressKeys(t, model, "pgdown", "pgdown", "pgdown")
	if rendered := model.render(); !strings.Contains(rendered, "repo-8") {
		t.Errorf("render after scrolling does not reveal repo-8:\n%s", rendered)
	}
	model = pressKeys(t, model, "j")
	if model.detailOffset != 0 {
		t.Errorf("detailOffset after selecting another session = %d, want 0", model.detailOffset)
	}
}

func TestDashboardModelMouseWheelScrollsPaneUnderPointer(t *testing.T) {
	t.Parallel()

	repos := make([]dashboardRepository, 0, 8)
	for index := range 8 {
		repos = append(repos, dashboardRepository{
			status:   repositoryStatus{repositoryName: fmt.Sprintf("repo-%d", index+1), branch: "feature/wide", baseBranch: "main", baseKnown: true, worktree: "clean"},
			diffStat: "file.go | 1 +\n",
		})
	}
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{
		{name: "feature/wide", lastActivity: time.Now(), drift: "current", repos: repos},
		{name: "feature/other", drift: "current"},
	}})
	model = updated.(dashboardModel)
	if model.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("dashboard view does not enable mouse wheel events")
	}
	detailX := model.listWidth() + 3

	model = wheel(t, model, detailX, tea.MouseWheelDown)
	if model.selected != 0 || model.detailOffset != dashboardWheelDetailLines {
		t.Errorf("wheel down over detail: selected = %d, detailOffset = %d; want 0, %d", model.selected, model.detailOffset, dashboardWheelDetailLines)
	}
	model = wheel(t, model, detailX, tea.MouseWheelUp)
	if model.detailOffset != 0 {
		t.Errorf("wheel up over detail: detailOffset = %d, want 0", model.detailOffset)
	}

	maxOffset := model.maxDetailOffset()
	if maxOffset == 0 {
		t.Fatal("detail fits the pane; the test needs scrollable details")
	}
	for range 50 {
		model = wheel(t, model, detailX, tea.MouseWheelDown)
	}
	if model.detailOffset != maxOffset {
		t.Errorf("detailOffset after scrolling past the end = %d, want %d", model.detailOffset, maxOffset)
	}
	if rendered := model.render(); !strings.Contains(rendered, "repo-8") {
		t.Errorf("render at the bottom of the detail does not reveal repo-8:\n%s", rendered)
	}
	model = pressKeys(t, model, "pgdown")
	if model.detailOffset != maxOffset {
		t.Errorf("pgdown at the bottom moved detailOffset to %d, want %d", model.detailOffset, maxOffset)
	}

	model = wheel(t, model, 0, tea.MouseWheelDown)
	if model.selected != 1 || model.detailOffset != 0 {
		t.Errorf("wheel down over list: selected = %d, detailOffset = %d; want 1, 0", model.selected, model.detailOffset)
	}
	model = wheel(t, model, 0, tea.MouseWheelDown)
	if model.selected != 1 {
		t.Errorf("wheel down past the last session: selected = %d, want 1", model.selected)
	}
	model = wheel(t, model, 0, tea.MouseWheelUp)
	if model.selected != 0 {
		t.Errorf("wheel up over list: selected = %d, want 0", model.selected)
	}

	model.mode = dashboardDiff
	model.diff.SetWidth(80)
	model.diff.SetHeight(5)
	model.diff.SetContent(strings.Repeat("line\n", 50))
	model = wheel(t, model, 0, tea.MouseWheelDown)
	if model.diff.YOffset() == 0 || model.selected != 0 {
		t.Errorf("wheel in diff mode: diff offset = %d, selected = %d; want scrolled diff, 0", model.diff.YOffset(), model.selected)
	}
}

func wheel(t *testing.T, model dashboardModel, x int, button tea.MouseButton) dashboardModel {
	t.Helper()
	updated, _ := model.Update(tea.MouseWheelMsg{X: x, Y: 2, Button: button})
	return updated.(dashboardModel)
}

func TestDashboardModelRunsActionsInBackground(t *testing.T) {
	t.Parallel()

	executable := filepath.Join(t.TempDir(), "autofeat")
	writeMainFile(t, filepath.Dir(executable), "autofeat", `#!/bin/sh
echo "$1 $2 prompt=$GIT_TERMINAL_PROMPT"
if [ "$1" = teardown ]; then
	echo "autofeat: cannot teardown dirty worktree" >&2
	exit 1
fi
`)
	if err := os.Chmod(executable, 0o755); err != nil {
		t.Fatal(err)
	}
	model := newDashboardModel(executable, defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{
		{name: "feature/a", lastActivity: time.Now(), drift: "current"},
		{name: "feature/b", drift: "current"},
	}})
	model = updated.(dashboardModel)

	model = pressKeys(t, model, "l")
	if model.mode != dashboardList || model.message != "no action output yet" {
		t.Errorf("l before any action: mode = %v, message = %q", model.mode, model.message)
	}

	updated, syncCmd := model.Update(keyPress("s"))
	model = updated.(dashboardModel)
	if syncCmd == nil || model.running["feature/a"] != "sync" || !strings.Contains(model.render(), "sync…") {
		t.Fatalf("s did not start a background sync: running = %v\n%s", model.running, model.render())
	}
	for _, key := range []string{"s", "t"} {
		updated, cmd := model.Update(keyPress(key))
		model = updated.(dashboardModel)
		if cmd != nil || model.mode != dashboardList || model.message != "feature/a is busy: sync is still running" {
			t.Errorf("%s during sync: cmd = %v, mode = %v, message = %q", key, cmd != nil, model.mode, model.message)
		}
	}
	if _, cmd := model.Update(keyPress("q")); cmd != nil {
		t.Error("q quit while an action was running")
	}
	if _, cmd := model.Update(keyPress("j")); cmd != nil {
		t.Error("navigating while an action runs returned a command")
	}

	updated, _ = model.Update(syncCmd())
	model = updated.(dashboardModel)
	if len(model.running) != 0 || model.message != "sync feature/a finished" || model.lastOutput != "sync feature/a prompt=0\n" {
		t.Errorf("after sync: running = %v, message = %q, output = %q", model.running, model.message, model.lastOutput)
	}

	model = pressKeys(t, model, "t")
	updated, teardownCmd := model.Update(keyPress("y"))
	model = updated.(dashboardModel)
	if teardownCmd == nil || model.running["feature/a"] != "teardown" {
		t.Fatalf("y after t did not start a background teardown: running = %v", model.running)
	}
	updated, _ = model.Update(teardownCmd())
	model = updated.(dashboardModel)
	if want := "teardown feature/a failed: cannot teardown dirty worktree · l output"; model.message != want {
		t.Errorf("teardown failure message = %q, want %q", model.message, want)
	}

	model = pressKeys(t, model, "l")
	if rendered := model.render(); model.mode != dashboardOutput ||
		!strings.Contains(rendered, "output of teardown feature/a") || !strings.Contains(rendered, "cannot teardown dirty worktree") {
		t.Errorf("l did not show the teardown output:\n%s", rendered)
	}
	model = pressKeys(t, model, "esc")
	if model.mode != dashboardList {
		t.Errorf("esc in output view: mode = %v, want dashboardList", model.mode)
	}
}

func TestDashboardModelDetailShowsSessionOpenInVSCode(t *testing.T) {
	t.Parallel()

	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{
		{name: "feature/open", lastActivity: time.Now(), drift: "current", editorOpen: true},
		{name: "feature/closed", drift: "current"},
	}})
	model = updated.(dashboardModel)
	if detail := model.renderDetail(80, 20); !strings.Contains(detail, "open in VS Code") {
		t.Errorf("detail of an open session lacks the VS Code line:\n%s", detail)
	}
	model = pressKeys(t, model, "j")
	if detail := model.renderDetail(80, 20); strings.Contains(detail, "open in VS Code") {
		t.Errorf("detail of a closed session shows the VS Code line:\n%s", detail)
	}
}

func TestDashboardModelDetailLinksArtifacts(t *testing.T) {
	t.Parallel()

	buildURL := "https://dev.azure.com/org/Project/_build/results?buildId=123"
	armID := "/subscriptions/s/resourceGroups/rg"
	session := dashboardSession{name: "feature/links", lastActivity: time.Now(), drift: "current", session: state.Session{Artifacts: []artifact.Artifact{
		{Type: artifact.ADOBuild, Value: buildURL, ID: "123"},
		{Type: artifact.AzureResource, Value: armID, ID: armID},
		{Type: artifact.ADOWorkItem, Value: "AB#4567", ID: "4567"},
	}}}
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: []dashboardSession{session}})
	model = updated.(dashboardModel)

	for _, width := range []int{120, 24} {
		detail := model.renderDetail(width, 40)
		for _, link := range []string{buildURL, "https://portal.azure.com/#resource" + armID} {
			if !strings.Contains(detail, "\x1b]8;;"+link) {
				t.Errorf("width %d: detail lacks a hyperlink to %s:\n%q", width, link, detail)
			}
		}
		if got := strings.Count(detail, "\x1b]8;;\x1b\\") + strings.Count(detail, "\x1b]8;;\a"); got != 2 {
			t.Errorf("width %d: detail closes %d hyperlinks, want 2:\n%q", width, got, detail)
		}
		for _, line := range strings.Split(detail, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: detail line is %d columns wide: %q", width, got, line)
			}
		}
	}
}

func writeMainFile(t *testing.T, directory, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pressKeys(t *testing.T, model dashboardModel, keys ...string) dashboardModel {
	t.Helper()
	for _, key := range keys {
		updated, _ := model.Update(keyPress(key))
		model = updated.(dashboardModel)
	}
	return model
}

func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
}

func TestDashboardModelSortCyclesOrdersAndKeepsSelection(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	behind := func(commits int) []dashboardRepository {
		return []dashboardRepository{{status: repositoryStatus{baseKnown: true, baseBehind: commits}}}
	}
	sessions := []dashboardSession{
		{name: "feature/b", lastActivity: now, session: state.Session{CreatedAt: now.Add(-3 * time.Hour)}, drift: "current", repos: behind(0)},
		{name: "feature/c", lastActivity: now.Add(-time.Hour), session: state.Session{CreatedAt: now.Add(-time.Hour)}, drift: "5 behind", repos: behind(5), stale: "merged"},
		{name: "feature/a", lastActivity: now.Add(-2 * time.Hour), session: state.Session{CreatedAt: now.Add(-2 * time.Hour)}, drift: "unknown"},
		{name: "feature/d", lastActivity: now.Add(-3 * time.Hour), session: state.Session{CreatedAt: now}, drift: "2 behind", repos: behind(2)},
	}
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(dashboardLoadedMsg{sessions: slices.Clone(sessions)})
	model = pressKeys(t, updated.(dashboardModel), "j")

	names := func() []string {
		result := []string{}
		for _, index := range model.visible {
			result = append(result, model.sessions[index].name)
		}
		return result
	}
	for _, want := range []struct {
		order string
		names []string
	}{
		{"last activity", []string{"feature/b", "feature/c", "feature/a", "feature/d"}},
		{"name", []string{"feature/a", "feature/b", "feature/c", "feature/d"}},
		{"created", []string{"feature/d", "feature/c", "feature/a", "feature/b"}},
		{"stale first", []string{"feature/c", "feature/b", "feature/a", "feature/d"}},
		{"most behind", []string{"feature/c", "feature/d", "feature/b", "feature/a"}},
		{"last activity", []string{"feature/b", "feature/c", "feature/a", "feature/d"}},
	} {
		if got := names(); !slices.Equal(got, want.names) || !strings.Contains(model.render(), "sorted by "+want.order) {
			t.Errorf("sorted by %s = %v, want %v; header:\n%s", want.order, got, want.names, strings.SplitN(model.render(), "\n", 2)[0])
		}
		if got := model.currentName(); got != "feature/c" {
			t.Errorf("selection after sorting by %s = %q, want feature/c", want.order, got)
		}
		model = pressKeys(t, model, ",")
	}

	updated, _ = model.Update(dashboardLoadedMsg{sessions: slices.Clone(sessions)})
	model = updated.(dashboardModel)
	if got := names(); !slices.Equal(got, []string{"feature/a", "feature/b", "feature/c", "feature/d"}) {
		t.Errorf("reload while sorted by name = %v, want name order", got)
	}
}

func TestDashboardInitViewDiffActionsAndStates(t *testing.T) {
	requireMainGit(t)
	t.Setenv("HOME", t.TempDir())

	repoPath := createMainRepository(t)
	runMainGit(t, repoPath, "branch", "-M", "main")
	runMainGit(t, repoPath, "checkout", "-qb", "feature/dash")
	writeMainFile(t, repoPath, "untracked.txt", "new\n")
	if err := state.SaveSession("feature/dash", state.Session{Repos: []state.Repository{
		{Name: "api", WorktreePath: repoPath, BaseBranch: "main"},
	}}); err != nil {
		t.Fatal(err)
	}

	model := newDashboardModel("autofeat", defaultStaleAfter)
	// Loading state before first message.
	if rendered := model.render(); !strings.Contains(rendered, "Inspecting sessions…") {
		t.Errorf("initial loading render = %q, want Inspecting sessions…", rendered)
	}
	view := model.View()
	if !view.AltScreen || view.WindowTitle != "autofeat dash" {
		t.Errorf("View() = (AltScreen %t, WindowTitle %q), want true and autofeat dash", view.AltScreen, view.WindowTitle)
	}

	initCmd := model.Init()
	if initCmd == nil {
		t.Fatal("Init() = nil, want loadCmd")
	}
	loadedMsg := initCmd()
	updated, _ := model.Update(loadedMsg)
	model = updated.(dashboardModel)
	if len(model.sessions) != 1 || !strings.Contains(model.render(), "1 session · 0 stale") {
		t.Fatalf("after Init() load: sessions = %d, render:\n%s", len(model.sessions), model.render())
	}

	// Execute diffCmd for the loaded repository (has valid baseRef) and for a repository without baseRef.
	updated, diffCmd := model.Update(keyPress("d"))
	model = updated.(dashboardModel)
	if model.mode != dashboardDiff || diffCmd == nil {
		t.Fatal("d did not enter diff mode")
	}
	if !strings.Contains(model.render(), "Loading diff…") {
		t.Errorf("unloaded diff render missing Loading diff…:\n%s", model.render())
	}
	diffMsg := diffCmd().(dashboardDiffMsg)
	if diffMsg.err != nil || !strings.Contains(diffMsg.content, "untracked.txt") {
		t.Errorf("diffCmd() = %+v, want untracked.txt diff", diffMsg)
	}

	// Ignored stale diff message for another feature, then empty diff, then error diff.
	updated, _ = model.Update(dashboardDiffMsg{feature: "other", repository: 0, content: "ignored"})
	model = updated.(dashboardModel)
	if model.diffLoaded {
		t.Error("mismatched dashboardDiffMsg marked diff as loaded")
	}
	updated, _ = model.Update(dashboardDiffMsg{feature: "feature/dash", repository: 0, content: "   "})
	model = updated.(dashboardModel)
	if !strings.Contains(model.render(), "No changes against the base branch.") {
		t.Errorf("empty diff render = %q, want no-changes message", model.render())
	}
	updated, _ = model.Update(dashboardDiffMsg{feature: "feature/dash", repository: 0, err: errors.New("boom")})
	model = updated.(dashboardModel)
	if !strings.Contains(model.render(), "error: boom") {
		t.Errorf("error diff render = %q, want error: boom", model.render())
	}

	// Multi-repo tab/shift+tab in diff mode, plus missing baseRef error in diffCmd.
	model.sessions[0].repos = append(model.sessions[0].repos, dashboardRepository{
		status: repositoryStatus{repositoryName: "web", baseBranch: "main", detail: "no base"},
	})
	updated, tabCmd := model.Update(keyPress("tab"))
	model = updated.(dashboardModel)
	if model.diffRepo != 1 || tabCmd == nil {
		t.Fatalf("tab in diff mode: diffRepo = %d, cmd = %v; want 1 and non-nil cmd", model.diffRepo, tabCmd)
	}
	noBaseMsg := tabCmd().(dashboardDiffMsg)
	if noBaseMsg.err == nil || !strings.Contains(noBaseMsg.err.Error(), "base branch is unavailable") {
		t.Errorf("diffCmd(no baseRef) = %+v, want base branch unavailable error", noBaseMsg)
	}
	updated, _ = model.Update(keyPress("shift+tab"))
	model = updated.(dashboardModel)
	if model.diffRepo != 0 {
		t.Errorf("shift+tab in diff mode: diffRepo = %d, want 0", model.diffRepo)
	}

	// Reload while in diff mode: stays in diff mode when repo exists, exits to list when repos disappear.
	updated, reloadDiffCmd := model.Update(dashboardLoadedMsg{sessions: model.sessions})
	model = updated.(dashboardModel)
	if model.mode != dashboardDiff || reloadDiffCmd == nil {
		t.Errorf("reload in diff mode with valid repo: mode = %v, cmd = %v", model.mode, reloadDiffCmd)
	}
	updated, _ = model.Update(dashboardLoadedMsg{sessions: []dashboardSession{{name: "feature/dash"}}})
	model = updated.(dashboardModel)
	if model.mode != dashboardList {
		t.Errorf("reload in diff mode without repos: mode = %v, want dashboardList", model.mode)
	}

	// Actions (s, o, r), action messages, navigation keys (k, pgup, q, ctrl+c), and error/filter states.
	updated, syncCmd := model.Update(keyPress("s"))
	model = updated.(dashboardModel)
	if syncCmd == nil {
		t.Error("s did not return sync actionCmd")
	}
	updated, _ = model.Update(dashboardActionMsg{action: "sync", feature: "feature/dash"})
	model = updated.(dashboardModel)
	if model.message != "sync feature/dash finished" {
		t.Errorf("action success message = %q", model.message)
	}
	updated, openCmd := model.Update(keyPress("o"))
	model = updated.(dashboardModel)
	if openCmd == nil {
		t.Error("o did not return open actionCmd")
	}
	updated, refreshCmd := model.Update(keyPress("r"))
	model = updated.(dashboardModel)
	if !model.loading || refreshCmd == nil {
		t.Error("r did not trigger refresh")
	}
	updated, _ = model.Update(dashboardActionMsg{action: "open", feature: "feature/dash", err: errors.New("conflict")})
	model = updated.(dashboardModel)
	if !strings.Contains(model.message, "open feature/dash failed: conflict") {
		t.Errorf("action failure message = %q", model.message)
	}

	model.detailOffset = 10
	model = pressKeys(t, model, "pgup", "k")
	if model.detailOffset >= 10 {
		t.Errorf("pgup did not decrease detailOffset: %d", model.detailOffset)
	}

	// Filter with no matches.
	model = pressKeys(t, model, "/", "z", "z", "z", "enter")
	if !strings.Contains(model.render(), "No sessions match the filter.") {
		t.Errorf("empty filter render = %q, want no-match message", model.render())
	}

	// Load error render.
	updated, _ = model.Update(dashboardLoadedMsg{err: errors.New("corrupt state")})
	model = updated.(dashboardModel)
	if !strings.Contains(model.render(), "error: corrupt state") {
		t.Errorf("error render = %q, want corrupt state", model.render())
	}

	for _, key := range []string{"q", "ctrl+c"} {
		if _, quitCmd := model.Update(keyPress(key)); quitCmd == nil {
			t.Errorf("key %q did not return tea.Quit", key)
		}
	}
}

func TestDashboardBadgesFormatAgeAndExec(t *testing.T) {
	t.Parallel()

	if got := formatAge(30 * time.Second); got != "0m" {
		t.Errorf("formatAge(30s) = %q, want 0m", got)
	}
	if got := formatAge(45 * time.Minute); got != "45m" {
		t.Errorf("formatAge(45m) = %q, want 45m", got)
	}
	if got := formatAge(5 * time.Hour); got != "5h" {
		t.Errorf("formatAge(5h) = %q, want 5h", got)
	}
	if got := formatAge(3 * 24 * time.Hour); got != "3d" {
		t.Errorf("formatAge(3d) = %q, want 3d", got)
	}

	badges := sessionBadges(dashboardSession{
		drift: "unknown",
		stale: "idle 15d",
		repos: []dashboardRepository{{
			status:    repositoryStatus{rebasing: true, missing: true, dirty: true},
			untracked: []string{"a.txt"},
		}},
	})
	for _, want := range []string{"rebasing", "!", "●", "?", "stale"} {
		if !strings.Contains(badges, want) {
			t.Errorf("sessionBadges() = %q, missing %q", badges, want)
		}
	}
	if strings.Contains(badges, "open") {
		t.Errorf("sessionBadges() = %q, want no open badge for a session closed in VS Code", badges)
	}
	if badges := sessionBadges(dashboardSession{drift: "current", editorOpen: true}); !strings.Contains(badges, "open") {
		t.Errorf("sessionBadges(editorOpen) = %q, missing %q", badges, "open")
	}

	file, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if isTerminal(file) {
		t.Error("isTerminal(regular file) = true, want false")
	}
	if err := runDashboard(defaultStaleAfter); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		// Under `go test`, os.Stdin is not a terminal.
		if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
			t.Errorf("runDashboard() without terminal error = %v, want interactive terminal error", err)
		}
	}
}
