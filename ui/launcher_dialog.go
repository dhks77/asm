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

var launcherTabs = []string{"Favorites", "Recent", "Directories", "Repos"}

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
	tab             launcherTab
	currentPath     string
	repoPath        string
	favoriteDirRoot string
	entries         []launcherEntry
	cursor          int
	filter          string
	SelectedPath    string
	width           int
	height          int
	err             string
	tracker         tracker.Tracker
	taskCache       *tracker.PathCache
}

func NewLauncherModel(initialPath string, t tracker.Tracker, taskCache *tracker.PathCache) LauncherModel {
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
		case "ctrl+f":
			return m.toggleFavorite()
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
		if m.isFavoritesRootView() {
			switch entry.kind {
			case "favorite-dir":
				label += " " + lipgloss.NewStyle().Foreground(secondaryColor).Render("[dir]")
			case "favorite-repo":
				label += " " + lipgloss.NewStyle().Foreground(activeColor).Render("[repo]")
			}
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
	entries []launcherEntry
	err     error
}

func (m *LauncherModel) advanceTab(delta int) {
	m.tab = launcherTab((int(m.tab) + delta + len(launcherTabs)) % len(launcherTabs))
	m.repoPath = ""
	m.favoriteDirRoot = ""
	m.filter = ""
	m.cursor = 0
}

func (m LauncherModel) handleBack() (LauncherModel, tea.Cmd) {
	switch m.tab {
	case launcherTabFavorites:
		if m.repoPath != "" {
			m.repoPath = ""
			m.cursor = 0
			m.filter = ""
			return m, m.reload()
		}
		if m.favoriteDirRoot != "" {
			if filepath.Clean(m.currentPath) == filepath.Clean(m.favoriteDirRoot) {
				m.favoriteDirRoot = ""
				m.cursor = 0
				m.filter = ""
				return m, m.reload()
			}
			parent := filepath.Dir(m.currentPath)
			if parent == m.currentPath {
				m.favoriteDirRoot = ""
				m.cursor = 0
				m.filter = ""
				return m, m.reload()
			}
			m.currentPath = parent
			m.cursor = 0
			m.filter = ""
			return m, m.reload()
		}
		return m, nil
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
	case launcherTabFavorites:
		if m.repoPath != "" {
			return m, nil
		}
		if m.favoriteDirRoot != "" {
			if entry.kind == "dir" {
				m.currentPath = entry.path
				m.cursor = 0
				m.filter = ""
				return m, m.reload()
			}
			return m, nil
		}
		switch entry.kind {
		case "favorite-dir":
			m.currentPath = entry.path
			m.favoriteDirRoot = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.reload()
		case "favorite-repo":
			m.repoPath = entry.path
			m.cursor = 0
			m.filter = ""
			return m, m.reload()
		}
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
	if m.tab == launcherTabFavorites && m.isFavoritesRootView() && entry.kind == "favorite-repo" {
		m.repoPath = entry.path
		m.cursor = 0
		m.filter = ""
		asmlog.Debugf("launcher: drilling into favorite repo session=%q repo_path=%q", asmtmux.SessionName, m.repoPath)
		return m, m.reload()
	}
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
	return m, m.reload()
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
		return asmfavorites.KindDir, entry.path, true
	case launcherTabDirectories:
		if entry == nil || (entry.kind != "open-current" && entry.kind != "dir") {
			return "", "", false
		}
		return asmfavorites.KindDir, entry.path, true
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

func (m LauncherModel) reload() tea.Cmd {
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
			return launcherEntriesLoadedMsg{err: err}
		}
		favoriteSet := launcherFavoriteSet(favoriteEntries)
		resolver := newLauncherTaskResolver(t, taskCache)
		switch tab {
		case launcherTabFavorites:
			if repoPath != "" {
				entries, err := loadRepoWorktreeEntries(repoPath, filter, activeTargets, favoriteSet, resolver)
				return launcherEntriesLoadedMsg{entries: entries, err: err}
			}
			if favoriteDirRoot != "" {
				entries, err := loadDirectoryEntries(currentPath, filter, activeTargets, favoriteSet, resolver)
				return launcherEntriesLoadedMsg{entries: entries, err: err}
			}
			entries, err := loadFavoriteEntries(filter, activeTargets, resolver, favoriteEntries)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		case launcherTabRecent:
			entries, err := loadRecentEntries(filter, activeTargets, favoriteSet, resolver)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		case launcherTabRepos:
			if repoPath != "" {
				entries, err := loadRepoWorktreeEntries(repoPath, filter, activeTargets, favoriteSet, resolver)
				return launcherEntriesLoadedMsg{entries: entries, err: err}
			}
			entries, err := loadRepoEntries(currentPath, filter, activeTargets, favoriteSet, resolver)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		default:
			entries, err := loadDirectoryEntries(currentPath, filter, activeTargets, favoriteSet, resolver)
			return launcherEntriesLoadedMsg{entries: entries, err: err}
		}
	}
}

func loadRecentEntries(filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, error) {
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
			active:   activeTargets.hasPath(item.Path),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindDir, item.Path)],
		})
	}
	return entries, nil
}

func loadDirectoryEntries(currentPath, filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, error) {
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
		active:   activeTargets.hasPath(currentPath),
		favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindDir, currentPath)],
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
			active:   activeTargets.hasPath(path),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindDir, path)],
		})
	}
	return rows, nil
}

func loadRepoEntries(currentPath, filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, error) {
	var rows []launcherEntry
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
			active:   activeTargets.hasRepo(clean),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindRepo, clean)],
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

func loadRepoWorktreeEntries(repoPath, filter string, activeTargets launcherActiveTargets, favoriteSet map[string]bool, resolver *launcherTaskResolver) ([]launcherEntry, error) {
	wts, err := worktree.ScanRepo(repoPath)
	if err != nil {
		return nil, err
	}
	var rows []launcherEntry
	for _, wt := range wts {
		label := worktree.CurrentBranch(wt.Path)
		if label == "" {
			label = wt.Name
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
			active:   activeTargets.hasPath(wt.Path),
			favorite: favoriteSet[launcherFavoriteKey(asmfavorites.KindDir, wt.Path)],
		})
	}
	return rows, nil
}

func loadFavoriteEntries(filter string, activeTargets launcherActiveTargets, resolver *launcherTaskResolver, items []asmfavorites.Entry) ([]launcherEntry, error) {
	var rows []launcherEntry
	for _, item := range items {
		info, err := os.Stat(item.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		clean := filepath.Clean(item.Path)
		switch item.Kind {
		case asmfavorites.KindDir:
			label := filepath.Base(clean)
			taskName := resolver.taskName(clean)
			if filter != "" && !matchesLauncherFilter(filter, label, taskName, clean) {
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
		case asmfavorites.KindRepo:
			if !worktree.IsRepoMode(clean) {
				continue
			}
			_, label := config.ProjectIdentity(clean)
			if label == "" {
				label = filepath.Base(clean)
			}
			taskName := resolver.taskName(clean)
			if filter != "" && !matchesLauncherFilter(filter, label, taskName, clean) {
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
		}
	}
	return rows, nil
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
		set[launcherFavoriteKey(entry.Kind, entry.Path)] = true
	}
	return set
}

func launcherFavoriteKey(kind asmfavorites.Kind, path string) string {
	return string(kind) + ":" + filepath.Clean(path)
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
	case launcherTabRepos:
		if m.repoPath != "" {
			return " Tab: switch view  ↑↓: move  ←: back to repos  Enter: launch  Ctrl+F: favorite repo  Backspace: filter  Esc: cancel"
		}
		return " Tab: switch view  ↑↓: move  ←→: parent/open repo  Enter: select repo  Ctrl+F: favorite repo  Backspace: filter  Esc: cancel"
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
