package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
