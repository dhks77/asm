package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/csm/claude"
	"github.com/nhn/csm/config"
	"github.com/nhn/csm/integration"
	"github.com/nhn/csm/notification"
	csmtmux "github.com/nhn/csm/tmux"
	"github.com/nhn/csm/worktree"
)

// Messages
type DirectoriesScannedMsg struct {
	Directories []worktree.Worktree
}

type GitStatusUpdatedMsg struct {
	Path   string
	Status worktree.GitStatus
}

type TaskNameResolvedMsg struct {
	Path string
	Name string
}

type tickMsg time.Time

type claudeStateTickMsg time.Time

type ClaudeStateUpdatedMsg struct {
	Name  string
	State claude.State
}

type spinnerTickMsg time.Time
type scrollTickMsg time.Time

type sessionExitedMsg struct {
	DirName string
}

type flashExpiredMsg struct {
	DirName   string
	StartedAt time.Time
}

type batchKillCompletedMsg struct{ count int }
type batchDeleteCompletedMsg struct{ count int }

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type PickerModel struct {
	cfg            *config.Config
	rootPath       string
	directories    []worktree.Worktree
	gitStatus      map[string]worktree.GitStatus
	taskNames      map[string]string
	claudeStates      map[string]claude.State
	prevClaudeStates  map[string]claude.State
	sessionStartTimes map[string]time.Time
	flashItems        map[string]time.Time
	spinnerFrame      int
	scrollTick     int
	cursor         int
	viewTop        int    // first visible item index for scrolling
	workingDir     string // directory shown in working panel (Claude session)
	termDir        string // directory shown in working panel (terminal)
	dooray         *integration.DoorayClient
	focused        bool
	width          int
	height         int
	ready          bool
	err            string
	searchQuery    string
	selectedItems  map[string]bool
	batchConfirm   BatchConfirmModel
}

func NewPickerModel(cfg *config.Config, rootPath string) PickerModel {
	var dooray *integration.DoorayClient
	projectSettings, err := config.LoadProjectSettings(rootPath)
	if err == nil {
		dooray = integration.NewDoorayClient(projectSettings.Dooray)
	}
	return PickerModel{
		cfg:            cfg,
		rootPath:       rootPath,
		gitStatus:      make(map[string]worktree.GitStatus),
		taskNames:      make(map[string]string),
		claudeStates:      make(map[string]claude.State),
		prevClaudeStates:  make(map[string]claude.State),
		sessionStartTimes: make(map[string]time.Time),
		flashItems:        make(map[string]time.Time),
		selectedItems:  make(map[string]bool),
		batchConfirm:   NewBatchConfirmModel(),
		dooray:            dooray,
		focused:        true,
	}
}

// filteredDirectories returns indices into m.directories matching the current search query.
func (m *PickerModel) filteredDirectories() []int {
	if m.searchQuery == "" {
		indices := make([]int, len(m.directories))
		for i := range m.directories {
			indices[i] = i
		}
		return indices
	}

	query := strings.ToLower(m.searchQuery)
	var indices []int
	for i, wt := range m.directories {
		if strings.Contains(strings.ToLower(wt.Name), query) {
			indices = append(indices, i)
			continue
		}
		if taskName, ok := m.taskNames[wt.Path]; ok && taskName != "" {
			if strings.Contains(strings.ToLower(taskName), query) {
				indices = append(indices, i)
				continue
			}
		}
		if gs, ok := m.gitStatus[wt.Path]; ok && gs.Branch != "" {
			if strings.Contains(strings.ToLower(gs.Branch), query) {
				indices = append(indices, i)
				continue
			}
		}
	}
	return indices
}

func (m PickerModel) Init() tea.Cmd {
	return tea.Batch(m.scanDirectories(), tickCmd(), claudeStateTickCmd(), spinnerTickCmd(), scrollTickCmd())
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to batch confirm dialog when visible
	if m.batchConfirm.IsVisible() {
		switch msg.(type) {
		case tea.WindowSizeMsg:
			// fall through to main handler
		default:
			var cmd tea.Cmd
			m.batchConfirm, cmd = m.batchConfirm.Update(msg)
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.batchConfirm.SetSize(msg.Width)
		m.ready = true
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, nil

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case tickMsg:
		var cmds []tea.Cmd
		for _, wt := range m.directories {
			cmds = append(cmds, m.fetchGitStatus(wt.Path))
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case claudeStateTickMsg:
		var cmds []tea.Cmd
		activeWindows := csmtmux.ListDirectoryWindows()
		activeSet := make(map[string]bool)
		for _, name := range activeWindows {
			activeSet[name] = true
		}
		for _, wt := range m.directories {
			if activeSet[wt.Name] {
				if _, tracked := m.sessionStartTimes[wt.Name]; !tracked {
					m.sessionStartTimes[wt.Name] = time.Now()
				}
				cmds = append(cmds, m.fetchClaudeState(wt.Name))
			}
		}
		cmds = append(cmds, claudeStateTickCmd())
		return m, tea.Batch(cmds...)

	case ClaudeStateUpdatedMsg:
		if msg.State != claude.StateUnknown {
			prevState := m.prevClaudeStates[msg.Name]
			m.claudeStates[msg.Name] = msg.State
			m.prevClaudeStates[msg.Name] = msg.State

			if prevState.IsBusy() && msg.State == claude.StateIdle {
				now := time.Now()
				m.flashItems[msg.Name] = now
				var cmds []tea.Cmd
				cmds = append(cmds, flashExpireCmd(msg.Name, now, 3*time.Second))
				if m.cfg.IsDesktopNotificationsEnabled() {
					cmds = append(cmds, notifyCompletionCmd(msg.Name))
				}
				return m, tea.Batch(cmds...)
			}
		}
		return m, nil

	case flashExpiredMsg:
		if startedAt, ok := m.flashItems[msg.DirName]; ok {
			if startedAt.Equal(msg.StartedAt) {
				delete(m.flashItems, msg.DirName)
			}
		}
		return m, nil

	case spinnerTickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTickCmd()

	case scrollTickMsg:
		m.scrollTick++
		return m, scrollTickCmd()

	case sessionExitedMsg:
		delete(m.claudeStates, msg.DirName)
		delete(m.prevClaudeStates, msg.DirName)
		delete(m.sessionStartTimes, msg.DirName)
		delete(m.flashItems, msg.DirName)
		isDisplayed := m.workingDir == msg.DirName
		csmtmux.CleanupExitedWindow(msg.DirName, isDisplayed)
		if isDisplayed {
			m.workingDir = ""
			// Show terminal for this directory if it exists
			termWin := csmtmux.TerminalWindowName(msg.DirName)
			if csmtmux.WindowExists(termWin) {
				csmtmux.SwapTermToWorkingPanel(msg.DirName)
				m.termDir = msg.DirName
			} else {
				csmtmux.FocusPickingPanel()
			}
		}
		return m, nil

	case worktreeExitedMsg:
		if msg.created {
			return m, m.scanDirectories()
		}
		return m, nil

	case DirectoryRemovedMsg:
		return m, m.scanDirectories()

	case WorktreeErrorMsg:
		m.err = msg.Err
		return m, nil

	case deleteExitedMsg:
		if !msg.confirmed {
			return m, nil
		}
		var wt *worktree.Worktree
		for i := range m.directories {
			if m.directories[i].Name == msg.dirName {
				wt = &m.directories[i]
				break
			}
		}
		if wt == nil {
			return m, nil
		}
		delete(m.claudeStates, wt.Name)
		delete(m.sessionStartTimes, wt.Name)
		winName := csmtmux.WindowName(wt.Name)
		if csmtmux.WindowExists(winName) {
			csmtmux.KillDirectoryWindow(wt.Name)
		}
		termWinName := csmtmux.TerminalWindowName(wt.Name)
		if csmtmux.WindowExists(termWinName) {
			csmtmux.KillTerminalWindow(wt.Name)
		}
		return m, m.removeDirectory(wt)

	case BatchConfirmedMsg:
		m.clearSelection()
		switch msg.Action {
		case BatchKillSessions:
			return m, m.batchKillSessions(msg.Items)
		case BatchDeleteWorktrees:
			return m, m.batchDeleteWorktrees(msg.Items)
		}
		return m, nil

	case BatchCancelledMsg:
		return m, nil

	case batchKillCompletedMsg:
		return m, nil

	case batchDeleteCompletedMsg:
		return m, m.scanDirectories()

	case tea.KeyMsg:
		if m.err != "" {
			m.err = ""
			return m, nil
		}
		return m.handleKey(msg)

	case DirectoriesScannedMsg:
		m.directories = msg.Directories
		// Prune stale selections
		validNames := make(map[string]bool)
		for _, wt := range msg.Directories {
			validNames[wt.Name] = true
		}
		for name := range m.selectedItems {
			if !validNames[name] {
				delete(m.selectedItems, name)
			}
		}
		filtered := m.filteredDirectories()
		if m.cursor >= len(filtered) {
			m.cursor = max(0, len(filtered)-1)
		}
		m.viewTop = 0
		var cmds []tea.Cmd
		for _, wt := range msg.Directories {
			cmds = append(cmds, m.fetchGitStatus(wt.Path))
		}
		return m, tea.Batch(cmds...)

	case GitStatusUpdatedMsg:
		m.gitStatus[msg.Path] = msg.Status
		if m.dooray != nil && msg.Status.Branch != "" {
			if _, ok := m.taskNames[msg.Path]; !ok {
				return m, m.fetchTaskName(msg.Path, msg.Status.Branch)
			}
		}
		return m, nil

	case TaskNameResolvedMsg:
		if msg.Name != "" {
			m.taskNames[msg.Path] = msg.Name
		}
		return m, nil

	case terminalExitedMsg:
		isDisplayed := m.termDir == msg.dirName
		if isDisplayed {
			csmtmux.SwapTermBackFromWorkingPanel(msg.dirName)
			m.termDir = ""
		}
		csmtmux.KillTerminalWindow(msg.dirName)
		if isDisplayed {
			// Show Claude session for this directory if it exists
			winName := csmtmux.WindowName(msg.dirName)
			if csmtmux.WindowExists(winName) {
				csmtmux.SwapToWorkingPanel(msg.dirName)
				m.workingDir = msg.dirName
			} else {
				csmtmux.FocusPickingPanel()
			}
		}
		return m, nil

	case settingsExitedMsg:
		m.reloadDoorayClient()
		if m.dooray != nil {
			var cmds []tea.Cmd
			for _, wt := range m.directories {
				if gs, ok := m.gitStatus[wt.Path]; ok && gs.Branch != "" {
					cmds = append(cmds, m.fetchTaskName(wt.Path, gs.Branch))
				}
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil
	}

	return m, nil
}

func (m PickerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	filtered := m.filteredDirectories()

	switch key {
	case "ctrl+c":
		csmtmux.KillSession()
		return m, tea.Quit

	case "up":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.viewTop {
				m.viewTop = m.cursor
			}
		}
	case "down":
		if m.cursor < len(filtered)-1 {
			m.cursor++
			m.adjustViewTop()
		}

	case "enter":
		wt := m.selectedDirectory()
		if wt == nil {
			return m, nil
		}
		winName := csmtmux.WindowName(wt.Name)
		if csmtmux.WindowExists(winName) {
			m.showInWorkingPanel(wt)
		} else {
			return m, m.startSession(wt)
		}

	case "f12": // Ctrl+t: toggle terminal
		if m.workingDir != "" || m.termDir != "" {
			return m, m.toggleTerminal()
		}
		wt := m.selectedDirectory()
		if wt == nil {
			return m, nil
		}
		return m, m.switchToTerminal(wt)

	case "f11": // Ctrl+g: focus working panel or start Claude
		if m.workingDir != "" || m.termDir != "" {
			csmtmux.FocusWorkingPanel()
		} else {
			wt := m.selectedDirectory()
			if wt == nil {
				return m, nil
			}
			return m, m.startSession(wt)
		}

	case "f10": // Ctrl+n: new Claude session
		wt := m.contextDirectory()
		if wt == nil {
			return m, nil
		}
		delete(m.claudeStates, wt.Name)
		delete(m.sessionStartTimes, wt.Name)
		winName := csmtmux.WindowName(wt.Name)
		if csmtmux.WindowExists(winName) {
			if m.workingDir == wt.Name {
				csmtmux.SwapBackFromWorkingPanel(wt.Name)
				m.workingDir = ""
			}
			csmtmux.KillDirectoryWindow(wt.Name)
		}
		return m, m.startSession(wt)

	case "f9": // Ctrl+s: settings
		return m, m.openSettings()

	case "f8": // Ctrl+q: quit
		csmtmux.KillSession()
		return m, tea.Quit

	case "f7": // Ctrl+w: create worktree
		dir := m.selectedDirectory()
		if dir == nil {
			return m, nil
		}
		return m, m.openWorktreeDialog(dir)

	case "f6": // Ctrl+d: delete active directory
		wt := m.contextDirectory()
		if wt == nil {
			return m, nil
		}
		return m, m.openDelete(wt)

	case "f5": // Ctrl+x: toggle selection
		wt := m.selectedDirectory()
		if wt != nil {
			if m.selectedItems[wt.Name] {
				delete(m.selectedItems, wt.Name)
			} else {
				m.selectedItems[wt.Name] = true
			}
		}

	case "backspace":
		if len(m.searchQuery) > 0 {
			runes := []rune(m.searchQuery)
			m.searchQuery = string(runes[:len(runes)-1])
			m.cursor = 0
			m.viewTop = 0
		}

	case "esc":
		if len(m.selectedItems) > 0 {
			m.clearSelection()
		} else if m.searchQuery != "" {
			m.searchQuery = ""
			m.cursor = 0
			m.viewTop = 0
		}

	default:
		// Batch action keys (only active when items are selected)
		if len(m.selectedItems) > 0 && msg.Type == tea.KeyRunes {
			switch string(msg.Runes) {
			case "k":
				return m, m.openBatchKill()
			case "x":
				return m, m.openBatchDelete()
			}
		}
		if msg.Type == tea.KeyRunes {
			m.searchQuery += string(msg.Runes)
			m.cursor = 0
			m.viewTop = 0
		} else if msg.Type == tea.KeySpace {
			m.searchQuery += " "
			m.cursor = 0
			m.viewTop = 0
		}
	}

	return m, nil
}

func (m *PickerModel) startSession(wt *worktree.Worktree) tea.Cmd {
	claudePath := m.cfg.ResolveClaudePath()
	csmtmux.CreateDirectoryWindow(wt.Name, wt.Path, claudePath, m.cfg.ClaudeArgs)
	m.sessionStartTimes[wt.Name] = time.Now()
	m.showInWorkingPanel(wt)
	return waitForExitCmd(wt.Name)
}

func (m *PickerModel) clearSelection() {
	m.selectedItems = make(map[string]bool)
}

func (m *PickerModel) selectedItemNames() []string {
	var names []string
	for name := range m.selectedItems {
		names = append(names, name)
	}
	return names
}

func (m *PickerModel) openBatchKill() tea.Cmd {
	names := m.selectedItemNames()
	m.batchConfirm.Show(BatchKillSessions, names, 0)
	return nil
}

func (m *PickerModel) openBatchDelete() tea.Cmd {
	names := m.selectedItemNames()
	dirtyCount := 0
	for _, name := range names {
		for _, wt := range m.directories {
			if wt.Name == name {
				if worktree.HasChanges(wt.Path) {
					dirtyCount++
				}
				break
			}
		}
	}
	m.batchConfirm.Show(BatchDeleteWorktrees, names, dirtyCount)
	return nil
}

func (m *PickerModel) batchKillSessions(names []string) tea.Cmd {
	return func() tea.Msg {
		count := 0
		for _, name := range names {
			winName := csmtmux.WindowName(name)
			if csmtmux.WindowExists(winName) {
				csmtmux.KillDirectoryWindow(name)
				count++
			}
		}
		return batchKillCompletedMsg{count: count}
	}
}

func (m *PickerModel) batchDeleteWorktrees(names []string) tea.Cmd {
	// Kill active sessions and collect worktrees to remove
	for _, name := range names {
		delete(m.claudeStates, name)
		delete(m.prevClaudeStates, name)
		delete(m.sessionStartTimes, name)
		delete(m.flashItems, name)

		winName := csmtmux.WindowName(name)
		if csmtmux.WindowExists(winName) {
			if m.workingDir == name {
				csmtmux.SwapBackFromWorkingPanel(name)
				m.workingDir = ""
			}
			csmtmux.KillDirectoryWindow(name)
		}

		termWinName := csmtmux.TerminalWindowName(name)
		if csmtmux.WindowExists(termWinName) {
			if m.termDir == name {
				csmtmux.SwapTermBackFromWorkingPanel(name)
				m.termDir = ""
			}
			csmtmux.KillTerminalWindow(name)
		}
	}

	var toRemove []worktree.Worktree
	for _, name := range names {
		for _, wt := range m.directories {
			if wt.Name == name {
				toRemove = append(toRemove, wt)
				break
			}
		}
	}

	return func() tea.Msg {
		count := 0
		for _, wt := range toRemove {
			if worktree.IsWorktree(wt.Path) {
				mainRepo, err := worktree.FindMainRepo(wt.Path)
				if err == nil {
					if err := worktree.RemoveWorktree(mainRepo, wt.Path, false); err != nil {
						worktree.RemoveWorktree(mainRepo, wt.Path, true)
					}
					count++
					continue
				}
			}
			if err := os.RemoveAll(wt.Path); err == nil {
				count++
			}
		}
		return batchDeleteCompletedMsg{count: count}
	}
}

type DirectoryRemovedMsg struct{}

func (m *PickerModel) removeDirectory(dir *worktree.Worktree) tea.Cmd {
	dirPath := dir.Path
	return func() tea.Msg {
		if worktree.IsWorktree(dirPath) {
			mainRepo, err := worktree.FindMainRepo(dirPath)
			if err == nil {
				if err := worktree.RemoveWorktree(mainRepo, dirPath, false); err != nil {
					worktree.RemoveWorktree(mainRepo, dirPath, true)
				}
				return DirectoryRemovedMsg{}
			}
		}
		if err := os.RemoveAll(dirPath); err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("remove failed: %v", err)}
		}
		return DirectoryRemovedMsg{}
	}
}

// waitForExitCmd returns a tea.Cmd that blocks until claude exits in the directory.
func waitForExitCmd(dirName string) tea.Cmd {
	return func() tea.Msg {
		csmtmux.WaitForExit(dirName)
		return sessionExitedMsg{DirName: dirName}
	}
}

type settingsExitedMsg struct{}
type worktreeExitedMsg struct {
	created bool
}
type terminalExitedMsg struct {
	dirName string
}
type deleteExitedMsg struct {
	dirName   string
	confirmed bool
}

func (m *PickerModel) swapOutWorkingPanel() {
	if m.workingDir != "" {
		csmtmux.SwapBackFromWorkingPanel(m.workingDir)
		m.workingDir = ""
	}
	if m.termDir != "" {
		csmtmux.SwapTermBackFromWorkingPanel(m.termDir)
		m.termDir = ""
	}
}

func (m *PickerModel) openSettings() tea.Cmd {
	m.swapOutWorkingPanel()

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	cmd := fmt.Sprintf("%s --settings --path %s", exe, m.rootPath)
	csmtmux.RunInWorkingPanel("csm-settings", cmd)
	csmtmux.FocusWorkingPanel()

	return func() tea.Msg {
		csmtmux.WaitAndCleanupWorkingPanel("csm-settings")
		return settingsExitedMsg{}
	}
}

func (m *PickerModel) openWorktreeDialog(dir *worktree.Worktree) tea.Cmd {
	m.swapOutWorkingPanel()

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	cmd := fmt.Sprintf("%s --worktree-create --path %s --worktree-dir %s", exe, m.rootPath, dir.Path)
	csmtmux.RunInWorkingPanel("csm-worktree", cmd)
	csmtmux.FocusWorkingPanel()

	return func() tea.Msg {
		exitCode := csmtmux.WaitAndCleanupWorkingPanel("csm-worktree")
		return worktreeExitedMsg{created: exitCode == 0}
	}
}

func (m *PickerModel) openDelete(wt *worktree.Worktree) tea.Cmd {
	m.swapOutWorkingPanel()

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	taskName := ""
	if tn, ok := m.taskNames[wt.Path]; ok {
		taskName = tn
	}

	dirty := worktree.HasChanges(wt.Path)
	isWt := worktree.IsWorktree(wt.Path)

	cmd := fmt.Sprintf("%s --delete %s", exe, wt.Name)
	if taskName != "" {
		cmd += fmt.Sprintf(" --delete-task '%s'", taskName)
	}
	if dirty {
		cmd += " --delete-dirty"
	}
	if isWt {
		cmd += " --delete-worktree"
	}
	csmtmux.RunInWorkingPanel("csm-delete", cmd)
	csmtmux.FocusWorkingPanel()

	wtName := wt.Name
	return func() tea.Msg {
		exitCode := csmtmux.WaitAndCleanupWorkingPanel("csm-delete")
		return deleteExitedMsg{dirName: wtName, confirmed: exitCode == 0}
	}
}

func (m *PickerModel) switchToTerminal(wt *worktree.Worktree) tea.Cmd {
	// Already showing this terminal
	if m.termDir == wt.Name {
		csmtmux.FocusWorkingPanel()
		return nil
	}

	// Swap out whatever is in the working panel
	m.swapOutWorkingPanel()

	// Create terminal if needed
	var cmd tea.Cmd
	termWin := csmtmux.TerminalWindowName(wt.Name)
	if !csmtmux.WindowExists(termWin) {
		csmtmux.CreateTerminalWindow(wt.Name, wt.Path)
		cmd = waitForTermExitCmd(wt.Name)
	}

	csmtmux.SwapTermToWorkingPanel(wt.Name)
	m.termDir = wt.Name
	csmtmux.FocusWorkingPanel()
	return cmd
}

func (m *PickerModel) toggleTerminal() tea.Cmd {
	if m.workingDir != "" {
		// Claude is displayed → switch to terminal
		wtName := m.workingDir
		var wtPath string
		for _, wt := range m.directories {
			if wt.Name == wtName {
				wtPath = wt.Path
				break
			}
		}
		if wtPath == "" {
			return nil
		}
		csmtmux.SwapBackFromWorkingPanel(wtName)
		m.workingDir = ""

		var cmd tea.Cmd
		termWin := csmtmux.TerminalWindowName(wtName)
		if !csmtmux.WindowExists(termWin) {
			csmtmux.CreateTerminalWindow(wtName, wtPath)
			cmd = waitForTermExitCmd(wtName)
		}
		csmtmux.SwapTermToWorkingPanel(wtName)
		m.termDir = wtName
		csmtmux.FocusWorkingPanel()
		return cmd
	} else if m.termDir != "" {
		// Terminal is displayed → switch to Claude (if exists)
		wtName := m.termDir
		csmtmux.SwapTermBackFromWorkingPanel(wtName)
		m.termDir = ""

		winName := csmtmux.WindowName(wtName)
		if csmtmux.WindowExists(winName) {
			csmtmux.SwapToWorkingPanel(wtName)
			m.workingDir = wtName
			csmtmux.FocusWorkingPanel()
		}
	}
	return nil
}

func waitForTermExitCmd(dirName string) tea.Cmd {
	return func() tea.Msg {
		exec.Command("tmux", "wait-for", csmtmux.TermExitSignalName(dirName)).Run()
		return terminalExitedMsg{dirName: dirName}
	}
}

func (m *PickerModel) reloadDoorayClient() {
	projectSettings, err := config.LoadProjectSettings(m.rootPath)
	if err != nil {
		return
	}
	m.dooray = integration.NewDoorayClient(projectSettings.Dooray)
	m.taskNames = make(map[string]string)
}

func (m *PickerModel) showInWorkingPanel(wt *worktree.Worktree) {
	if m.workingDir == wt.Name {
		csmtmux.FocusWorkingPanel()
		return
	}
	if m.workingDir != "" {
		csmtmux.SwapBackFromWorkingPanel(m.workingDir)
	}
	if m.termDir != "" {
		csmtmux.SwapTermBackFromWorkingPanel(m.termDir)
		m.termDir = ""
	}
	csmtmux.SwapToWorkingPanel(wt.Name)
	m.workingDir = wt.Name
	csmtmux.FocusWorkingPanel()
}

func (m *PickerModel) itemHeight(wi int) int {
	h := 1
	_, hasTask := m.taskNames[m.directories[wi].Path]
	hasTask = hasTask && m.taskNames[m.directories[wi].Path] != ""
	_, hasBranch := m.gitStatus[m.directories[wi].Path]
	if hasTask {
		h += 3
	} else if hasBranch {
		h += 2
	} else {
		h += 1
	}
	return h
}

// adjustViewTop scrolls the viewport down so the cursor item is fully visible.
func (m *PickerModel) adjustViewTop() {
	if m.height == 0 {
		return
	}
	filtered := m.filteredDirectories()
	maxListLines := m.height - 4
	if m.searchQuery != "" {
		maxListLines-- // search bar takes one line
	}
	if maxListLines < 1 {
		maxListLines = 1
	}

	// Count lines from viewTop to cursor (inclusive)
	linesUsed := 0
	for fi := m.viewTop; fi <= m.cursor && fi < len(filtered); fi++ {
		linesUsed += m.itemHeight(filtered[fi])
	}

	for linesUsed > maxListLines && m.viewTop < m.cursor {
		linesUsed -= m.itemHeight(filtered[m.viewTop])
		m.viewTop++
	}
}

func (m *PickerModel) selectedDirectory() *worktree.Worktree {
	filtered := m.filteredDirectories()
	if len(filtered) == 0 || m.cursor >= len(filtered) {
		return nil
	}
	return &m.directories[filtered[m.cursor]]
}

// contextDirectory returns the directory currently displayed in working panel,
// falling back to the cursor selection.
func (m *PickerModel) contextDirectory() *worktree.Worktree {
	wtName := m.workingDir
	if wtName == "" {
		wtName = m.termDir
	}
	if wtName != "" {
		for i := range m.directories {
			if m.directories[i].Name == wtName {
				return &m.directories[i]
			}
		}
	}
	return m.selectedDirectory()
}

func (m PickerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	var title string
	if m.focused {
		title = headerStyle.Render(filepath.Base(m.rootPath))
	} else {
		title = lipgloss.NewStyle().Foreground(dimColor).Padding(0, 1).Render(filepath.Base(m.rootPath))
	}

	activeWindows := csmtmux.ListDirectoryWindows()
	activeSet := make(map[string]bool)
	for _, name := range activeWindows {
		activeSet[name] = true
	}

	filtered := m.filteredDirectories()

	// Render all filtered items and count lines per item
	type renderedItem struct {
		text      string
		lineCount int
	}
	items := make([]renderedItem, len(filtered))
	for fi, wi := range filtered {
		wt := m.directories[wi]
		text := m.renderItem(fi, wt, activeSet[wt.Name])
		items[fi] = renderedItem{text: text, lineCount: strings.Count(text, "\n") + 1}
	}

	// Available lines for the list (height - title - status bar - margin)
	maxListLines := m.height - 4
	if m.searchQuery != "" {
		maxListLines-- // search bar takes one line
	}
	if maxListLines < 1 {
		maxListLines = 1
	}

	// Build visible list with viewport scrolling
	var visibleRows []string
	usedLines := 0
	for i := m.viewTop; i < len(items); i++ {
		if usedLines+items[i].lineCount > maxListLines {
			break
		}
		visibleRows = append(visibleRows, items[i].text)
		usedLines += items[i].lineCount
	}

	// Build view: title (fixed) + search bar + list + padding + status bar
	var viewLines []string
	viewLines = append(viewLines, title)
	if m.searchQuery != "" {
		searchLine := lipgloss.NewStyle().Foreground(primaryColor).Padding(0, 1).Render("/ " + m.searchQuery)
		viewLines = append(viewLines, searchLine)
	}
	for _, row := range visibleRows {
		viewLines = append(viewLines, strings.Split(row, "\n")...)
	}
	targetLines := m.height - 3
	statusBar := RenderStatusBar(m.width, m.focused, len(m.selectedItems))
	for len(viewLines) < targetLines {
		viewLines = append(viewLines, "")
	}
	if len(viewLines) > targetLines {
		viewLines = viewLines[:targetLines]
	}
	viewLines = append(viewLines, statusBar)
	view := strings.Join(viewLines, "\n")

	if m.err != "" {
		errDialog := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 2).
			Width(min(50, m.width-4)).
			Render(
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Error") + "\n\n" +
					lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(m.err) + "\n\n" +
					statusBarStyle.Render("Press any key to dismiss"))
		view = m.overlayCenter(view, errDialog)
	}

	if m.batchConfirm.IsVisible() {
		view = m.overlayCenter(view, m.batchConfirm.View())
	}

	return view
}

func (m PickerModel) renderItem(index int, wt worktree.Worktree, hasSession bool) string {
	isSelected := index == m.cursor

	dimmed := !m.focused && !isSelected

	indicator := inactiveSessionStyle.String()
	if hasSession {
		indicator = activeSessionStyle.String()
	}
	if m.workingDir == wt.Name || m.termDir == wt.Name {
		indicator = lipgloss.NewStyle().Foreground(activeColor).Bold(true).Render("●")
	}
	if isSelected {
		indicator = lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("●")
	}
	if dimmed {
		if hasSession || m.workingDir == wt.Name || m.termDir == wt.Name {
			indicator = lipgloss.NewStyle().Foreground(dimColor).Render("●")
		} else {
			indicator = lipgloss.NewStyle().Foreground(dimColor).Render("○")
		}
	}

	// Line 1: task name or branch or folder name
	var primaryName string
	var subLines []string

	taskName, hasTask := m.taskNames[wt.Path]
	hasTask = hasTask && taskName != ""
	gs, hasBranch := m.gitStatus[wt.Path]

	primaryStyle := taskNameStyle
	if isSelected {
		primaryStyle = lipgloss.NewStyle().Foreground(primaryColor).Bold(true)
	}
	if dimmed {
		primaryStyle = lipgloss.NewStyle().Foreground(dimColor)
	}

	// Selection checkbox (only shown when in selection mode)
	var checkbox string
	inSelectionMode := len(m.selectedItems) > 0
	if inSelectionMode {
		if m.selectedItems[wt.Name] {
			checkbox = lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("◆") + " "
		} else {
			checkbox = lipgloss.NewStyle().Foreground(dimColor).Render("◇") + " "
		}
	}

	// Calculate available width for primary name
	prefixWidth := 2 // indicator(1) + space(1)
	if inSelectionMode {
		prefixWidth += 2 // checkbox(1) + space(1)
	}
	maxNameWidth := m.width - prefixWidth

	var rawName string
	if hasTask {
		rawName = taskName
		subLines = append(subLines, normalItemStyle.Render(wt.Name))
		if hasBranch {
			subLines = append(subLines, gitStatusStyle.Render(gs.Summary()))
		}
	} else if hasBranch {
		rawName = wt.Name
		subLines = append(subLines, gitStatusStyle.Render(gs.Summary()))
	} else {
		rawName = wt.Name
	}

	// State as sub-line (prepend to subLines)
	var stateLine string
	if hasSession {
		if _, flashing := m.flashItems[wt.Name]; flashing {
			if m.spinnerFrame%4 < 2 {
				stateLine = CompletionFlashStyle.Render("✓ done!")
			} else {
				stateLine = IdleStateStyle.Render("idle")
			}
		} else if state, ok := m.claudeStates[wt.Name]; ok {
			stateLine = renderClaudeState(state, m.spinnerFrame)
		}
		if startTime, ok := m.sessionStartTimes[wt.Name]; ok {
			elapsed := formatElapsed(time.Since(startTime))
			badge := ElapsedTimeStyle.Render(elapsed)
			if stateLine != "" {
				stateLine = stateLine + " " + badge
			} else {
				stateLine = badge
			}
		}
	} else {
		stateLine = ClosedStateStyle.Render("closed")
	}
	if stateLine != "" {
		subLines = append([]string{stateLine}, subLines...)
	}

	displayName := scrollText(rawName, maxNameWidth, m.scrollTick)
	primaryName = primaryStyle.Render(displayName)

	line1 := fmt.Sprintf("%s%s %s", checkbox, indicator, primaryName)

	barPad := "  "
	if inSelectionMode {
		barPad = "    "
	}
	bar := barPad
	if isSelected {
		bar = barPad[:len(barPad)-2] + lipgloss.NewStyle().Foreground(primaryColor).Render("▎") + " "
	}

	result := line1
	for _, sub := range subLines {
		result += "\n" + fmt.Sprintf("%s%s", bar, sub)
	}

	return result
}

func (m PickerModel) overlayCenter(base, overlay string) string {
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
	)
}

// Commands

func (m PickerModel) scanDirectories() tea.Cmd {
	return func() tea.Msg {
		wts, _ := worktree.Scan(m.rootPath)
		return DirectoriesScannedMsg{Directories: wts}
	}
}

func (m PickerModel) fetchGitStatus(path string) tea.Cmd {
	return func() tea.Msg {
		gs := worktree.GetGitStatus(path)
		return GitStatusUpdatedMsg{Path: path, Status: gs}
	}
}

func (m PickerModel) fetchTaskName(path string, branch string) tea.Cmd {
	dooray := m.dooray
	return func() tea.Msg {
		name := dooray.ResolveTaskName(branch)
		return TaskNameResolvedMsg{Path: path, Name: name}
	}
}

func flashExpireCmd(dirName string, startedAt time.Time, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(t time.Time) tea.Msg {
		return flashExpiredMsg{DirName: dirName, StartedAt: startedAt}
	})
}

func notifyCompletionCmd(dirName string) tea.Cmd {
	return func() tea.Msg {
		notification.Send("CSM", dirName+" session is idle")
		return nil
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*5, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func claudeStateTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return claudeStateTickMsg(t)
	})
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func scrollTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return scrollTickMsg(t)
	})
}

func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func scrollText(text string, maxWidth int, tick int) string {
	runes := []rune(text)
	if maxWidth <= 0 {
		return text
	}

	// Check visual width (CJK chars take 2 columns)
	textWidth := lipgloss.Width(text)
	if textWidth <= maxWidth {
		return text
	}

	// Build a list of rune visual widths
	runeWidths := make([]int, len(runes))
	for i, r := range runes {
		runeWidths[i] = lipgloss.Width(string(r))
	}

	// Count how many scroll steps are needed
	// Each step drops one rune from the left
	maxOffset := 0
	for off := 1; off <= len(runes); off++ {
		// Width from runes[off:]
		w := 0
		for j := off; j < len(runes); j++ {
			w += runeWidths[j]
		}
		if w <= maxWidth {
			maxOffset = off
			break
		}
		maxOffset = off
	}

	if maxOffset == 0 {
		return text
	}

	pauseTicks := 4 // ~0.8s pause
	totalTicks := pauseTicks + maxOffset + pauseTicks

	phase := tick % totalTicks

	var offset int
	if phase < pauseTicks {
		offset = 0
	} else if phase < pauseTicks+maxOffset {
		offset = phase - pauseTicks
	} else {
		offset = maxOffset
	}

	// Truncate from offset to fit maxWidth
	w := 0
	end := offset
	for end < len(runes) && w+runeWidths[end] <= maxWidth {
		w += runeWidths[end]
		end++
	}
	return string(runes[offset:end])
}

func (m PickerModel) fetchClaudeState(dirName string) tea.Cmd {
	currentWT := m.workingDir
	return func() tea.Msg {
		isDisplayed := currentWT == dirName
		title, err := csmtmux.GetPaneTitle(dirName, isDisplayed)
		if err != nil {
			return ClaudeStateUpdatedMsg{Name: dirName, State: claude.StateUnknown}
		}
		state := claude.DetectStateFromTitle(title)

		// If busy, refine with pane content for detail (thinking/tool/responding)
		if state == claude.StateBusy {
			content, err := csmtmux.CapturePaneContent(dirName, isDisplayed)
			if err == nil {
				detail := claude.DetectBusyDetail(content)
				if detail != claude.StateBusy {
					state = detail
				}
			}
		}

		return ClaudeStateUpdatedMsg{Name: dirName, State: state}
	}
}

func renderClaudeState(state claude.State, frame int) string {
	if state == claude.StateIdle {
		return IdleStateStyle.Render(state.Label())
	}
	if !state.IsBusy() {
		return ""
	}

	spinner := spinnerFrames[frame%len(spinnerFrames)]
	var style lipgloss.Style
	switch state {
	case claude.StateThinking:
		style = ThinkingStateStyle
	case claude.StateToolUse:
		style = ToolUseStateStyle
	case claude.StateResponding:
		style = RespondingStateStyle
	default:
		style = BusyStateStyle
	}
	return style.Render(spinner + " " + state.Label())
}

