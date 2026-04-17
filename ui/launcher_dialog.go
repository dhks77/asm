package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/asmlog"
	"github.com/nhn/asm/config"
	asmfavorites "github.com/nhn/asm/favorites"
	"github.com/nhn/asm/recent"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/worktree"
)

type launcherTab int

const (
	launcherTabFavorites launcherTab = iota
	launcherTabRecent
	launcherTabDirectories
	launcherTabRepos
)

var launcherTabs = []string{"Favorites", "Recent", "Directories"}

type launcherEntry struct {
	label    string
	taskName string
	subtitle string
	path     string
	kind     string
	active   bool
	favorite bool
}

type launcherActiveTargets struct {
	paths     map[string]asmtmux.SessionKind
	repoNames map[string]bool
}

// LauncherModel is a standalone launcher for the working panel.
type LauncherModel struct {
	tab                  launcherTab
	currentPath          string
	repoPath             string
	favoriteDirRoot      string
	entries              []launcherEntry
	cursor               int
	viewTop              int
	filter               string
	SelectedPath         string
	width                int
	height               int
	err                  string
	tracker              tracker.Tracker
	taskCache            *tracker.TaskCache
	loadVersion          int
	taskFetchQueue       []string
	taskFetchPending     bool
	pendingSelectionPath string
}

func NewLauncherModel(initialPath string, t tracker.Tracker, taskCache *tracker.TaskCache) LauncherModel {
	clean := filepath.Clean(initialPath)
	if clean == "." || clean == "" {
		if cwd, err := os.Getwd(); err == nil {
			clean = cwd
		}
	}
	return LauncherModel{
		tab:         launcherTabFavorites,
		currentPath: clean,
		tracker:     t,
		taskCache:   taskCache,
	}
}

func (m LauncherModel) Init() tea.Cmd {
	return m.reload(m.loadVersion)
}

func (m LauncherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case launcherEntriesLoadedMsg:
		if msg.version != m.loadVersion {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			m.entries = nil
			m.cursor = 0
			m.viewTop = 0
			m.taskFetchQueue = nil
			m.taskFetchPending = false
			return m, nil
		}
		m.err = ""
		m.entries = msg.entries
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.applyPendingSelection()
		m.clampViewTop()
		m.taskFetchQueue = dedupeLauncherPaths(msg.pendingPaths)
		m.taskFetchPending = false
		return m, m.startNextTaskFetch()

	case launcherTaskResolvedMsg:
		if msg.version != m.loadVersion {
			return m, nil
		}
		m.taskFetchPending = false
		for i := range m.entries {
			if filepath.Clean(m.entries[i].path) != msg.path {
				continue
			}
			m.entries[i].taskName = msg.taskName
		}
		return m, m.startNextTaskFetch()

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.advanceTab(+1)
			return m, m.triggerReload()
		case "shift+tab":
			m.advanceTab(-1)
			return m, m.triggerReload()
		case "up":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.viewTop {
					m.viewTop = m.cursor
				}
			}
		case "down":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
				m.adjustViewTop()
			}
		case "left":
			return m.handleBack()
		case "right":
			return m.handleForward()
		case "enter":
			return m.handleEnter()
		case "ctrl+f":
			return m.toggleFavorite()
		case "backspace":
			if len(m.filter) == 0 {
				return m, nil
			}
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.cursor = 0
			return m, m.triggerReload()
		case "esc", "ctrl+c":
			return m, tea.Quit
		default:
			switch msg.Type {
			case tea.KeyRunes:
				m.filter += string(msg.Runes)
				m.cursor = 0
				return m, m.triggerReload()
			case tea.KeySpace:
				m.filter += " "
				m.cursor = 0
				return m, m.triggerReload()
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
	start, end := m.visibleRange()
	for i := start; i < end; i++ {
		entry := m.entries[i]
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
		if m.isFavoritesRootView() {
			switch entry.kind {
			case "favorite-dir":
				label += " " + lipgloss.NewStyle().Foreground(secondaryColor).Render("[dir]")
			case "favorite-repo":
				label += " " + lipgloss.NewStyle().Foreground(activeColor).Render("[repo]")
			}
		} else if entry.kind == "repo" || entry.kind == "open-current-repo" {
			label += " " + lipgloss.NewStyle().Foreground(activeColor).Render("[repo]")
		}
		if entry.favorite && !m.isFavoritesRootView() {
			label += " " + lipgloss.NewStyle().Foreground(warnColor).Render("[fav]")
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
		rows = append(rows, lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render(m.emptyMessage()))
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
	version      int
	entries      []launcherEntry
	pendingPaths []string
	err          error
}

type launcherTaskResolvedMsg struct {
	version  int
	path     string
	taskName string
}

func (m *LauncherModel) advanceTab(delta int) {
	m.tab = launcherTab((int(m.tab) + delta + len(launcherTabs)) % len(launcherTabs))
	m.repoPath = ""
	m.favoriteDirRoot = ""
	m.filter = ""
	m.cursor = 0
	m.viewTop = 0
}

func (m LauncherModel) handleBack() (LauncherModel, tea.Cmd) {
	switch m.tab {
	case launcherTabFavorites:
		if m.repoPath != "" {
			prevRepoPath := m.repoPath
			m.repoPath = ""
			m.cursor = 0
			m.filter = ""
			return m, m.triggerReloadSelecting(prevRepoPath)
		}
		if m.favoriteDirRoot != "" {
			if filepath.Clean(m.currentPath) == filepath.Clean(m.favoriteDirRoot) {
				prevFavoriteRoot := m.favoriteDirRoot
				m.favoriteDirRoot = ""
				m.cursor = 0
				m.filter = ""
				return m, m.triggerReloadSelecting(prevFavoriteRoot)
			}
			childPath := m.currentPath
			parent := filepath.Dir(m.currentPath)
			if parent == m.currentPath {
				prevFavoriteRoot := m.favoriteDirRoot
				m.favoriteDirRoot = ""
				m.cursor = 0
				m.filter = ""
				return m, m.triggerReloadSelecting(prevFavoriteRoot)
			}
			m.currentPath = parent
			m.cursor = 0
			m.filter = ""
			return m, m.triggerReloadSelecting(childPath)
		}
		return m, nil
	case launcherTabDirectories:
		if m.repoPath != "" {
			prevRepoPath := m.repoPath
			m.repoPath = ""
			m.cursor = 0
			return m, m.triggerReloadSelecting(prevRepoPath)
		}
		childPath := m.currentPath
		parent := filepath.Dir(m.currentPath)
		if parent == m.currentPath {
			return m, nil
		}
		m.currentPath = parent
		m.cursor = 0
		return m, m.triggerReloadSelecting(childPath)
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
	case launcherTabFavorites:
		if m.repoPath != "" {
			return m, nil
		}
		if m.favoriteDirRoot != "" {
			if entry.kind == "dir" {
				m.currentPath = entry.path
				m.cursor = 0
				m.filter = ""
				return m, m.triggerReload()
			}
			return m, nil
		}
		switch entry.kind {
		case "favorite-dir":
			m.currentPath = entry.path
			m.favoriteDirRoot = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.triggerReload()
		case "favorite-repo":
			m.repoPath = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.triggerReload()
		}
	case launcherTabDirectories:
		switch entry.kind {
		case "repo", "open-current-repo":
			m.repoPath = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.triggerReload()
		case "dir":
			m.currentPath = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.triggerReload()
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
	if m.tab == launcherTabFavorites && m.isFavoritesRootView() && entry.kind == "favorite-repo" {
		m.repoPath = entry.path
		m.cursor = 0
		m.filter = ""
		asmlog.Debugf("launcher: drilling into favorite repo session=%q repo_path=%q", asmtmux.SessionName, m.repoPath)
		return m, m.triggerReload()
	}
	if m.tab == launcherTabRepos && entry.kind == "repo" {
		m.repoPath = entry.path
		m.cursor = 0
		m.filter = ""
		asmlog.Debugf("launcher: drilling into repo session=%q repo_path=%q", asmtmux.SessionName, m.repoPath)
		return m, m.triggerReload()
	}
	m.SelectedPath = entry.path
	asmlog.Debugf("launcher: selected path=%q session=%q", m.SelectedPath, asmtmux.SessionName)
	return m, tea.Quit
}

func (m LauncherModel) toggleFavorite() (LauncherModel, tea.Cmd) {
	kind, path, ok := m.favoriteToggleTarget()
	if !ok {
		return m, nil
	}
	added, err := asmfavorites.Toggle(kind, path)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	if !added {
		clean := filepath.Clean(path)
		if kind == asmfavorites.KindRepo && filepath.Clean(m.repoPath) == clean {
			m.repoPath = ""
			m.cursor = 0
			m.filter = ""
		}
		if kind == asmfavorites.KindDir && filepath.Clean(m.favoriteDirRoot) == clean {
			m.favoriteDirRoot = ""
			m.cursor = 0
			m.filter = ""
		}
	}
	return m, m.triggerReload()
}

func (m LauncherModel) favoriteToggleTarget() (asmfavorites.Kind, string, bool) {
	entry := m.selectedEntry()
	switch m.tab {
	case launcherTabFavorites:
		if m.repoPath != "" {
			return asmfavorites.KindRepo, m.repoPath, true
		}
		if m.favoriteDirRoot != "" {
			if entry == nil || (entry.kind != "open-current" && entry.kind != "dir") {
				return "", "", false
			}
			return asmfavorites.KindDir, entry.path, true
		}
		if entry == nil {
			return "", "", false
		}
		switch entry.kind {
		case "favorite-dir":
			return asmfavorites.KindDir, entry.path, true
		case "favorite-repo":
			return asmfavorites.KindRepo, entry.path, true
		}
	case launcherTabRecent:
		if entry == nil {
			return "", "", false
		}
		return launcherFavoriteKindForPath(entry.path), entry.path, true
	case launcherTabDirectories:
		if m.repoPath != "" {
			return asmfavorites.KindRepo, m.repoPath, true
		}
		if entry == nil {
			return "", "", false
		}
		switch entry.kind {
		case "repo", "open-current-repo":
			return asmfavorites.KindRepo, entry.path, true
		case "open-current", "dir":
			return asmfavorites.KindDir, entry.path, true
		default:
			return "", "", false
		}
	case launcherTabRepos:
		if m.repoPath != "" {
			return asmfavorites.KindRepo, m.repoPath, true
		}
		if entry == nil || entry.kind != "repo" {
			return "", "", false
		}
		return asmfavorites.KindRepo, entry.path, true
	}
	return "", "", false
}

func (m *LauncherModel) triggerReload() tea.Cmd {
	return m.beginReload("")
}

func (m *LauncherModel) triggerReloadSelecting(path string) tea.Cmd {
	return m.beginReload(path)
}

func (m *LauncherModel) beginReload(path string) tea.Cmd {
	m.pendingSelectionPath = ""
	if strings.TrimSpace(path) != "" {
		m.pendingSelectionPath = filepath.Clean(path)
	}
	m.loadVersion++
	m.taskFetchQueue = nil
	m.taskFetchPending = false
	return m.reload(m.loadVersion)
}

func (m LauncherModel) reload(version int) tea.Cmd {
	tab := m.tab
	currentPath := m.currentPath
	repoPath := m.repoPath
	favoriteDirRoot := m.favoriteDirRoot
	filter := strings.ToLower(strings.TrimSpace(m.filter))
	t := m.tracker
	taskCache := m.taskCache
	return func() tea.Msg {
		activeTargets := newLauncherActiveTargets(asmtmux.ListActiveSessions())
		favoriteEntries, err := asmfavorites.Load()
		if err != nil {
			return launcherEntriesLoadedMsg{version: version, err: err}
		}
		favoriteSet := launcherFavoriteSet(favoriteEntries)
		resolver := newLauncherTaskResolver(t, taskCache)
		switch tab {
		case launcherTabFavorites:
			if repoPath != "" {
				entries, pendingPaths, err := loadRepoWorktreeEntries(repoPath, filter, activeTargets, favoriteSet, resolver)
				return launcherEntriesLoadedMsg{version: version, entries: entries, pendingPaths: pendingPaths, err: err}
			}
			if favoriteDirRoot != "" {
				entries, pendingPaths, err := loadDirectoryEntries(currentPath, filter, activeTargets, favoriteSet, resolver, true)
				return launcherEntriesLoadedMsg{version: version, entries: entries, pendingPaths: pendingPaths, err: err}
			}
			entries, pendingPaths, err := loadFavoriteEntries(filter, activeTargets, resolver, favoriteEntries)
			return launcherEntriesLoadedMsg{version: version, entries: entries, pendingPaths: pendingPaths, err: err}
		case launcherTabRecent:
			entries, pendingPaths, err := loadRecentEntries(filter, activeTargets, favoriteSet, resolver)
			return launcherEntriesLoadedMsg{version: version, entries: entries, pendingPaths: pendingPaths, err: err}
		case launcherTabDirectories, launcherTabRepos:
			if repoPath != "" {
				entries, pendingPaths, err := loadRepoWorktreeEntries(repoPath, filter, activeTargets, favoriteSet, resolver)
				return launcherEntriesLoadedMsg{version: version, entries: entries, pendingPaths: pendingPaths, err: err}
			}
			entries, pendingPaths, err := loadDirectoryEntries(currentPath, filter, activeTargets, favoriteSet, resolver, false)
			return launcherEntriesLoadedMsg{version: version, entries: entries, pendingPaths: pendingPaths, err: err}
		}
		return launcherEntriesLoadedMsg{version: version}
	}
}

func loadRecentEntries(filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, []string, error) {
	items, err := recent.Load()
	if err != nil {
		return nil, nil, err
	}
	var entries []launcherEntry
	var pendingPaths []string
	for _, item := range items {
		info, err := os.Stat(item.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		base := filepath.Base(item.Path)
		taskName, needsResolve, include := launcherTaskNameForEntry(filter, base, item.Path, resolver)
		if !include {
			continue
		}
		entries = append(entries, launcherEntry{
			label:    base,
			taskName: taskName,
			subtitle: item.Path,
			path:     item.Path,
			kind:     "recent",
			active:   activeTargets.hasPath(item.Path),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindDir, item.Path)],
		})
		if needsResolve {
			pendingPaths = append(pendingPaths, item.Path)
		}
	}
	return entries, pendingPaths, nil
}

func loadDirectoryEntries(currentPath, filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver, resolveTasks bool) ([]launcherEntry, []string, error) {
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, nil, err
	}

	currentKind := "open-current"
	if worktree.IsRepoMode(currentPath) {
		currentKind = "open-current-repo"
	}
	currentTask, currentNeedsResolve, includeCurrent := launcherDirectoryTaskNameForEntry(filter, "[Open current directory]", currentPath, resolver, resolveTasks)
	currentFavoriteKind := asmfavorites.KindDir
	if currentKind == "open-current-repo" {
		currentFavoriteKind = asmfavorites.KindRepo
	}
	rows := []launcherEntry{{
		label:    "[Open current directory]",
		taskName: currentTask,
		subtitle: currentPath,
		path:     currentPath,
		kind:     currentKind,
		active:   activeTargets.hasPath(currentPath),
		favorite: favoriteSet[launcherFavoriteKey(currentFavoriteKind, currentPath)],
	}}
	var pendingPaths []string
	if currentNeedsResolve {
		pendingPaths = append(pendingPaths, currentPath)
	}
	if filter != "" && !includeCurrent {
		rows = nil
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(currentPath, entry.Name())
		entryKind := "dir"
		favoriteKind := asmfavorites.KindDir
		if worktree.IsRepoMode(path) {
			entryKind = "repo"
			favoriteKind = asmfavorites.KindRepo
		}
		taskName, needsResolve, include := launcherDirectoryTaskNameForEntry(filter, entry.Name(), path, resolver, resolveTasks)
		if !include {
			continue
		}
		rows = append(rows, launcherEntry{
			label:    entry.Name(),
			taskName: taskName,
			subtitle: path,
			path:     path,
			kind:     entryKind,
			active:   activeTargets.hasPath(path),
			favorite: favoriteSet[launcherFavoriteKey(favoriteKind, path)],
		})
		if needsResolve {
			pendingPaths = append(pendingPaths, path)
		}
	}
	return rows, pendingPaths, nil
}

func loadRepoEntries(currentPath, filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, []string, error) {
	var rows []launcherEntry
	var pendingPaths []string
	seen := make(map[string]bool)
	addRepo := func(path string) {
		clean := filepath.Clean(path)
		if seen[clean] || !worktree.IsRepoMode(clean) {
			return
		}
		seen[clean] = true
		_, label := config.ProjectIdentity(clean)
		if label == "" {
			label = filepath.Base(clean)
		}
		taskName, needsResolve, include := launcherTaskNameForEntry(filter, label, clean, resolver)
		if !include {
			return
		}
		rows = append(rows, launcherEntry{
			label:    label,
			taskName: taskName,
			subtitle: clean,
			path:     clean,
			kind:     "repo",
			active:   activeTargets.hasRepo(clean),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindRepo, clean)],
		})
		if needsResolve {
			pendingPaths = append(pendingPaths, clean)
		}
	}

	addRepo(currentPath)
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		addRepo(filepath.Join(currentPath, entry.Name()))
	}
	return rows, pendingPaths, nil
}

func loadRepoWorktreeEntries(repoPath, filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, []string, error) {
	wts, err := worktree.ScanRepo(repoPath)
	if err != nil {
		return nil, nil, err
	}
	var rows []launcherEntry
	var pendingPaths []string
	for _, wt := range wts {
		label := worktree.CurrentBranch(wt.Path)
		if label == "" {
			label = wt.Name
		}
		taskName, needsResolve, include := launcherTaskNameForEntry(filter, label, wt.Path, resolver)
		if !include {
			continue
		}
		rows = append(rows, launcherEntry{
			label:    label,
			taskName: taskName,
			subtitle: wt.Path,
			path:     wt.Path,
			kind:     "repo-target",
			active:   activeTargets.hasPath(wt.Path),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindDir, wt.Path)],
		})
		if needsResolve {
			pendingPaths = append(pendingPaths, wt.Path)
		}
	}
	return rows, pendingPaths, nil
}

func loadFavoriteEntries(filter string, activeTargets launcherActiveTargets, resolver *launcherTaskResolver, items []asmfavorites.Entry) ([]launcherEntry, []string, error) {
	var rows []launcherEntry
	var pendingPaths []string
	for _, item := range items {
		info, err := os.Stat(item.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		clean := filepath.Clean(item.Path)
		switch launcherFavoriteKindForPath(clean) {
		case asmfavorites.KindDir:
			label := filepath.Base(clean)
			taskName, needsResolve, include := launcherTaskNameForEntry(filter, label, clean, resolver)
			if !include {
				continue
			}
			rows = append(rows, launcherEntry{
				label:    label,
				taskName: taskName,
				subtitle: clean,
				path:     clean,
				kind:     "favorite-dir",
				active:   activeTargets.hasPath(clean),
				favorite: true,
			})
			if needsResolve {
				pendingPaths = append(pendingPaths, clean)
			}
		case asmfavorites.KindRepo:
			_, label := config.ProjectIdentity(clean)
			if label == "" {
				label = filepath.Base(clean)
			}
			taskName, needsResolve, include := launcherTaskNameForEntry(filter, label, clean, resolver)
			if !include {
				continue
			}
			rows = append(rows, launcherEntry{
				label:    label,
				taskName: taskName,
				subtitle: clean,
				path:     clean,
				kind:     "favorite-repo",
				active:   activeTargets.hasRepo(clean),
				favorite: true,
			})
			if needsResolve {
				pendingPaths = append(pendingPaths, clean)
			}
		}
	}
	return rows, pendingPaths, nil
}

func dedupeLauncherPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

func newLauncherActiveTargets(activeKinds map[string]asmtmux.SessionKind) launcherActiveTargets {
	active := launcherActiveTargets{
		paths:     activeKinds,
		repoNames: make(map[string]bool),
	}
	for path := range activeKinds {
		_, label := config.ProjectIdentity(path)
		if label != "" && label != "." {
			active.repoNames[label] = true
		}
	}
	return active
}

func (a launcherActiveTargets) hasPath(path string) bool {
	return a.paths[filepath.Clean(path)] != 0
}

func (a launcherActiveTargets) hasRepo(repoPath string) bool {
	clean := filepath.Clean(repoPath)
	if a.hasPath(clean) {
		return true
	}
	_, label := config.ProjectIdentity(clean)
	if label == "" || label == "." {
		return false
	}
	return a.repoNames[label]
}

func launcherFavoriteSet(entries []asmfavorites.Entry) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, entry := range entries {
		set[launcherFavoriteKey(launcherFavoriteKindForPath(entry.Path), entry.Path)] = true
	}
	return set
}

func launcherFavoriteKey(kind asmfavorites.Kind, path string) string {
	return string(kind) + ":" + filepath.Clean(path)
}

func launcherFavoriteKindForPath(path string) asmfavorites.Kind {
	if worktree.IsRepoMode(path) {
		return asmfavorites.KindRepo
	}
	return asmfavorites.KindDir
}

type launcherTaskResolver struct {
	tracker   tracker.Tracker
	taskCache *tracker.TaskCache
	peeker    tracker.Peeker
	taskNames map[string]string
}

func newLauncherTaskResolver(t tracker.Tracker, taskCache *tracker.TaskCache) *launcherTaskResolver {
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

func (r *launcherTaskResolver) cachedTaskName(path string) (string, bool) {
	clean := filepath.Clean(path)
	if name, ok := r.taskNames[clean]; ok {
		return name, false
	}
	if r.taskCache != nil {
		if entry, ok := r.taskCache.GetEntry(clean); ok {
			r.taskNames[clean] = entry.Info.Name
			return entry.Info.Name, false
		}
	}
	if !worktree.IsRepoMode(clean) {
		return "", false
	}
	return "", true
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

func launcherTaskNameForEntry(filter, label, path string, resolver *launcherTaskResolver) (string, bool, bool) {
	taskName, needsResolve := resolver.cachedTaskName(path)
	if filter == "" {
		return taskName, needsResolve, true
	}
	if matchesLauncherFilter(filter, label, path) {
		return taskName, needsResolve, true
	}
	if taskName == "" && needsResolve {
		taskName = resolver.taskName(path)
		needsResolve = false
	}
	return taskName, needsResolve, matchesLauncherFilter(filter, label, taskName, path)
}

func launcherDirectoryTaskNameForEntry(filter, label, path string, resolver *launcherTaskResolver, resolveTasks bool) (string, bool, bool) {
	if !resolveTasks {
		if filter == "" {
			return "", false, true
		}
		return "", false, matchesLauncherFilter(filter, label, path)
	}
	return launcherTaskNameForEntry(filter, label, path, resolver)
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
			if name := r.resolveRepoTaskName(path, branch); name != "" {
				return name
			}
			return entry.Info.Name
		}
	}
	if !worktree.IsRepoMode(path) {
		return ""
	}
	branch := worktree.CurrentBranch(path)
	return r.resolveRepoTaskName(path, branch)
}

func (r *launcherTaskResolver) resolveRepoTaskName(path, branch string) string {
	if branch == "" {
		return ""
	}
	if r.peeker != nil {
		if info, ok := r.peeker.Peek(branch); ok {
			r.persistTaskName(path, branch, info)
			return info.Name
		}
	}
	if r.tracker == nil {
		return ""
	}
	info := r.tracker.Resolve(branch)
	r.persistTaskName(path, branch, info)
	return info.Name
}

func (r *launcherTaskResolver) persistTaskName(path, branch string, info tracker.TaskInfo) {
	if r.taskCache == nil || info.Name == "" {
		return
	}
	r.taskCache.Set(path, branch, info)
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

func (m LauncherModel) startNextTaskFetch() tea.Cmd {
	if m.taskFetchPending || len(m.taskFetchQueue) == 0 {
		return nil
	}
	path := m.taskFetchQueue[0]
	m.taskFetchQueue = m.taskFetchQueue[1:]
	m.taskFetchPending = true
	return m.fetchTaskName(m.loadVersion, path)
}

func (m LauncherModel) fetchTaskName(version int, path string) tea.Cmd {
	t := m.tracker
	taskCache := m.taskCache
	return func() tea.Msg {
		resolver := newLauncherTaskResolver(t, taskCache)
		return launcherTaskResolvedMsg{
			version:  version,
			path:     filepath.Clean(path),
			taskName: resolver.taskName(path),
		}
	}
}

func (m *LauncherModel) applyPendingSelection() {
	if m.pendingSelectionPath == "" {
		return
	}
	target := filepath.Clean(m.pendingSelectionPath)
	m.pendingSelectionPath = ""
	for i := range m.entries {
		if filepath.Clean(m.entries[i].path) == target {
			m.cursor = i
			return
		}
	}
}

func (m *LauncherModel) adjustViewTop() {
	visible := m.visibleEntryCount()
	if visible <= 0 {
		return
	}
	if m.cursor < m.viewTop {
		m.viewTop = m.cursor
	}
	if m.cursor >= m.viewTop+visible {
		m.viewTop = m.cursor - visible + 1
	}
	m.clampViewTop()
}

func (m *LauncherModel) clampViewTop() {
	visible := m.visibleEntryCount()
	if visible <= 0 {
		m.viewTop = 0
		return
	}
	maxTop := max(0, len(m.entries)-visible)
	if m.viewTop > maxTop {
		m.viewTop = maxTop
	}
	if m.viewTop < 0 {
		m.viewTop = 0
	}
	if m.cursor < m.viewTop {
		m.viewTop = m.cursor
	}
}

func (m LauncherModel) visibleEntryCount() int {
	if m.height <= 0 {
		return 12
	}
	return max(1, m.height-9)
}

func (m LauncherModel) visibleRange() (int, int) {
	if len(m.entries) == 0 {
		return 0, 0
	}
	start := m.viewTop
	if start < 0 {
		start = 0
	}
	if start > len(m.entries) {
		start = len(m.entries)
	}
	end := min(len(m.entries), start+m.visibleEntryCount())
	return start, end
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
	case launcherTabFavorites:
		if m.repoPath != "" {
			return "Repo: " + m.repoPath
		}
		if m.favoriteDirRoot != "" {
			return m.currentPath
		}
		return "Favorite targets"
	case launcherTabRecent:
		return "Recent targets"
	case launcherTabDirectories:
		if m.repoPath != "" {
			return "Repo: " + m.repoPath
		}
		return m.currentPath
	default:
		return m.currentPath
	}
}

func (m LauncherModel) hint() string {
	switch m.tab {
	case launcherTabFavorites:
		if m.repoPath != "" {
			return " Tab: switch view  ↑↓: move  ←: back to favorites  Enter: launch  Ctrl+F: toggle favorite  Backspace: filter  Esc: cancel"
		}
		if m.favoriteDirRoot != "" {
			return " Tab: switch view  ↑↓: move  ←→: back/enter dir  Enter: launch  Ctrl+F: favorite  Backspace: filter  Esc: cancel"
		}
		return " Tab: switch view  ↑↓: move  ←→: browse  Enter: open/select  Ctrl+F: remove favorite  Backspace: filter  Esc: cancel"
	case launcherTabRecent:
		return " Tab: switch view  ↑↓: move  Enter: launch  Ctrl+F: favorite  Backspace: filter  Esc: cancel"
	case launcherTabDirectories:
		if m.repoPath != "" {
			return " Tab: switch view  ↑↓: move  ←: back to directories  Enter: launch  Ctrl+F: favorite repo  Backspace: filter  Esc: cancel"
		}
		return " Tab: switch view  ↑↓: move  ←→: parent/open dir or repo  Enter: launch  Ctrl+F: favorite  Backspace: filter  Esc: cancel"
	default:
		return " Tab: switch view  ↑↓: move  ←→: parent/enter dir  Enter: launch  Ctrl+F: favorite  Backspace: filter  Esc: cancel"
	}
}

func (m LauncherModel) isFavoritesRootView() bool {
	return m.tab == launcherTabFavorites && m.repoPath == "" && m.favoriteDirRoot == ""
}

func (m LauncherModel) emptyMessage() string {
	if m.isFavoritesRootView() {
		return "No favorites yet. Use Ctrl+F in Directories or Repos."
	}
	return "No entries"
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
