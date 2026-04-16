package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/asmlog"
	"github.com/nhn/asm/recent"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/worktree"
)

type launcherTab int

const (
	launcherTabRecent launcherTab = iota
	launcherTabDirectories
	launcherTabRepos
)

var launcherTabs = []string{"Recent", "Directories", "Repos"}

type launcherEntry struct {
	label    string
	taskName string
	subtitle string
	path     string
	kind     string
	active   bool
}

// LauncherModel is a standalone launcher for the working panel.
type LauncherModel struct {
	tab          launcherTab
	currentPath  string
	repoPath     string
	entries      []launcherEntry
	cursor       int
	filter       string
	SelectedPath string
	width        int
	height       int
	err          string
	tracker      tracker.Tracker
	taskCache    *tracker.PathCache
}

func NewLauncherModel(initialPath string, t tracker.Tracker, taskCache *tracker.PathCache) LauncherModel {
	clean := filepath.Clean(initialPath)
	if clean == "." || clean == "" {
		if cwd, err := os.Getwd(); err == nil {
			clean = cwd
		}
	}
	return LauncherModel{
		tab:         launcherTabDirectories,
		currentPath: clean,
		tracker:     t,
		taskCache:   taskCache,
	}
}

func (m LauncherModel) Init() tea.Cmd {
	return m.reload()
}

func (m LauncherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case launcherEntriesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.entries = nil
			m.cursor = 0
			return m, nil
		}
		m.err = ""
		m.entries = msg.entries
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.advanceTab(+1)
			return m, m.reload()
		case "shift+tab":
			m.advanceTab(-1)
			return m, m.reload()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "left", "h":
			return m.handleBack()
		case "right", "l":
			return m.handleForward()
		case "enter":
			return m.handleEnter()
		case "backspace":
			if len(m.filter) == 0 {
				return m, nil
			}
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.cursor = 0
			return m, m.reload()
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		default:
			switch msg.Type {
			case tea.KeyRunes:
				m.filter += string(msg.Runes)
				m.cursor = 0
				return m, m.reload()
			case tea.KeySpace:
				m.filter += " "
				m.cursor = 0
				return m, m.reload()
			}
		}
	}
	return m, nil
}

func (m LauncherModel) View() string {
	title := renderDialogTitle("Launch Session", primaryColor)
	tabLine := m.renderTabs()
	contextLine := lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render(m.contextLabel())
	filterLine := lipgloss.NewStyle().Padding(0, 2).Foreground(primaryColor).Render("/ " + m.filter)

	var rows []string
	for i, entry := range m.entries {
		cursor := "  "
		style := normalItemStyle
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
			style = selectedItemStyle
		}
		label := entry.label
		if entry.active {
			label += " " + lipgloss.NewStyle().Foreground(primaryColor).Render("[open]")
		}
		row := "  " + cursor + style.Render(label)
		if entry.taskName != "" {
			taskStyle := taskNameStyle.Bold(true)
			if i == m.cursor {
				taskStyle = lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
			}
			row += "  " + taskStyle.Render(entry.taskName)
		}
		if entry.subtitle != "" {
			pathStyle := lipgloss.NewStyle().Foreground(secondaryColor)
			if i == m.cursor {
				pathStyle = lipgloss.NewStyle().Foreground(whiteColor)
			}
			row += "  " + pathStyle.Render(entry.subtitle)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render("No entries"))
	}

	body := title + "\n" + tabLine + "\n" + contextLine + "\n\n" + filterLine + "\n\n" + strings.Join(rows, "\n")
	if m.err != "" {
		body += "\n\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dangerColor).Render(m.err)
	}

	content := padToHeight(body, m.height-3)
	hint := renderDialogHintBar(m.width, m.hint())
	return content + "\n" + hint
}

type launcherEntriesLoadedMsg struct {
	entries []launcherEntry
	err     error
}

func (m *LauncherModel) advanceTab(delta int) {
	m.tab = launcherTab((int(m.tab) + delta + len(launcherTabs)) % len(launcherTabs))
	m.repoPath = ""
	m.filter = ""
	m.cursor = 0
}

func (m LauncherModel) handleBack() (LauncherModel, tea.Cmd) {
	switch m.tab {
	case launcherTabDirectories:
		parent := filepath.Dir(m.currentPath)
		if parent == m.currentPath {
			return m, nil
		}
		m.currentPath = parent
		m.cursor = 0
		return m, m.reload()
	case launcherTabRepos:
		if m.repoPath != "" {
			m.repoPath = ""
			m.cursor = 0
			return m, m.reload()
		}
		parent := filepath.Dir(m.currentPath)
		if parent == m.currentPath {
			return m, nil
		}
		m.currentPath = parent
		m.cursor = 0
		return m, m.reload()
	default:
		return m, nil
	}
}

func (m LauncherModel) handleForward() (LauncherModel, tea.Cmd) {
	entry := m.selectedEntry()
	if entry == nil {
		return m, nil
	}
	switch m.tab {
	case launcherTabDirectories:
		if entry.kind == "dir" {
			m.currentPath = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.reload()
		}
	case launcherTabRepos:
		if entry.kind == "repo" {
			m.repoPath = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.reload()
		}
	}
	return m, nil
}

func (m LauncherModel) handleEnter() (LauncherModel, tea.Cmd) {
	entry := m.selectedEntry()
	if entry == nil {
		asmlog.Debugf("launcher: enter ignored session=%q tab=%d current_path=%q", asmtmux.SessionName, m.tab, m.currentPath)
		return m, nil
	}
	asmlog.Debugf("launcher: enter session=%q tab=%d kind=%q path=%q repo_path=%q",
		asmtmux.SessionName, m.tab, entry.kind, entry.path, m.repoPath)
	if m.tab == launcherTabRepos && entry.kind == "repo" {
		m.repoPath = entry.path
		m.cursor = 0
		m.filter = ""
		asmlog.Debugf("launcher: drilling into repo session=%q repo_path=%q", asmtmux.SessionName, m.repoPath)
		return m, m.reload()
	}
	m.SelectedPath = entry.path
	asmlog.Debugf("launcher: selected path=%q session=%q", m.SelectedPath, asmtmux.SessionName)
	return m, tea.Quit
}

func (m LauncherModel) reload() tea.Cmd {
	tab := m.tab
	currentPath := m.currentPath
	repoPath := m.repoPath
	filter := strings.ToLower(strings.TrimSpace(m.filter))
	t := m.tracker
	taskCache := m.taskCache
	return func() tea.Msg {
		activeKinds := asmtmux.ListActiveSessions()
		resolver := newLauncherTaskResolver(t, taskCache)
		switch tab {
		case launcherTabRecent:
			entries, err := loadRecentEntries(filter, activeKinds, resolver)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		case launcherTabRepos:
			if repoPath != "" {
				entries, err := loadRepoWorktreeEntries(repoPath, filter, activeKinds, resolver)
				return launcherEntriesLoadedMsg{entries: entries, err: err}
			}
			entries, err := loadRepoEntries(currentPath, filter, activeKinds, resolver)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		default:
			entries, err := loadDirectoryEntries(currentPath, filter, activeKinds, resolver)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		}
	}
}

func loadRecentEntries(filter string, activeKinds map[string]asmtmux.SessionKind, resolver *launcherTaskResolver) ([]launcherEntry, error) {
	items, err := recent.Load()
	if err != nil {
		return nil, err
	}
	var entries []launcherEntry
	for _, item := range items {
		info, err := os.Stat(item.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		base := filepath.Base(item.Path)
		taskName := resolver.taskName(item.Path)
		if filter != "" && !matchesLauncherFilter(filter, base, taskName, item.Path) {
			continue
		}
		entries = append(entries, launcherEntry{
			label:    base,
			taskName: taskName,
			subtitle: item.Path,
			path:     item.Path,
			kind:     "recent",
			active:   activeKinds[item.Path] != 0,
		})
	}
	return entries, nil
}

func loadDirectoryEntries(currentPath, filter string, activeKinds map[string]asmtmux.SessionKind, resolver *launcherTaskResolver) ([]launcherEntry, error) {
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, err
	}

	currentTask := resolver.taskName(currentPath)
	rows := []launcherEntry{{
		label:    "[Open current directory]",
		taskName: currentTask,
		subtitle: currentPath,
		path:     currentPath,
		kind:     "open-current",
		active:   activeKinds[currentPath] != 0,
	}}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(currentPath, entry.Name())
		taskName := resolver.taskName(path)
		if filter != "" && !matchesLauncherFilter(filter, entry.Name(), taskName, path) {
			continue
		}
		rows = append(rows, launcherEntry{
			label:    entry.Name(),
			taskName: taskName,
			subtitle: path,
			path:     path,
			kind:     "dir",
			active:   activeKinds[path] != 0,
		})
	}
	return rows, nil
}

func loadRepoEntries(currentPath, filter string, activeKinds map[string]asmtmux.SessionKind, resolver *launcherTaskResolver) ([]launcherEntry, error) {
	var rows []launcherEntry
	seen := make(map[string]bool)
	addRepo := func(path string) {
		clean := filepath.Clean(path)
		if seen[clean] || !worktree.IsRepoMode(clean) {
			return
		}
		seen[clean] = true
		label := worktree.RepoName(clean)
		if label == "" {
			label = filepath.Base(clean)
		}
		taskName := resolver.taskName(clean)
		if filter != "" && !matchesLauncherFilter(filter, label, taskName, clean) {
			return
		}
		rows = append(rows, launcherEntry{
			label:    label,
			taskName: taskName,
			subtitle: clean,
			path:     clean,
			kind:     "repo",
			active:   activeKinds[clean] != 0,
		})
	}

	addRepo(currentPath)
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		addRepo(filepath.Join(currentPath, entry.Name()))
	}
	return rows, nil
}

func loadRepoWorktreeEntries(repoPath, filter string, activeKinds map[string]asmtmux.SessionKind, resolver *launcherTaskResolver) ([]launcherEntry, error) {
	wts, err := worktree.ScanRepo(repoPath)
	if err != nil {
		return nil, err
	}
	mainRepo, _ := worktree.FindMainRepo(repoPath)
	var rows []launcherEntry
	for _, wt := range wts {
		label := wt.Name
		if filepath.Clean(wt.Path) == filepath.Clean(mainRepo) {
			label = "main"
		}
		taskName := resolver.taskName(wt.Path)
		if filter != "" && !matchesLauncherFilter(filter, label, taskName, wt.Path) {
			continue
		}
		rows = append(rows, launcherEntry{
			label:    label,
			taskName: taskName,
			subtitle: wt.Path,
			path:     wt.Path,
			kind:     "repo-target",
			active:   activeKinds[wt.Path] != 0,
		})
	}
	return rows, nil
}

type launcherTaskResolver struct {
	tracker   tracker.Tracker
	taskCache *tracker.PathCache
	peeker    tracker.Peeker
	taskNames map[string]string
}

func newLauncherTaskResolver(t tracker.Tracker, taskCache *tracker.PathCache) *launcherTaskResolver {
	r := &launcherTaskResolver{
		tracker:   t,
		taskCache: taskCache,
		taskNames: make(map[string]string),
	}
	if peeker, ok := t.(tracker.Peeker); ok {
		r.peeker = peeker
	}
	return r
}

func (r *launcherTaskResolver) taskName(path string) string {
	clean := filepath.Clean(path)
	if name, ok := r.taskNames[clean]; ok {
		return name
	}
	name := r.resolveTaskName(clean)
	r.taskNames[clean] = name
	return name
}

func (r *launcherTaskResolver) resolveTaskName(path string) string {
	if r.taskCache != nil {
		if entry, ok := r.taskCache.GetEntry(path); ok {
			if !worktree.IsRepoMode(path) {
				return entry.Info.Name
			}
			branch := worktree.CurrentBranch(path)
			if branch == "" || branch == entry.Branch {
				return entry.Info.Name
			}
			if r.peeker != nil {
				if info, ok := r.peeker.Peek(branch); ok {
					return info.Name
				}
			}
			return entry.Info.Name
		}
	}
	if !worktree.IsRepoMode(path) || r.peeker == nil {
		return ""
	}
	branch := worktree.CurrentBranch(path)
	if branch == "" {
		return ""
	}
	if info, ok := r.peeker.Peek(branch); ok {
		return info.Name
	}
	return ""
}

func matchesLauncherFilter(filter string, parts ...string) bool {
	var joined []string
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		joined = append(joined, part)
	}
	if len(joined) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(strings.Join(joined, " ")), filter)
}

func (m LauncherModel) selectedEntry() *launcherEntry {
	if len(m.entries) == 0 || m.cursor < 0 || m.cursor >= len(m.entries) {
		return nil
	}
	entry := m.entries[m.cursor]
	return &entry
}

func (m LauncherModel) contextLabel() string {
	switch m.tab {
	case launcherTabRecent:
		return "Recent targets"
	case launcherTabRepos:
		if m.repoPath != "" {
			return "Repo: " + m.repoPath
		}
		return "Browse repos in " + m.currentPath
	default:
		return m.currentPath
	}
}

func (m LauncherModel) hint() string {
	switch m.tab {
	case launcherTabRecent:
		return " Tab: switch view  ↑↓: move  Enter: launch  Backspace: filter  Esc: cancel"
	case launcherTabRepos:
		if m.repoPath != "" {
			return " Tab: switch view  ↑↓: move  ←: back to repos  Enter: launch  Backspace: filter  Esc: cancel"
		}
		return " Tab: switch view  ↑↓: move  ←→: parent/open repo  Enter: select repo  Backspace: filter  Esc: cancel"
	default:
		return " Tab: switch view  ↑↓: move  ←→: parent/enter dir  Enter: launch  Backspace: filter  Esc: cancel"
	}
}

func (m LauncherModel) renderTabs() string {
	var tabs []string
	for i, label := range launcherTabs {
		style := lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor)
		if launcherTab(i) == m.tab {
			style = style.Background(primaryColor).Foreground(lipgloss.Color("0")).Bold(true)
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.NewStyle().Padding(0, 2).Render(lipgloss.JoinHorizontal(lipgloss.Left, tabs...))
}
