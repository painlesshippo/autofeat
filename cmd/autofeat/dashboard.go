package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/vscode"
)

const defaultStaleAfter = 14 * 24 * time.Hour

type dashboardRepository struct {
	status       repositoryStatus
	worktreePath string
	baseRef      string
	diffStat     string
	untracked    []string
	merged       bool
}

type dashboardSession struct {
	name         string
	session      state.Session
	lastActivity time.Time
	drift        string
	repos        []dashboardRepository
	stale        string
	editorOpen   bool
}

func loadDashboardSessions(staleAfter time.Duration, now time.Time) ([]dashboardSession, error) {
	currentState, err := state.Load()
	if err != nil {
		return nil, err
	}

	sessions := make([]dashboardSession, 0, len(currentState.Sessions))
	for name, session := range currentState.Sessions {
		sessions = append(sessions, dashboardSession{name: name, session: session})
	}

	var group sync.WaitGroup
	for index := range sessions {
		group.Go(func() {
			inspectDashboardSession(&sessions[index], currentState.DefaultBaseBranch, staleAfter, now)
		})
	}
	group.Wait()
	return sessions, nil
}

func inspectDashboardSession(session *dashboardSession, defaultBaseBranch string, staleAfter time.Duration, now time.Time) {
	session.drift, session.lastActivity = inspectSessionListing(session.session, defaultBaseBranch)
	session.editorOpen = vscode.WorkspaceOpen(session.session.WorkspaceFile)

	repositories := append([]state.Repository(nil), session.session.Repos...)
	sort.Slice(repositories, func(i, j int) bool {
		if repositories[i].Name == repositories[j].Name {
			return repositories[i].WorktreePath < repositories[j].WorktreePath
		}
		return repositories[i].Name < repositories[j].Name
	})
	for _, repository := range repositories {
		entry := dashboardRepository{
			status:       inspectRepositoryStatus(session.name, repository, defaultBaseBranch),
			worktreePath: repository.WorktreePath,
		}
		if entry.status.baseKnown {
			inspectDashboardChanges(&entry, repository.WorktreePath)
		}
		session.repos = append(session.repos, entry)
	}
	session.stale = classifyStale(session.repos, session.lastActivity, now, staleAfter)
}

// inspectDashboardChanges fills in the change summary of a repository whose
// base branch already resolved during status inspection. Failures are recorded
// in the status detail rather than hiding the repository.
func inspectDashboardChanges(entry *dashboardRepository, worktreePath string) {
	baseRef, err := gitcmd.ResolveBaseRef(worktreePath, entry.status.baseBranch)
	if err == nil {
		entry.baseRef = baseRef
		entry.diffStat, err = gitcmd.DiffStat(worktreePath, baseRef)
	}
	if err == nil {
		entry.untracked, err = gitcmd.UntrackedFiles(worktreePath)
	}
	if err == nil {
		entry.merged, err = gitcmd.ChangesMerged(worktreePath, baseRef)
	}
	if err != nil && entry.status.detail == "" {
		entry.status.detail = err.Error()
	}
}

// classifyStale returns why a session looks safe to tear down, or an empty
// string when it does not. A session is merged when its repositories hold no
// local work and at least one has commits whose changes the base branch
// already contains; it is missing when every worktree is gone; otherwise it is
// idle when its last activity is at least staleAfter old. A non-positive
// staleAfter disables the idle rule.
func classifyStale(repos []dashboardRepository, lastActivity, now time.Time, staleAfter time.Duration) string {
	if len(repos) != 0 {
		allMissing := true
		anyMerged := false
		allSettled := true
		for _, repo := range repos {
			status := repo.status
			if !status.missing {
				allMissing = false
			}
			anyMerged = anyMerged || repo.merged
			unsettled := status.missing || status.inspectionError || status.rebasing || status.dirty || !status.baseKnown
			if unsettled || (status.baseAhead > 0 && !repo.merged) {
				allSettled = false
			}
		}
		if allMissing {
			return "missing"
		}
		if allSettled && anyMerged {
			return "merged"
		}
	}

	if staleAfter > 0 && !lastActivity.IsZero() && now.Sub(lastActivity) >= staleAfter {
		return "idle " + formatAge(now.Sub(lastActivity))
	}
	return ""
}

func formatAge(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "0m"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}

type dashboardSortOrder int

const (
	sortByActivity dashboardSortOrder = iota
	sortByName
	sortByCreated
	sortByStale
	sortByBehind
	dashboardSortOrders
)

func (order dashboardSortOrder) String() string {
	return [...]string{"last activity", "name", "created", "stale first", "most behind"}[order]
}

// sortDashboardSessions orders sessions in place. Ties keep the default
// order: most recent activity first, then name.
func sortDashboardSessions(sessions []dashboardSession, order dashboardSortOrder) {
	slices.SortStableFunc(sessions, func(a, b dashboardSession) int {
		var result int
		switch order {
		case sortByName:
			result = strings.Compare(a.name, b.name)
		case sortByCreated:
			result = b.session.CreatedAt.Compare(a.session.CreatedAt)
		case sortByStale:
			result = cmp.Compare(boolRank(a.stale == ""), boolRank(b.stale == ""))
		case sortByBehind:
			result = cmp.Compare(sessionBehind(b), sessionBehind(a))
		}
		if result != 0 {
			return result
		}
		if result = b.lastActivity.Compare(a.lastActivity); result != 0 {
			return result
		}
		return strings.Compare(a.name, b.name)
	})
}

func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

// sessionBehind returns how many commits the session's repositories are
// behind their cached bases, or -1 when that is unknown so such sessions sort
// last.
func sessionBehind(session dashboardSession) int {
	if session.drift == "unknown" {
		return -1
	}
	behind := 0
	for _, repo := range session.repos {
		behind += repo.status.baseBehind
	}
	return behind
}

type dashboardMode int

const (
	dashboardList dashboardMode = iota
	dashboardDiff
	dashboardConfirmTeardown
)

type dashboardLoadedMsg struct {
	sessions []dashboardSession
	err      error
}

type dashboardDiffMsg struct {
	feature    string
	repository int
	content    string
	err        error
}

type dashboardActionMsg struct {
	action  string
	feature string
	err     error
}

type dashboardModel struct {
	staleAfter time.Duration
	now        func() time.Time
	executable string

	sessions  []dashboardSession
	sortOrder dashboardSortOrder
	visible   []int
	selected  int
	loading   bool
	err       error
	message   string

	mode          dashboardMode
	confirmTarget string
	detailOffset  int
	filtering     bool
	filter        textinput.Model
	diff          viewport.Model
	diffRepo      int
	diffLoaded    bool

	width  int
	height int
}

func newDashboardModel(executable string, staleAfter time.Duration) dashboardModel {
	filter := textinput.New()
	filter.Prompt = "/"
	filter.Placeholder = "filter features"
	return dashboardModel{
		staleAfter: staleAfter,
		now:        time.Now,
		executable: executable,
		loading:    true,
		filter:     filter,
		diff:       viewport.New(),
		width:      80,
		height:     24,
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return m.loadCmd()
}

func (m dashboardModel) loadCmd() tea.Cmd {
	staleAfter, now := m.staleAfter, m.now()
	return func() tea.Msg {
		sessions, err := loadDashboardSessions(staleAfter, now)
		return dashboardLoadedMsg{sessions: sessions, err: err}
	}
}

func (m dashboardModel) diffCmd() tea.Cmd {
	session, ok := m.current()
	if !ok || len(session.repos) == 0 {
		return nil
	}
	index := m.diffRepo
	repo := session.repos[index]
	feature := session.name
	return func() tea.Msg {
		if repo.baseRef == "" {
			return dashboardDiffMsg{feature: feature, repository: index, err: errors.New("base branch is unavailable: " + statusDetail(repo.status))}
		}
		content, err := gitcmd.Diff(repo.worktreePath, repo.baseRef, true)
		return dashboardDiffMsg{feature: feature, repository: index, content: content, err: err}
	}
}

// actionCmd hands the terminal to an autofeat subcommand. Commands whose output
// matters wait for Enter before the dashboard redraws over it.
func (m dashboardModel) actionCmd(action, feature string, pause bool) tea.Cmd {
	command := &dashboardExec{command: exec.Command(m.executable, action, feature), pause: pause}
	return tea.Exec(command, func(err error) tea.Msg {
		return dashboardActionMsg{action: action, feature: feature, err: err}
	})
}

func (m dashboardModel) current() (dashboardSession, bool) {
	if m.selected < 0 || m.selected >= len(m.visible) {
		return dashboardSession{}, false
	}
	return m.sessions[m.visible[m.selected]], true
}

func (m dashboardModel) hasSession(name string) bool {
	for _, session := range m.sessions {
		if session.name == name {
			return true
		}
	}
	return false
}

func (m dashboardModel) currentName() string {
	session, _ := m.current()
	return session.name
}

// applyFilter rebuilds the visible sessions and keeps selectedName selected
// when it is still visible. selectedName must be read before m.sessions or
// the filter changes, because m.visible indexes the previous sessions.
func (m *dashboardModel) applyFilter(selectedName string) {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = m.visible[:0]
	for index, session := range m.sessions {
		if query == "" || strings.Contains(strings.ToLower(session.name), query) {
			m.visible = append(m.visible, index)
		}
	}
	m.selected = 0
	for position, index := range m.visible {
		if m.sessions[index].name == selectedName {
			m.selected = position
		}
	}
	if m.currentName() != selectedName {
		m.detailOffset = 0
	}
}

func (m *dashboardModel) resizeDiff() {
	m.diff.SetWidth(m.width)
	m.diff.SetHeight(max(1, m.height-2))
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeDiff()
		return m, nil
	case dashboardLoadedMsg:
		m.loading = false
		m.err = msg.err
		selectedName := m.currentName()
		m.sessions = msg.sessions
		sortDashboardSessions(m.sessions, m.sortOrder)
		m.applyFilter(selectedName)
		if m.mode == dashboardConfirmTeardown && !m.hasSession(m.confirmTarget) {
			m.mode = dashboardList
			m.message = "teardown cancelled: " + m.confirmTarget + " no longer exists"
		}
		if m.mode == dashboardDiff {
			if session, ok := m.current(); !ok || m.diffRepo >= len(session.repos) {
				m.mode = dashboardList
			} else {
				m.diffLoaded = false
				return m, m.diffCmd()
			}
		}
		return m, nil
	case dashboardDiffMsg:
		session, ok := m.current()
		if !ok || session.name != msg.feature || m.diffRepo != msg.repository {
			return m, nil
		}
		m.diffLoaded = true
		content := msg.content
		if msg.err != nil {
			content = "error: " + msg.err.Error()
		} else if strings.TrimSpace(content) == "" {
			content = "No changes against the base branch."
		}
		m.diff.SetContent(content)
		m.diff.GotoTop()
		return m, nil
	case dashboardActionMsg:
		if msg.err != nil {
			m.message = fmt.Sprintf("%s %s failed: %v", msg.action, msg.feature, msg.err)
		} else {
			m.message = fmt.Sprintf("%s %s finished", msg.action, msg.feature)
		}
		m.loading = true
		return m, m.loadCmd()
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}

	if m.filtering {
		selectedName := m.currentName()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter(selectedName)
		return m, cmd
	}

	if m.mode == dashboardDiff {
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m dashboardModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	if m.filtering {
		switch key {
		case "enter", "esc":
			m.filtering = false
			m.filter.Blur()
			if key == "esc" {
				selectedName := m.currentName()
				m.filter.SetValue("")
				m.applyFilter(selectedName)
			}
			return m, nil
		}
		selectedName := m.currentName()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter(selectedName)
		return m, cmd
	}

	switch m.mode {
	case dashboardConfirmTeardown:
		m.mode = dashboardList
		if (key != "y" && key != "Y") || !m.hasSession(m.confirmTarget) {
			m.message = "teardown cancelled"
			return m, nil
		}
		return m, m.actionCmd("teardown", m.confirmTarget, true)
	case dashboardDiff:
		session, ok := m.current()
		switch key {
		case "q", "esc":
			m.mode = dashboardList
			return m, nil
		case "tab", "shift+tab":
			if ok && len(session.repos) > 1 {
				step := 1
				if key == "shift+tab" {
					step = len(session.repos) - 1
				}
				m.diffRepo = (m.diffRepo + step) % len(session.repos)
				m.diffLoaded = false
				return m, m.diffCmd()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}

	session, ok := m.current()
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.selected = max(0, m.selected-1)
	case "down", "j":
		m.selected = min(max(0, len(m.visible)-1), m.selected+1)
	case "home", "g":
		m.selected = 0
	case "end", "G":
		m.selected = max(0, len(m.visible)-1)
	case "pgdown", "ctrl+d":
		m.detailOffset += max(1, m.height/2)
	case "pgup", "ctrl+u":
		m.detailOffset = max(0, m.detailOffset-max(1, m.height/2))
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case ",":
		selectedName := m.currentName()
		m.sortOrder = (m.sortOrder + 1) % dashboardSortOrders
		sortDashboardSessions(m.sessions, m.sortOrder)
		m.applyFilter(selectedName)
	case "r":
		m.loading = true
		m.message = ""
		return m, m.loadCmd()
	case "enter", "d":
		if ok && len(session.repos) != 0 {
			m.mode = dashboardDiff
			m.diffRepo = 0
			m.diffLoaded = false
			m.resizeDiff()
			return m, m.diffCmd()
		}
	case "s":
		if ok {
			return m, m.actionCmd("sync", session.name, true)
		}
	case "o":
		if ok {
			return m, m.actionCmd("open", session.name, false)
		}
	case "t":
		if ok {
			m.mode = dashboardConfirmTeardown
			m.confirmTarget = session.name
		}
	}
	if m.currentName() != session.name {
		m.detailOffset = 0
	}
	return m, nil
}

var (
	dashboardTitleStyle    = lipgloss.NewStyle().Bold(true)
	dashboardFaintStyle    = lipgloss.NewStyle().Faint(true)
	dashboardSelectedStyle = lipgloss.NewStyle().Reverse(true)
	dashboardGreenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	dashboardYellowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	dashboardRedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	dashboardMagentaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	dashboardCyanStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	dashboardLinkStyle     = lipgloss.NewStyle().Underline(true)
)

func (m dashboardModel) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "autofeat dash"
	return view
}

func (m dashboardModel) render() string {
	if m.mode == dashboardDiff {
		return m.renderDiff()
	}

	stale := 0
	for _, session := range m.sessions {
		if session.stale != "" {
			stale++
		}
	}
	noun := "sessions"
	if len(m.sessions) == 1 {
		noun = "session"
	}
	header := dashboardTitleStyle.Render(fmt.Sprintf("autofeat dash · %d %s · %d stale", len(m.sessions), noun, stale)) +
		dashboardFaintStyle.Render(" · sorted by "+m.sortOrder.String())
	if m.loading {
		header += dashboardFaintStyle.Render(" · refreshing…")
	}

	header = truncate(header, m.width)
	footer := lipgloss.NewStyle().Width(m.width).Render(m.renderFooter())
	bodyHeight := max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer))

	var body string
	switch {
	case m.err != nil:
		body = dashboardRedStyle.Render("error: " + m.err.Error())
	case m.loading && len(m.sessions) == 0:
		body = dashboardFaintStyle.Render("Inspecting sessions…")
	case len(m.sessions) == 0:
		body = "No sessions. Create one with `autofeat new FEATURE`."
	case len(m.visible) == 0:
		body = "No sessions match the filter."
	default:
		listWidth := min(max(30, m.width*2/5), 60, max(1, (m.width-3)/2))
		list := m.renderList(listWidth, bodyHeight)
		detail := m.renderDetail(max(1, m.width-listWidth-3), bodyHeight)
		separator := dashboardFaintStyle.Render(strings.Repeat(" │ \n", bodyHeight))
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, strings.TrimSuffix(separator, "\n"), detail)
	}
	body = lipgloss.NewStyle().Height(bodyHeight).MaxHeight(bodyHeight).Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m dashboardModel) renderFooter() string {
	if m.filtering {
		return m.filter.View()
	}
	if m.mode == dashboardConfirmTeardown {
		return dashboardYellowStyle.Render(fmt.Sprintf("Tear down %s? Its worktrees and local branches are removed. [y/N]", m.confirmTarget))
	}
	help := "↑/↓ select · pgup/pgdn details · enter diff · s sync · o open · t teardown · / filter · , sort · r refresh · q quit"
	if m.filter.Value() != "" {
		help = "filter: " + m.filter.Value() + " · " + help
	}
	if m.message != "" {
		return m.message + "\n" + dashboardFaintStyle.Render(help)
	}
	return dashboardFaintStyle.Render(help)
}

func (m dashboardModel) renderList(width, height int) string {
	offset := max(0, m.selected-height+1)
	now := m.now()
	lines := make([]string, 0, height)
	for position := offset; position < len(m.visible) && len(lines) < height; position++ {
		session := m.sessions[m.visible[position]]
		badges := sessionBadges(session)
		age := formatAge(now.Sub(session.lastActivity))
		name := truncate(session.name, max(4, width-lipgloss.Width(badges)-len(age)-3))
		gap := max(1, width-lipgloss.Width(name)-len(age)-lipgloss.Width(badges)-2)
		line := name + strings.Repeat(" ", gap) + badges + " " + dashboardFaintStyle.Render(age)
		if position == m.selected {
			line = dashboardSelectedStyle.Render(name) + strings.Repeat(" ", gap) + badges + " " + age
		}
		lines = append(lines, line)
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func sessionBadges(session dashboardSession) string {
	badges := []string{}
	if session.editorOpen {
		badges = append(badges, dashboardCyanStyle.Render("open"))
	}
	dirty, rebasing, problem := false, false, false
	for _, repo := range session.repos {
		dirty = dirty || repo.status.dirty || len(repo.untracked) != 0
		rebasing = rebasing || repo.status.rebasing
		problem = problem || repo.status.inspectionError || repo.status.missing || repo.status.detached || repo.status.wrongBranch
	}
	if rebasing {
		badges = append(badges, dashboardRedStyle.Render("rebasing"))
	}
	if problem {
		badges = append(badges, dashboardRedStyle.Render("!"))
	}
	if dirty {
		badges = append(badges, dashboardYellowStyle.Render("●"))
	}
	switch {
	case session.drift == "current":
	case session.drift == "unknown":
		badges = append(badges, dashboardRedStyle.Render("?"))
	default:
		badges = append(badges, dashboardYellowStyle.Render("↓"+strings.TrimSuffix(session.drift, " behind")))
	}
	if session.stale != "" {
		badges = append(badges, dashboardMagentaStyle.Render("stale"))
	}
	return strings.Join(badges, " ")
}

func (m dashboardModel) renderDetail(width, height int) string {
	session, _ := m.current()
	now := m.now()
	lines := []string{
		dashboardTitleStyle.Render(session.name),
		dashboardFaintStyle.Render(fmt.Sprintf("created %s ago · last activity %s ago · %s",
			formatAge(now.Sub(session.session.CreatedAt)), formatAge(now.Sub(session.lastActivity)), session.session.FeatureDir)),
	}
	if session.editorOpen {
		lines = append(lines, dashboardCyanStyle.Render("open in VS Code"))
	}
	if session.stale != "" {
		lines = append(lines, dashboardMagentaStyle.Render("stale: "+session.stale+" · press t to tear down"))
	}
	for _, repo := range session.repos {
		status := repo.status
		state := statusState(status)
		style := dashboardGreenStyle
		switch state {
		case "error", "missing", "detached", "wrong-branch", "rebasing", "remote-diverged":
			style = dashboardRedStyle
		case "behind-base", "dirty", "remote-ahead", "unpushed":
			style = dashboardYellowStyle
		}
		lines = append(lines, "",
			dashboardTitleStyle.Render(status.repositoryName)+" "+style.Render(state),
			fmt.Sprintf("  branch %s · base %s %s · push %s · %s",
				status.branch, status.baseBranch,
				formatStatusRelation(status.baseKnown, status.baseAhead, status.baseBehind, "unknown"),
				formatPushStatus(status), status.worktree),
		)
		if repo.merged {
			lines = append(lines, dashboardMagentaStyle.Render("  changes already in "+status.baseBranch))
		}
		if detail := statusDetail(status); detail != "-" {
			lines = append(lines, dashboardRedStyle.Render("  "+detail))
		}
		for _, statLine := range strings.Split(strings.TrimRight(repo.diffStat, "\n"), "\n") {
			if statLine != "" {
				lines = append(lines, "  "+statLine)
			}
		}
		if count := len(repo.untracked); count != 0 {
			lines = append(lines, dashboardYellowStyle.Render(fmt.Sprintf("  + %d untracked file(s)", count)))
		}
	}
	if artifacts := session.session.Artifacts; len(artifacts) != 0 {
		lines = append(lines, "", dashboardTitleStyle.Render("artifacts"))
		for _, recorded := range artifacts {
			label := artifactLabel(recorded)
			if link, ok := recorded.URL(); ok {
				label = dashboardLinkStyle.Hyperlink(link).Render(label)
			}
			line := fmt.Sprintf("  %s %s", recorded.Type, label)
			if recorded.Repo != "" {
				line += " · " + recorded.Repo
			}
			if recorded.ID != "" {
				line += " · " + recorded.Value
			}
			lines = append(lines, line)
		}
	}
	for index, line := range lines {
		lines[index] = truncate(line, width)
	}
	if len(lines) > height {
		// The last row shows the scroll position, so height-1 detail lines fit.
		visible := max(0, height-1)
		offset := min(m.detailOffset, len(lines)-visible)
		indicator := truncate(dashboardFaintStyle.Render(fmt.Sprintf("lines %d-%d of %d · pgup/pgdn scroll", offset+1, offset+visible, len(lines))), width)
		lines = append(lines[offset:offset+visible:offset+visible], indicator)
	}
	return strings.Join(lines, "\n")
}

func (m dashboardModel) renderDiff() string {
	session, _ := m.current()
	header := session.name
	if m.diffRepo < len(session.repos) {
		repo := session.repos[m.diffRepo]
		header = fmt.Sprintf("%s › %s (%d/%d) vs merge-base with %s",
			session.name, repo.status.repositoryName, m.diffRepo+1, len(session.repos), repo.status.baseBranch)
	}
	content := m.diff.View()
	if !m.diffLoaded {
		content = dashboardFaintStyle.Render("Loading diff…")
	}
	footer := truncate(dashboardFaintStyle.Render(fmt.Sprintf("↑/↓/pgup/pgdn scroll · tab next repository · esc back · %3.0f%%", m.diff.ScrollPercent()*100)), m.width)
	return lipgloss.JoinVertical(lipgloss.Left, dashboardTitleStyle.Render(truncate(header, m.width)), content, footer)
}

func truncate(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(text)
}

// dashboardExec runs a subcommand with the dashboard's terminal and, when
// pause is set, keeps its output on screen until Enter is pressed.
type dashboardExec struct {
	command *exec.Cmd
	pause   bool
	stdin   io.Reader
	stdout  io.Writer
}

func (c *dashboardExec) SetStdin(r io.Reader) { c.stdin = r; c.command.Stdin = r }

func (c *dashboardExec) SetStdout(w io.Writer) { c.stdout = w; c.command.Stdout = w }

func (c *dashboardExec) SetStderr(w io.Writer) { c.command.Stderr = w }

func (c *dashboardExec) Run() error {
	err := c.command.Run()
	if c.pause && c.stdout != nil && c.stdin != nil {
		fmt.Fprint(c.stdout, "\nPress Enter to return to the dashboard…")
		buffer := make([]byte, 1)
		for {
			if n, readErr := c.stdin.Read(buffer); readErr != nil || (n == 1 && (buffer[0] == '\n' || buffer[0] == '\r')) {
				break
			}
		}
	}
	return err
}

func runDashboard(staleAfter time.Duration) error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return errors.New("dash requires an interactive terminal; use `autofeat status` or `autofeat list` instead")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate autofeat executable: %w", err)
	}
	_, err = tea.NewProgram(newDashboardModel(executable, staleAfter)).Run()
	return err
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
