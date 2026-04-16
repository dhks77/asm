package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/config"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/worktree"
)

type worktreeDialogMode int

const (
	wtModeSelectBranch worktreeDialogMode = iota
	wtModeSelectBase
	wtModeNewBranch
)

// Messages

type BranchesLoadedMsg struct {
	Branches []worktree.Branch
	RepoDir  string
	RepoName string
	Err      error
	// CachedTasks are task infos already present in the persistent cache,
	// keyed by branch name. Seeded in one message to avoid the N-way
	// per-branch render wave.
	CachedTasks map[string]tracker.TaskInfo
}

type WorktreeCreatedMsg struct {
	Name             string
	Path             string
	TemplateCopied   int      // number of files copied from the template
	TemplateWarnings []string // non-fatal warnings from template application
}

type WorktreeCancelledMsg struct{}

type WorktreeErrorMsg struct {
	Err string
}

type wtTaskResolvedMsg struct {
	RepoDir string
	Branch  string
	Info    tracker.TaskInfo
}

type wtTaskRetryTickMsg struct {
	remaining int
}

type worktreeRepoChoice struct {
	Path string
	Root string
	Name string
}

// WorktreeDialogModel handles branch selection and worktree creation.
type WorktreeDialogModel struct {
	visible      bool
	mode         worktreeDialogMode
	branches     []worktree.Branch
	filtered     []worktree.Branch
	filter       string
	cursor       int
	scrollOffset int
	maxVisible   int

	newBranchName string
	baseBranch    string

	rootPath    string
	dirPath     string
	repoDir     string
	repoName    string
	repoChoices []worktreeRepoChoice
	repoIndex   int
	tracker     tracker.Tracker
	taskInfos   map[string]tracker.TaskInfo // branch name -> task info
	width       int
	height      int
	err         string
}

func NewWorktreeDialogModel(t tracker.Tracker) WorktreeDialogModel {
	return WorktreeDialogModel{
		maxVisible: 10,
		tracker:    t,
		taskInfos:  make(map[string]tracker.TaskInfo),
	}
}

func (m *WorktreeDialogModel) Show(rootPath, dirPath string) tea.Cmd {
	m.visible = true
	m.mode = wtModeSelectBranch
	m.filter = ""
	m.cursor = 0
	m.scrollOffset = 0
	m.newBranchName = ""
	m.baseBranch = ""
	m.rootPath = rootPath
	m.dirPath = dirPath
	m.repoChoices = worktreeRepoChoices(dirPath)
	m.repoIndex = currentRepoIndex(m.repoChoices, dirPath)
	m.err = ""
	m.branches = nil
	m.filtered = nil

	return m.loadBranches(dirPath)
}

func (m *WorktreeDialogModel) Hide() {
	m.visible = false
	m.branches = nil
	m.filtered = nil
}

func (m *WorktreeDialogModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// title(1) + repo(1) + blank(1) + filter(1) + blank(1) + status_bar(1) + margin(2) = 8
	m.maxVisible = h - 8
	if m.maxVisible < 3 {
		m.maxVisible = 3
	}
}

// adjustScrollOffset ensures the cursor item is visible within maxVisible lines.
func (m *WorktreeDialogModel) adjustScrollOffset() {
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
		return
	}
	// Count lines from scrollOffset to cursor (inclusive)
	usedLines := 0
	for i := m.scrollOffset; i <= m.cursor && i < len(m.filtered); i++ {
		usedLines++
		if info, ok := m.taskInfos[m.filtered[i].Name]; ok && info.Name != "" {
			usedLines++
		}
	}
	for usedLines > m.maxVisible && m.scrollOffset < m.cursor {
		usedLines--
		if info, ok := m.taskInfos[m.filtered[m.scrollOffset].Name]; ok && info.Name != "" {
			usedLines--
		}
		m.scrollOffset++
	}
}

func (m *WorktreeDialogModel) applyFilter() {
	hideWorktree := m.mode == wtModeSelectBranch
	lower := strings.ToLower(m.filter)
	m.filtered = nil
	for _, b := range m.branches {
		if hideWorktree && b.HasWorktree {
			continue
		}
		if m.filter != "" {
			nameMatch := strings.Contains(strings.ToLower(b.Name), lower)
			taskMatch := false
			if info, ok := m.taskInfos[b.Name]; ok {
				taskMatch = strings.Contains(strings.ToLower(info.Name), lower)
			}
			if !nameMatch && !taskMatch {
				continue
			}
		}
		m.filtered = append(m.filtered, b)
	}
	if len(m.filtered) == 0 {
		m.cursor = 0
		m.scrollOffset = 0
	} else if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	m.adjustScrollOffset()
}

func worktreeRepoChoices(currentPath string) []worktreeRepoChoice {
	type keyedChoice struct {
		root   string
		choice worktreeRepoChoice
	}

	seen := make(map[string]worktreeRepoChoice)
	addChoice := func(path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		root, name := config.ProjectIdentity(clean)
		if root == "" || root == "." {
			root = clean
		}
		if name == "" || name == "." {
			name = filepath.Base(clean)
		}
		if existing, ok := seen[root]; ok {
			if clean == filepath.Clean(currentPath) {
				existing.Path = clean
				seen[root] = existing
			}
			return
		}
		seen[root] = worktreeRepoChoice{
			Path: clean,
			Root: root,
			Name: name,
		}
	}

	addChoice(currentPath)
	for path := range asmtmux.ListActiveSessions() {
		addChoice(path)
	}

	choices := make([]worktreeRepoChoice, 0, len(seen))
	for _, choice := range seen {
		choices = append(choices, choice)
	}
	sort.Slice(choices, func(i, j int) bool {
		left := strings.ToLower(choices[i].Name)
		right := strings.ToLower(choices[j].Name)
		if left != right {
			return left < right
		}
		return choices[i].Root < choices[j].Root
	})
	return choices
}

func currentRepoIndex(choices []worktreeRepoChoice, currentPath string) int {
	currentRoot, _ := config.ProjectIdentity(filepath.Clean(currentPath))
	if currentRoot == "" || currentRoot == "." {
		currentRoot = filepath.Clean(currentPath)
	}
	for i, choice := range choices {
		if choice.Root == currentRoot {
			return i
		}
	}
	return 0
}

func (m *WorktreeDialogModel) loadBranches(dirPath string) tea.Cmd {
	t := m.tracker
	return func() tea.Msg {
		// Refresh remotes before listing so newly-pushed branches appear
		// without the user having to fetch in a shell first. Best-effort:
		// offline / auth failure / slow remote shouldn't block opening
		// the dialog — we fall through to the cached branch list.
		_ = worktree.FetchAllRemotes(dirPath)
		branches, err := worktree.ListBranches(dirPath)
		if err != nil {
			return BranchesLoadedMsg{Err: err}
		}
		repoName := worktree.RepoName(dirPath)
		if repoName == "" {
			_, repoName = config.ProjectIdentity(dirPath)
		}
		var cached map[string]tracker.TaskInfo
		if peeker, ok := t.(tracker.Peeker); ok {
			cached = make(map[string]tracker.TaskInfo, len(branches))
			for _, b := range branches {
				if info, ok := peeker.Peek(b.Name); ok {
					cached[b.Name] = info
				}
			}
		}
		return BranchesLoadedMsg{
			Branches:    branches,
			RepoDir:     dirPath,
			RepoName:    repoName,
			CachedTasks: cached,
		}
	}
}

func (m *WorktreeDialogModel) switchRepo(delta int) tea.Cmd {
	if len(m.repoChoices) <= 1 {
		return nil
	}
	m.repoIndex = (m.repoIndex + delta + len(m.repoChoices)) % len(m.repoChoices)
	choice := m.repoChoices[m.repoIndex]
	m.mode = wtModeSelectBranch
	m.filter = ""
	m.cursor = 0
	m.scrollOffset = 0
	m.newBranchName = ""
	m.baseBranch = ""
	m.rootPath = choice.Path
	m.dirPath = choice.Path
	m.repoDir = choice.Path
	m.repoName = choice.Name
	m.err = ""
	m.branches = nil
	m.filtered = nil
	m.taskInfos = make(map[string]tracker.TaskInfo)
	return m.loadBranches(choice.Path)
}

func (m WorktreeDialogModel) Update(msg tea.Msg) (WorktreeDialogModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case BranchesLoadedMsg:
		if m.dirPath != "" && msg.RepoDir != "" && filepath.Clean(msg.RepoDir) != filepath.Clean(m.dirPath) {
			return m, nil
		}
		if msg.Err != nil {
			m.err = msg.Err.Error()
			return m, nil
		}
		m.branches = msg.Branches
		m.repoDir = msg.RepoDir
		m.repoName = msg.RepoName
		// Seed cached task infos before applyFilter so search-by-task works
		// immediately and the first render is fully populated.
		for name, info := range msg.CachedTasks {
			m.taskInfos[name] = info
		}
		m.applyFilter()
		if m.tracker != nil {
			var cmds []tea.Cmd
			for _, b := range m.branches {
				// Skip fetches for branches already resolved from cache.
				if _, ok := m.taskInfos[b.Name]; ok {
					continue
				}
				cmds = append(cmds, m.fetchTaskName(m.repoDir, b.Name))
			}
			cmds = append(cmds, wtTaskRetryTickCmd(5))
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case wtTaskResolvedMsg:
		if m.repoDir != "" && msg.RepoDir != "" && filepath.Clean(msg.RepoDir) != filepath.Clean(m.repoDir) {
			return m, nil
		}
		if msg.Info.Name != "" {
			m.taskInfos[msg.Branch] = msg.Info
			// Propagate to branches sharing the same base name (e.g., origin/feature/X → feature/X)
			for _, b := range m.branches {
				if _, ok := m.taskInfos[b.Name]; ok {
					continue
				}
				if strings.HasSuffix(msg.Branch, b.Name) || strings.HasSuffix(b.Name, msg.Branch) {
					m.taskInfos[b.Name] = msg.Info
				}
			}
		}
		return m, nil

	case wtTaskRetryTickMsg:
		if m.tracker == nil || msg.remaining <= 0 {
			return m, nil
		}
		var cmds []tea.Cmd
		for _, b := range m.branches {
			if _, ok := m.taskInfos[b.Name]; !ok {
				// Check if another branch with overlapping name already resolved
				base := strings.TrimPrefix(b.Name, "origin/")
				if base != b.Name {
					if info, ok := m.taskInfos[base]; ok {
						m.taskInfos[b.Name] = info
						continue
					}
				}
				cmds = append(cmds, m.fetchTaskName(m.repoDir, b.Name))
			}
		}
		if len(cmds) > 0 {
			cmds = append(cmds, wtTaskRetryTickCmd(msg.remaining-1))
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == wtModeNewBranch {
			return m.handleNewBranchKey(msg)
		}
		if m.mode == wtModeSelectBase {
			return m.handleSelectBaseKey(msg)
		}
		return m.handleSelectBranchKey(msg)
	}

	return m, nil
}

func (m WorktreeDialogModel) handleSelectBranchKey(msg tea.KeyMsg) (WorktreeDialogModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
			return m, nil
		}
		m.Hide()
		return m, func() tea.Msg { return WorktreeCancelledMsg{} }

	case "up":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
		}

	case "down":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.adjustScrollOffset()
		}

	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		selected := m.filtered[m.cursor]
		repoDir := m.repoDir
		rootPath := m.rootPath
		repoName := m.repoName
		m.Hide()
		return m, createWorktreeFromBranchCmd(repoDir, rootPath, repoName, selected.Name)

	case "tab":
		return m, m.switchRepo(+1)

	case "ctrl+n", "ctrl+shift+n", "f10":
		m.mode = wtModeSelectBase
		m.filter = ""
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilter()

	case "backspace":
		if m.filter != "" {
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.applyFilter()
		}

	case "ctrl+u":
		m.filter = ""
		m.applyFilter()

	default:
		// Accept typed runes including non-ASCII (e.g. Korean 가, Japanese あ).
		// Named keys like "up"/"enter" don't reach here — they're matched above.
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.applyFilter()
		} else if msg.Type == tea.KeySpace {
			m.filter += " "
			m.applyFilter()
		}
	}

	return m, nil
}

func (m WorktreeDialogModel) handleSelectBaseKey(msg tea.KeyMsg) (WorktreeDialogModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.mode = wtModeSelectBranch
		m.filter = ""
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilter()

	case "tab":
		return m, m.switchRepo(+1)

	case "up":
		if m.cursor > 0 {
			m.cursor--
			m.adjustScrollOffset()
		}

	case "down":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.adjustScrollOffset()
		}

	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		m.baseBranch = m.filtered[m.cursor].Name
		m.newBranchName = ""
		m.mode = wtModeNewBranch

	case "backspace":
		if m.filter != "" {
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.applyFilter()
		}

	case "ctrl+u":
		m.filter = ""
		m.applyFilter()

	default:
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.applyFilter()
		} else if msg.Type == tea.KeySpace {
			m.filter += " "
			m.applyFilter()
		}
	}

	return m, nil
}

func (m WorktreeDialogModel) handleNewBranchKey(msg tea.KeyMsg) (WorktreeDialogModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.mode = wtModeSelectBranch
		m.newBranchName = ""

	case "tab":
		return m, m.switchRepo(+1)

	case "enter":
		name := strings.TrimSpace(m.newBranchName)
		if name == "" {
			return m, nil
		}
		repoDir := m.repoDir
		rootPath := m.rootPath
		repoName := m.repoName
		baseBranch := m.baseBranch
		m.Hide()
		return m, createWorktreeNewBranchCmd(repoDir, rootPath, repoName, name, baseBranch)

	case "backspace":
		if m.newBranchName != "" {
			runes := []rune(m.newBranchName)
			m.newBranchName = string(runes[:len(runes)-1])
		}

	case "ctrl+u":
		m.newBranchName = ""

	default:
		if msg.Type == tea.KeyRunes {
			m.newBranchName += string(msg.Runes)
		} else if msg.Type == tea.KeySpace {
			m.newBranchName += " "
		}
	}

	return m, nil
}

func wtTaskRetryTickCmd(remaining int) tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return wtTaskRetryTickMsg{remaining: remaining}
	})
}

func (m WorktreeDialogModel) fetchTaskName(repoDir, branch string) tea.Cmd {
	t := m.tracker
	return func() tea.Msg {
		info := t.Resolve(branch)
		return wtTaskResolvedMsg{RepoDir: repoDir, Branch: branch, Info: info}
	}
}

func createWorktreeFromBranchCmd(repoDir, rootPath, repoName, branch string) tea.Cmd {
	return func() tea.Msg {
		folderName := worktree.BranchToFolderName(branch)
		targetPath, err := prepareWorktreeTarget(rootPath, repoName, folderName)
		if err != nil {
			return WorktreeErrorMsg{Err: err.Error()}
		}
		if err := worktree.CreateWorktreeFromBranch(repoDir, targetPath, branch); err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("worktree add failed: %v", err)}
		}
		copied, warnings := applyTemplateBestEffort(rootPath, repoName, targetPath)
		return WorktreeCreatedMsg{
			Name:             folderName,
			Path:             targetPath,
			TemplateCopied:   copied,
			TemplateWarnings: warnings,
		}
	}
}

func createWorktreeNewBranchCmd(repoDir, rootPath, repoName, newBranch, baseBranch string) tea.Cmd {
	return func() tea.Msg {
		folderName := worktree.BranchToFolderName(newBranch)
		targetPath, err := prepareWorktreeTarget(rootPath, repoName, folderName)
		if err != nil {
			return WorktreeErrorMsg{Err: err.Error()}
		}
		if err := worktree.CreateWorktreeNewBranch(repoDir, targetPath, newBranch, baseBranch); err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("worktree add failed: %v", err)}
		}
		copied, warnings := applyTemplateBestEffort(rootPath, repoName, targetPath)
		return WorktreeCreatedMsg{
			Name:             folderName,
			Path:             targetPath,
			TemplateCopied:   copied,
			TemplateWarnings: warnings,
		}
	}
}

// prepareWorktreeTarget resolves the base directory for the new worktree and
// makes sure it exists on disk. `git worktree add` requires the target's
// parent to exist, so we MkdirAll before returning. Returned targetPath is
// base/folderName with the folder itself intentionally NOT created (git
// creates it).
func prepareWorktreeTarget(rootPath, repoName, folderName string) (string, error) {
	base := resolveWorktreeBase(rootPath, repoName)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s failed: %v", base, err)
	}
	return filepath.Join(base, folderName), nil
}

// resolveWorktreeBase decides where to create a new worktree. The picker only
// opens this dialog in repo mode (F7 is gated), so rootPath is guaranteed to
// be a git working tree — main repo or linked worktree.
//
// Resolution order:
//  1. Parent directory of the most-recently-modified linked worktree (by mtime).
//     Matches whatever layout the user is already using. The auto-seed on
//     first repo entry also writes this into project config, so repeat runs
//     usually hit step 2 with the seeded value — step 1 still wins here for
//     robustness if the config was cleared or the layout drifted.
//  2. Config's worktree_base_path, with `{repo}` expanded to repoName and a
//     built-in default of `~/worktrees/{repo}` when unset. Grouping by repo
//     avoids collisions when multiple repos share the base.
//  3. Parent directory of the main repo — unreachable when #2 resolves, which
//     it always does (default is never empty). Kept as safety net for the
//     case where the config read fails AND home dir can't be resolved.
//  4. rootPath — last-resort safety net.
func resolveWorktreeBase(rootPath, repoName string) string {
	if parent := worktree.MostRecentLinkedWorktreeParent(rootPath); parent != "" {
		return parent
	}
	cfg, err := config.LoadMerged(rootPath)
	if err == nil {
		if p := cfg.GetWorktreeBasePath(repoName); p != "" {
			return p
		}
	}
	if mainRepo, err := worktree.FindMainRepo(rootPath); err == nil && mainRepo != "" {
		return filepath.Dir(mainRepo)
	}
	return rootPath
}

// applyTemplateBestEffort copies files from the per-repo template directory
// into the new worktree. Any failure is surfaced as a warning — template copy
// never aborts worktree creation.
func applyTemplateBestEffort(rootPath, repoName, targetPath string) (int, []string) {
	cfg, err := config.LoadMerged(rootPath)
	if err != nil || cfg == nil {
		cfg = config.DefaultConfig()
	}
	policy := worktree.ConflictPolicy(cfg.TemplateConflictPolicy())
	res, err := worktree.ApplyTemplate(config.ProjectRoot(rootPath), repoName, targetPath, policy)
	warnings := res.Warnings
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("template copy error: %v", err))
	}
	return res.Copied, warnings
}

// WorktreeRunnerModel wraps WorktreeDialogModel for standalone use in the working panel.
type WorktreeRunnerModel struct {
	dialog  WorktreeDialogModel
	initCmd tea.Cmd
	Created bool
	err     string
	width   int
	height  int
}

func NewWorktreeRunnerModel(rootPath, dirPath string, t tracker.Tracker) WorktreeRunnerModel {
	d := NewWorktreeDialogModel(t)
	cmd := d.Show(rootPath, dirPath)
	return WorktreeRunnerModel{dialog: d, initCmd: cmd}
}

func (m WorktreeRunnerModel) Init() tea.Cmd {
	return m.initCmd
}

func (m WorktreeRunnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dialog.SetSize(msg.Width, msg.Height)
		return m, nil
	case WorktreeCreatedMsg:
		m.Created = true
		// Forward template-copy result to the parent picker via tmux session
		// options. The picker reads these when it handles worktreeExitedMsg.
		// Errors from tmux calls are intentionally ignored — template copy
		// feedback is best-effort.
		_ = asmtmux.SetSessionOption("asm-created-worktree-path", msg.Path)
		_ = asmtmux.SetSessionOption("asm-worktree-copied", fmt.Sprintf("%d", msg.TemplateCopied))
		_ = asmtmux.SetSessionOption("asm-worktree-warnings", strings.Join(msg.TemplateWarnings, "\n"))
		return m, tea.Quit
	case WorktreeCancelledMsg:
		return m, tea.Quit
	case WorktreeErrorMsg:
		m.err = msg.Err
		return m, nil
	case tea.KeyMsg:
		if m.err != "" {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.dialog, cmd = m.dialog.Update(msg)
	return m, cmd
}

func (m WorktreeRunnerModel) View() string {
	if m.err != "" {
		title := renderDialogTitle("Error", dangerColor)

		body := lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("255")).
			Render(m.err)

		inlineHint := statusBarStyle.Render("Press any key to dismiss")
		content := padToHeight(title+"\n\n"+body+"\n\n"+inlineHint, m.height-3)
		statusBar := renderDialogHintBar(m.width, " Press any key to close")
		return content + "\n" + statusBar
	}
	return m.dialog.View()
}

// View

func (m WorktreeDialogModel) View() string {
	if !m.visible {
		return ""
	}
	if m.mode == wtModeNewBranch {
		return m.viewNewBranch()
	}
	if m.mode == wtModeSelectBase {
		return m.viewSelectBase()
	}
	return m.viewSelectBranch()
}

func (m WorktreeDialogModel) repoLine() string {
	name := m.repoName
	if name == "" {
		name = filepath.Base(m.dirPath)
	}
	if len(m.repoChoices) > 1 {
		name = fmt.Sprintf("%s (%d/%d)", name, m.repoIndex+1, len(m.repoChoices))
	}
	return lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render("repo: ") +
		lipgloss.NewStyle().Foreground(secondaryColor).Render(name)
}

func (m WorktreeDialogModel) renderFullScreen(title, subtitle, filterLine string, rows []string, hint string) string {
	titleStr := renderDialogTitle(title, primaryColor)

	repo := m.repoLine()

	content := titleStr + "\n" + repo
	if subtitle != "" {
		content += "\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render(subtitle)
	}

	filterStr := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.NewStyle().Foreground(dimColor).Render("/ ") + filterLine +
			lipgloss.NewStyle().Foreground(primaryColor).Render("▎"),
	)

	content += "\n\n" + filterStr + "\n\n"
	for _, row := range rows {
		content += "  " + row + "\n"
	}

	content = padToHeight(content, m.height-3)
	statusBar := renderDialogHintBar(m.width, " "+hint)
	return content + "\n" + statusBar
}

func (m WorktreeDialogModel) viewSelectBranch() string {
	var rows []string
	if m.err != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(dangerColor).Render(m.err))
	} else if len(m.branches) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("Loading branches..."))
	} else if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("No matching branches"))
	} else {
		if m.scrollOffset > 0 {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↑ more"))
		}

		usedLines := 0
		end := m.scrollOffset
		for end < len(m.filtered) {
			lines := 1
			if info, ok := m.taskInfos[m.filtered[end].Name]; ok && info.Name != "" {
				lines = 2
			}
			if usedLines+lines > m.maxVisible {
				break
			}
			usedLines += lines
			end++
		}

		for i := m.scrollOffset; i < end; i++ {
			b := m.filtered[i]
			cursor := "  "
			style := normalItemStyle
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
				style = selectedItemStyle
			}

			name := style.Render(b.Name)
			if b.HasWorktree {
				name += " " + lipgloss.NewStyle().Foreground(activeColor).Render("●")
			}
			if b.IsLocal {
				name += " " + lipgloss.NewStyle().Foreground(dimColor).Render("(local)")
			}

			rows = append(rows, cursor+name)
			if info, ok := m.taskInfos[b.Name]; ok && info.Name != "" {
				rows = append(rows, "    "+lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render(info.Name))
			}
		}

		if end < len(m.filtered) {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↓ more"))
		}
	}

	return m.renderFullScreen("Create Worktree", "", m.filter, rows,
		"↑↓: navigate  Tab: repo  Enter: checkout  ^n: new branch  Esc: cancel")
}

func (m WorktreeDialogModel) viewSelectBase() string {
	var rows []string
	if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("No matching branches"))
	} else {
		if m.scrollOffset > 0 {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↑ more"))
		}

		usedLines := 0
		end := m.scrollOffset
		for end < len(m.filtered) {
			lines := 1
			if info, ok := m.taskInfos[m.filtered[end].Name]; ok && info.Name != "" {
				lines = 2
			}
			if usedLines+lines > m.maxVisible {
				break
			}
			usedLines += lines
			end++
		}

		for i := m.scrollOffset; i < end; i++ {
			b := m.filtered[i]
			cursor := "  "
			style := normalItemStyle
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
				style = selectedItemStyle
			}
			name := style.Render(b.Name)
			if b.IsLocal {
				name += " " + lipgloss.NewStyle().Foreground(dimColor).Render("(local)")
			}
			rows = append(rows, cursor+name)
			if info, ok := m.taskInfos[b.Name]; ok && info.Name != "" {
				rows = append(rows, "    "+lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render(info.Name))
			}
		}

		if end < len(m.filtered) {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↓ more"))
		}
	}

	return m.renderFullScreen("New Branch",
		"Select a base branch to create a new branch from",
		m.filter, rows,
		"↑↓: navigate  Tab: repo  Enter: select base  Esc: back")
}

func (m WorktreeDialogModel) viewNewBranch() string {
	titleStr := renderDialogTitle("New Branch", primaryColor)

	repo := m.repoLine()

	baseLine := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.NewStyle().Foreground(dimColor).Render("Base: ") +
			lipgloss.NewStyle().Foreground(whiteColor).Render(m.baseBranch),
	)

	nameInput := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.NewStyle().Foreground(dimColor).Render("Name: ") +
			m.newBranchName +
			lipgloss.NewStyle().Foreground(primaryColor).Render("▎"),
	)

	content := padToHeight(
		titleStr+"\n"+repo+"\n\n"+baseLine+"\n\n"+nameInput,
		m.height-3,
	)
	statusBar := renderDialogHintBar(m.width, " Tab: repo  Enter: create  Esc: back")
	return content + "\n" + statusBar
}
