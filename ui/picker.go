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
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/notification"
	"github.com/nhn/asm/provider"
	"github.com/nhn/asm/tracker"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/worktree"
)

// Messages
type DirectoriesScannedMsg struct {
	Directories []worktree.Worktree
	// CachedTasks seeds m.taskInfos on the first frame so task names don't
	// trickle in over multiple renders. Keyed by worktree path.
	CachedTasks map[string]tracker.TaskInfo
	// CachedBranches records the branch each cached entry was observed
	// under; used to invalidate stale names once GitStatus arrives.
	CachedBranches map[string]string
}

type GitStatusUpdatedMsg struct {
	Path   string
	Status worktree.GitStatus
}

type TaskResolvedMsg struct {
	Path   string
	Branch string
	Info   tracker.TaskInfo
}

type tickMsg time.Time

type providerStateTickMsg time.Time

type ProviderStateUpdatedMsg struct {
	Name  string
	State provider.State
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
	taskInfos      map[string]tracker.TaskInfo
	providerStates     map[string]provider.State
	prevProviderStates map[string]provider.State
	worktreeProviders  map[string]string // worktree name -> provider name
	registry           *provider.Registry
	sessionStartTimes  map[string]time.Time
	terminalStartTimes map[string]time.Time
	flashItems         map[string]time.Time
	spinnerFrame       int
	scrollTick     int
	cursor         int
	viewTop        int    // first visible item index for scrolling
	workingDir     string // directory shown in working panel (AI session)
	termDir        string // directory shown in working panel (terminal)
	tracker        tracker.Tracker
	taskCache      *tracker.PathCache
	// cachedBranches tracks the branch each seeded taskInfo was observed
	// under; we invalidate the seed when GitStatus reveals a different branch.
	cachedBranches map[string]string
	focused        bool
	width          int
	height         int
	ready          bool
	err            string
	searchQuery    string
	selectedItems    map[string]bool
	batchConfirm     BatchConfirmModel

	// Top status bar (summary of all active sessions)
	lastStatusSummary string
	statusBarEnabled  bool
}

func NewPickerModel(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker, taskCache *tracker.PathCache) PickerModel {
	return PickerModel{
		cfg:            cfg,
		rootPath:       rootPath,
		gitStatus:      make(map[string]worktree.GitStatus),
		taskInfos:      make(map[string]tracker.TaskInfo),
		providerStates:     make(map[string]provider.State),
		prevProviderStates: make(map[string]provider.State),
		worktreeProviders:  make(map[string]string),
		registry:           registry,
		sessionStartTimes:  make(map[string]time.Time),
		terminalStartTimes: make(map[string]time.Time),
		flashItems:         make(map[string]time.Time),
		selectedItems:  make(map[string]bool),
		batchConfirm:     NewBatchConfirmModel(),
		tracker:           t,
		taskCache:        taskCache,
		cachedBranches:    make(map[string]string),
		focused:        true,
	}
}

// filteredDirectories returns indices into m.directories matching the current search query.
// Active sessions (AI or terminal window open) are sorted first, preserving original order within each group.
func (m *PickerModel) filteredDirectories() []int {
	var matched []int
	if m.searchQuery == "" {
		matched = make([]int, len(m.directories))
		for i := range m.directories {
			matched[i] = i
		}
	} else {
		query := strings.ToLower(m.searchQuery)
		for i, wt := range m.directories {
			if strings.Contains(strings.ToLower(wt.Name), query) {
				matched = append(matched, i)
				continue
			}
			if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" {
				if strings.Contains(strings.ToLower(info.Name), query) {
					matched = append(matched, i)
					continue
				}
			}
			if gs, ok := m.gitStatus[wt.Path]; ok && gs.Branch != "" {
				if strings.Contains(strings.ToLower(gs.Branch), query) {
					matched = append(matched, i)
					continue
				}
			}
		}
	}

	// Partition: active sessions (AI or terminal) first, inactive after. Preserves internal order.
	activeKinds := asmtmux.ListActiveSessions()
	var active, inactive []int
	for _, i := range matched {
		if activeKinds[m.directories[i].Name] != 0 {
			active = append(active, i)
		} else {
			inactive = append(inactive, i)
		}
	}
	return append(active, inactive...)
}

func (m PickerModel) Init() tea.Cmd {
	return tea.Batch(m.scanDirectories(), tickCmd(), providerStateTickCmd(), spinnerTickCmd(), scrollTickCmd())
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
		m.batchConfirm.SetSize(msg.Width, msg.Height)
		firstReady := !m.ready
		m.ready = true
		// On initial render, fullscreen the picker so the right-hand working
		// pane isn't visible in auto_zoom mode.
		if firstReady {
			m.applyAutoZoomPicker()
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		// Close any utility dialogs when picker gets focus
		if asmtmux.HasUtilityWindow() {
			asmtmux.CloseUtilityPanel()
		}
		// Whenever picker regains focus (Ctrl+G from working, dialog exit,
		// session exit, etc.), re-apply picker fullscreen if auto_zoom is on.
		m.applyAutoZoomPicker()
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

	case providerStateTickMsg:
		// Remember the worktree under cursor so sort changes don't shift selection.
		cursorWT := ""
		if wt := m.selectedDirectory(); wt != nil {
			cursorWT = wt.Name
		}

		var cmds []tea.Cmd
		activeKinds := asmtmux.ListActiveSessions()
		for _, wt := range m.directories {
			kind := activeKinds[wt.Name]
			if kind.HasAI() {
				if _, tracked := m.sessionStartTimes[wt.Name]; !tracked {
					m.sessionStartTimes[wt.Name] = time.Now()
				}
				// Recover provider info from tmux if not tracked
				if _, known := m.worktreeProviders[wt.Name]; !known {
					stored := asmtmux.GetWindowOption(wt.Name, "asm-provider")
					if stored != "" {
						m.worktreeProviders[wt.Name] = stored
					} else {
						m.worktreeProviders[wt.Name] = m.registry.Default().Name()
					}
				}
				cmds = append(cmds, m.fetchProviderState(wt.Name))
			}
			if kind.HasTerm() {
				if _, tracked := m.terminalStartTimes[wt.Name]; !tracked {
					m.terminalStartTimes[wt.Name] = time.Now()
				}
			}
		}
		m.stabilizeCursor(cursorWT)
		cmds = append(cmds, providerStateTickCmd())
		return m, tea.Batch(cmds...)

	case ProviderStateUpdatedMsg:
		if msg.State != provider.StateUnknown {
			prevState := m.prevProviderStates[msg.Name]
			m.providerStates[msg.Name] = msg.State
			m.prevProviderStates[msg.Name] = msg.State

			if prevState.IsBusy() && msg.State == provider.StateIdle {
				now := time.Now()
				m.flashItems[msg.Name] = now
				var cmds []tea.Cmd
				cmds = append(cmds, flashExpireCmd(msg.Name, now, 3*time.Second))
				if m.cfg.IsDesktopNotificationsEnabled() {
					displayName := msg.Name
					for _, wt := range m.directories {
						if wt.Name == msg.Name {
							if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" {
								displayName = info.Name
							}
							break
						}
					}
					cmds = append(cmds, notifyCompletionCmd(msg.Name, displayName, m.workingDir))
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
		m.refreshStatusSummary()
		return m, scrollTickCmd()

	case sessionExitedMsg:
		m.cleanupSessionState(msg.DirName)
		isDisplayed := m.workingDir == msg.DirName
		asmtmux.CleanupExitedWindow(msg.DirName, isDisplayed)
		if isDisplayed {
			m.workingDir = ""
			// Show terminal for this directory if it exists
			termWin := asmtmux.TerminalWindowName(msg.DirName)
			if asmtmux.WindowExists(termWin) {
				asmtmux.SwapTermToWorkingPanel(msg.DirName)
				m.termDir = msg.DirName
			} else {
				asmtmux.FocusPickingPanel()
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
		m.cleanupSessionState(wt.Name)
		winName := asmtmux.WindowName(wt.Name)
		if asmtmux.WindowExists(winName) {
			asmtmux.KillDirectoryWindow(wt.Name)
		}
		termWinName := asmtmux.TerminalWindowName(wt.Name)
		if asmtmux.WindowExists(termWinName) {
			asmtmux.KillTerminalWindow(wt.Name)
		}
		delete(m.terminalStartTimes, wt.Name)
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

	case providerSelectDoneMsg:
		if msg.ProviderName == "" {
			return m, nil
		}
		wt := m.contextDirectory()
		if wt == nil {
			return m, nil
		}
		// Kill existing session if any, then start with selected provider
		winName := asmtmux.WindowName(wt.Name)
		if asmtmux.WindowExists(winName) {
			if m.workingDir == wt.Name {
				asmtmux.SwapBackFromWorkingPanel(wt.Name)
				m.workingDir = ""
			}
			asmtmux.KillDirectoryWindow(wt.Name)
		}
		m.cleanupSessionState(wt.Name)
		return m, m.startSession(wt, msg.ProviderName)

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
		// Seed taskInfos from the persistent cache so the first render has
		// task names filled in. Any entry with a branch mismatch when git
		// status arrives will be invalidated by GitStatusUpdatedMsg below.
		validPaths := make(map[string]bool, len(msg.Directories))
		for _, wt := range msg.Directories {
			validPaths[wt.Path] = true
		}
		for p, info := range msg.CachedTasks {
			if !validPaths[p] {
				continue
			}
			if _, exists := m.taskInfos[p]; !exists {
				m.taskInfos[p] = info
			}
		}
		for p, b := range msg.CachedBranches {
			if validPaths[p] {
				m.cachedBranches[p] = b
			}
		}
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
		if m.tracker != nil && msg.Status.Branch != "" {
			// Invalidate a seeded cache entry whose branch no longer matches
			// what's actually checked out — otherwise a stale task name
			// would stick until TTL expiry.
			if seeded, ok := m.cachedBranches[msg.Path]; ok && seeded != msg.Status.Branch {
				delete(m.taskInfos, msg.Path)
				delete(m.cachedBranches, msg.Path)
				if m.taskCache != nil {
					m.taskCache.Delete(msg.Path)
				}
			}
			if _, ok := m.taskInfos[msg.Path]; !ok {
				return m, m.fetchTaskName(msg.Path, msg.Status.Branch)
			}
		}
		return m, nil

	case TaskResolvedMsg:
		if msg.Info.Name != "" {
			m.taskInfos[msg.Path] = msg.Info
			if msg.Branch != "" {
				m.cachedBranches[msg.Path] = msg.Branch
				if m.taskCache != nil {
					m.taskCache.Set(msg.Path, msg.Branch, msg.Info)
				}
			}
		}
		return m, nil

	case settingsExitedMsg:
		// Reload merged config (user + project) to pick up changed defaults
		if newCfg, err := config.LoadMerged(m.rootPath); err == nil {
			m.cfg = newCfg
			defaultName := newCfg.DefaultProvider
			if defaultName == "" {
				defaultName = provider.DefaultProviderName
			}
			m.registry.SetDefault(defaultName)
			// Apply picker width immediately — only if not zoomed (resize is
			// ignored on zoomed panes and would confuse the state otherwise).
			if !asmtmux.IsWorkingPanelZoomed() {
				asmtmux.ResizePickerPanel(newCfg.GetPickerWidth())
			}
		}
		return m, nil

	case terminalExitedMsg:
		isDisplayed := m.termDir == msg.dirName
		if isDisplayed {
			asmtmux.SwapTermBackFromWorkingPanel(msg.dirName)
			m.termDir = ""
		}
		asmtmux.KillTerminalWindow(msg.dirName)
		delete(m.terminalStartTimes, msg.dirName)
		if isDisplayed {
			// If this directory also has an AI session, show it fullscreen in
			// the working pane instead of leaving the user with a split view
			// where the picker has focus and the working pane sits idle on
			// the right.
			winName := asmtmux.WindowName(msg.dirName)
			if asmtmux.WindowExists(winName) {
				asmtmux.SwapToWorkingPanel(msg.dirName)
				m.workingDir = msg.dirName
				asmtmux.FocusWorkingPanel()
				m.applyAutoZoom()
			} else {
				asmtmux.FocusPickingPanel()
			}
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
		asmtmux.KillSession()
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
		winName := asmtmux.WindowName(wt.Name)
		if asmtmux.WindowExists(winName) {
			m.showInWorkingPanel(wt)
		} else {
			return m, m.startSession(wt, m.registry.Default().Name())
		}

	case "f12": // Ctrl+t: open / focus / toggle terminal.
		// Source-pane aware:
		//   - picker pane: target = cursor's worktree
		//   - working pane: target = whatever session owns the working panel
		// When target == current working panel session → toggle AI↔Term.
		// Otherwise → switch the working panel to target's terminal.
		if asmtmux.ActivePaneIndex() == 1 {
			// From the working pane: act on the session currently fronted.
			if m.workingDir != "" || m.termDir != "" {
				return m, m.toggleTerminal()
			}
			// Nothing in working panel — fall through to cursor behavior.
		}
		wt := m.selectedDirectory()
		if wt == nil {
			return m, nil
		}
		if m.workingDir == wt.Name || m.termDir == wt.Name {
			return m, m.toggleTerminal()
		}
		return m, m.switchToTerminal(wt)

	case "f11": // Ctrl+g: act on cursor dir — swap/focus existing session, or start new
		// Close any open utility dialogs (settings, worktree, delete, provider-select)
		if asmtmux.HasUtilityWindow() {
			asmtmux.CloseUtilityPanel()
			return m, nil
		}

		wt := m.selectedDirectory()
		if wt == nil {
			return m, nil
		}
		winName := asmtmux.WindowName(wt.Name)
		if asmtmux.WindowExists(winName) {
			// Session exists: showInWorkingPanel handles both cases —
			// if already fronted, it just focuses + zooms; otherwise it
			// swaps the cursor's session into the working pane.
			m.showInWorkingPanel(wt)
		} else {
			return m, m.startSession(wt, m.registry.Default().Name())
		}

	case "f10": // Ctrl+n: new AI session
		wt := m.contextDirectory()
		if wt == nil {
			return m, nil
		}
		// Restart with the same provider, or default if none
		providerName := m.worktreeProviders[wt.Name]
		if providerName == "" {
			providerName = m.registry.Default().Name()
		}
		m.cleanupSessionState(wt.Name)
		winName := asmtmux.WindowName(wt.Name)
		if asmtmux.WindowExists(winName) {
			if m.workingDir == wt.Name {
				asmtmux.SwapBackFromWorkingPanel(wt.Name)
				m.workingDir = ""
			}
			asmtmux.KillDirectoryWindow(wt.Name)
		}
		return m, m.startSession(wt, providerName)

	case "f9": // Ctrl+s: settings
		return m, m.openSettings()

	case "f8": // Ctrl+q: quit
		asmtmux.KillSession()
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

	case "f4": // Ctrl+p: select AI provider
		if m.registry.Count() > 1 {
			return m, m.openProviderSelect()
		}

	case "f5": // Ctrl+x: toggle selection
		wt := m.selectedDirectory()
		if wt != nil {
			if m.selectedItems[wt.Name] {
				delete(m.selectedItems, wt.Name)
			} else {
				m.selectedItems[wt.Name] = true
			}
		}

	case "f1": // Ctrl+] : rotate to next active session (cyclic)
		return m, m.rotateSession(+1)

	case "f3", "o": // Ctrl+o / o: Open task URL in browser
		if key == "o" && len(m.selectedItems) > 0 {
			break // fall through to search in selection mode
		}
		wt := m.contextDirectory()
		if wt != nil {
			if info, ok := m.taskInfos[wt.Path]; ok && info.URL != "" {
				exec.Command("open", info.URL).Start()
			}
		}
		return m, nil

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

func (m *PickerModel) startSession(wt *worktree.Worktree, providerName string) tea.Cmd {
	p := m.registry.Get(providerName)
	if p == nil {
		p = m.registry.Default()
	}
	asmtmux.CreateDirectoryWindow(wt.Name, wt.Path, p.Command(), p.Args())
	asmtmux.SetWindowOption(wt.Name, "asm-provider", p.Name())
	m.worktreeProviders[wt.Name] = p.Name()
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
	m.batchConfirm.Show(BatchKillSessions, names, m.taskNamesFor(names), 0)
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
	m.batchConfirm.Show(BatchDeleteWorktrees, names, m.taskNamesFor(names), dirtyCount)
	return nil
}

// taskNamesFor returns a parallel slice of resolved task names for the
// given worktree names (empty string when no info is cached). Used by the
// batch-confirm dialog so users see task titles, not just folder names.
func (m *PickerModel) taskNamesFor(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		for _, wt := range m.directories {
			if wt.Name == name {
				if info, ok := m.taskInfos[wt.Path]; ok {
					out[i] = info.Name
				}
				break
			}
		}
	}
	return out
}

func (m *PickerModel) batchKillSessions(names []string) tea.Cmd {
	return func() tea.Msg {
		count := 0
		for _, name := range names {
			winName := asmtmux.WindowName(name)
			if asmtmux.WindowExists(winName) {
				asmtmux.KillDirectoryWindow(name)
				count++
			}
		}
		return batchKillCompletedMsg{count: count}
	}
}

func (m *PickerModel) batchDeleteWorktrees(names []string) tea.Cmd {
	// Kill active sessions and collect worktrees to remove
	for _, name := range names {
		m.cleanupSessionState(name)

		winName := asmtmux.WindowName(name)
		if asmtmux.WindowExists(winName) {
			if m.workingDir == name {
				asmtmux.SwapBackFromWorkingPanel(name)
				m.workingDir = ""
			}
			asmtmux.KillDirectoryWindow(name)
		}

		termWinName := asmtmux.TerminalWindowName(name)
		if asmtmux.WindowExists(termWinName) {
			if m.termDir == name {
				asmtmux.SwapTermBackFromWorkingPanel(name)
				m.termDir = ""
			}
			asmtmux.KillTerminalWindow(name)
		}
		delete(m.terminalStartTimes, name)
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

// waitForExitCmd returns a tea.Cmd that blocks until the AI process exits in the directory.
func waitForExitCmd(dirName string) tea.Cmd {
	return func() tea.Msg {
		asmtmux.WaitForExit(dirName)
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

// cleanupSessionState removes per-session bookkeeping for the given worktree
// name. Called whenever a session ends or is about to be restarted so stale
// state doesn't leak into the next session (e.g. frozen provider state,
// leftover "done!" flash, old start time).
func (m *PickerModel) cleanupSessionState(name string) {
	delete(m.providerStates, name)
	delete(m.prevProviderStates, name)
	delete(m.worktreeProviders, name)
	delete(m.sessionStartTimes, name)
	delete(m.flashItems, name)
}

func (m *PickerModel) swapOutWorkingPanel() {
	if m.workingDir != "" {
		asmtmux.SwapBackFromWorkingPanel(m.workingDir)
		m.workingDir = ""
	}
	if m.termDir != "" {
		asmtmux.SwapTermBackFromWorkingPanel(m.termDir)
		m.termDir = ""
	}
}

// runDialogInWorkingPanel is the shared boilerplate for every modal dialog
// that runs as a child `asm` invocation inside the working pane. It swaps any
// current session out, spawns the dialog, focuses + zooms the working pane,
// and returns a tea.Cmd that blocks on dialog exit and converts the exit
// code into the caller-supplied message.
//
// cmdFlags is the argv portion appended to the executable path (e.g.
// "--settings" or "--delete foo --delete-dirty"). resultMsg is invoked with
// the dialog's exit code once it terminates.
func (m *PickerModel) runDialogInWorkingPanel(windowName, cmdFlags string, resultMsg func(exitCode int) tea.Msg) tea.Cmd {
	m.swapOutWorkingPanel()

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	asmtmux.RunInWorkingPanel(windowName, fmt.Sprintf("%s %s", exe, cmdFlags))
	asmtmux.FocusWorkingPanel()
	m.applyAutoZoom()

	return func() tea.Msg {
		return resultMsg(asmtmux.WaitAndCleanupWorkingPanel(windowName))
	}
}

func (m *PickerModel) openProviderSelect() tea.Cmd {
	return m.runDialogInWorkingPanel("asm-provider-select", "--provider-select", func(exitCode int) tea.Msg {
		if exitCode == 0 {
			return providerSelectDoneMsg{ProviderName: asmtmux.GetSessionOption("asm-selected-provider")}
		}
		return providerSelectDoneMsg{}
	})
}

func (m *PickerModel) openSettings() tea.Cmd {
	return m.runDialogInWorkingPanel("asm-settings", "--settings", func(int) tea.Msg {
		return settingsExitedMsg{}
	})
}

func (m *PickerModel) openWorktreeDialog(dir *worktree.Worktree) tea.Cmd {
	flags := fmt.Sprintf("--worktree-create --path %s --worktree-dir %s", m.rootPath, dir.Path)
	return m.runDialogInWorkingPanel("asm-worktree", flags, func(exitCode int) tea.Msg {
		return worktreeExitedMsg{created: exitCode == 0}
	})
}

func (m *PickerModel) openDelete(wt *worktree.Worktree) tea.Cmd {
	taskName := ""
	if info, ok := m.taskInfos[wt.Path]; ok {
		taskName = info.Name
	}

	flags := fmt.Sprintf("--delete %s", wt.Name)
	if taskName != "" {
		flags += fmt.Sprintf(" --delete-task '%s'", taskName)
	}
	if worktree.HasChanges(wt.Path) {
		flags += " --delete-dirty"
	}
	if worktree.IsWorktree(wt.Path) {
		flags += " --delete-worktree"
	}

	wtName := wt.Name
	return m.runDialogInWorkingPanel("asm-delete", flags, func(exitCode int) tea.Msg {
		return deleteExitedMsg{dirName: wtName, confirmed: exitCode == 0}
	})
}

// showTerminalInWorkingPanel lazily creates the terminal window for `name`,
// swaps it into the working pane, sets m.termDir, and focuses + zooms.
// Returns a tea.Cmd that watches for the terminal's exit (nil if the
// terminal window already existed — its watcher is already running).
func (m *PickerModel) showTerminalInWorkingPanel(name, path string) tea.Cmd {
	var cmd tea.Cmd
	if !asmtmux.WindowExists(asmtmux.TerminalWindowName(name)) {
		asmtmux.CreateTerminalWindow(name, path)
		m.terminalStartTimes[name] = time.Now()
		cmd = waitForTermExitCmd(name)
	}
	asmtmux.SwapTermToWorkingPanel(name)
	m.termDir = name
	asmtmux.FocusWorkingPanel()
	m.applyAutoZoom()
	return cmd
}

func (m *PickerModel) switchToTerminal(wt *worktree.Worktree) tea.Cmd {
	// Already showing this terminal
	if m.termDir == wt.Name {
		asmtmux.FocusWorkingPanel()
		m.applyAutoZoom()
		return nil
	}

	// Swap out whatever is in the working panel
	m.swapOutWorkingPanel()
	return m.showTerminalInWorkingPanel(wt.Name, wt.Path)
}

func (m *PickerModel) toggleTerminal() tea.Cmd {
	if m.workingDir != "" {
		// AI session is displayed → switch to terminal
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
		asmtmux.SwapBackFromWorkingPanel(wtName)
		m.workingDir = ""
		return m.showTerminalInWorkingPanel(wtName, wtPath)
	} else if m.termDir != "" {
		// Terminal is displayed → switch to AI session (if exists)
		wtName := m.termDir
		asmtmux.SwapTermBackFromWorkingPanel(wtName)
		m.termDir = ""

		winName := asmtmux.WindowName(wtName)
		if asmtmux.WindowExists(winName) {
			asmtmux.SwapToWorkingPanel(wtName)
			m.workingDir = wtName
			asmtmux.FocusWorkingPanel()
			m.applyAutoZoom()
		}
	}
	return nil
}

func waitForTermExitCmd(dirName string) tea.Cmd {
	return func() tea.Msg {
		exec.Command("tmux", "wait-for", asmtmux.TermExitSignalName(dirName)).Run()
		return terminalExitedMsg{dirName: dirName}
	}
}


func (m *PickerModel) showInWorkingPanel(wt *worktree.Worktree) {
	if m.workingDir == wt.Name {
		asmtmux.FocusWorkingPanel()
		m.applyAutoZoom()
		return
	}
	// Recreate working panel if it was lost
	asmtmux.EnsureWorkingPanel()
	if m.workingDir != "" {
		asmtmux.SwapBackFromWorkingPanel(m.workingDir)
	}
	if m.termDir != "" {
		asmtmux.SwapTermBackFromWorkingPanel(m.termDir)
		m.termDir = ""
	}
	asmtmux.SwapToWorkingPanel(wt.Name)
	m.workingDir = wt.Name
	asmtmux.FocusWorkingPanel()
	m.applyAutoZoom()
}

// applyAutoZoom zooms the working pane if config's auto_zoom is enabled.
// No-op otherwise (keeps the split view).
func (m *PickerModel) applyAutoZoom() {
	if m.cfg != nil && m.cfg.IsAutoZoomEnabled() {
		asmtmux.ZoomWorkingPanel()
	}
}

// applyAutoZoomPicker zooms the picker pane if config's auto_zoom is enabled,
// so the picker fills the screen instead of leaving the working pane visible
// on the right. No-op when auto_zoom is disabled (keeps the split view).
func (m *PickerModel) applyAutoZoomPicker() {
	if m.cfg != nil && m.cfg.IsAutoZoomEnabled() {
		asmtmux.ZoomPickingPanel()
	}
}

func (m *PickerModel) itemHeight(wi int) int {
	h := 1
	taskInfo, hasTask := m.taskInfos[m.directories[wi].Path]
	hasTask = hasTask && taskInfo.Name != ""
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
	maxListLines := m.height - 2
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

// rotateSession cycles the working panel through active sessions (delta=+1 next, -1 prev).
// Preserves zoom state. No-op if fewer than 2 active sessions.
func (m *PickerModel) rotateSession(delta int) tea.Cmd {
	activeSet := make(map[string]bool)
	for _, n := range asmtmux.ListDirectoryWindows() {
		activeSet[n] = true
	}

	// Build ordered list of active worktrees (scan order).
	var active []*worktree.Worktree
	for i := range m.directories {
		if activeSet[m.directories[i].Name] {
			active = append(active, &m.directories[i])
		}
	}
	if len(active) == 0 {
		return nil
	}

	// Find current position. If nothing is shown, jump to first/last.
	idx := -1
	for i, wt := range active {
		if wt.Name == m.workingDir {
			idx = i
			break
		}
	}
	var next *worktree.Worktree
	if idx < 0 {
		if delta >= 0 {
			next = active[0]
		} else {
			next = active[len(active)-1]
		}
	} else {
		if len(active) < 2 {
			// Only session is already shown — just focus/zoom.
			asmtmux.FocusWorkingPanel()
			m.applyAutoZoom()
			return nil
		}
		ni := (idx + delta) % len(active)
		if ni < 0 {
			ni += len(active)
		}
		next = active[ni]
	}

	m.showInWorkingPanel(next)
	return nil
}

// Fixed widths (columns) for status bar items. All items are padded to these
// widths so neighboring items don't jitter as names scroll.
const (
	statusLine2NameWidth  = 20 // worktree/task name in the all-sessions row
	statusLine2StateWidth = 10 // state label column
	statusLine2KindWidth  = 5  // " [a+t]" badge column (width of "[a+t]")
	statusLine1NameWidth  = 24 // folder name on the current-session line
	// Task name width is computed dynamically based on terminal width; this
	// is the minimum fallback used when we can't query tmux.
	statusLine1TaskMinWidth = 60
	// Fixed chrome cost on line 1 (icon, separators, state, elapsed, badge, padding).
	statusLine1ChromeWidth = 58

	// Display-column cost of one line2 item: icon(1) + space + name(20) +
	// space + badge(5) + space + state(10) = 39.
	statusLine2ItemWidth = 39
	// Display-column cost of the " │ " separator between items.
	statusLine2SepWidth = 3
	// Reserved room for page indicator "(p/N) " plus the leading/trailing
	// single-space margins on the line.
	statusLine2ChromeWidth = 12
	// How many 200ms scroll ticks each page stays visible (5s).
	statusLine2TicksPerPage = 25
)

// refreshStatusSummary rebuilds the three-line bottom-bar (summary + shortcuts)
// and pushes it to tmux. Called on scrollTick (200ms). Skips the tmux write if
// the rendered strings haven't changed.
func (m *PickerModel) refreshStatusSummary() {
	activeKinds := asmtmux.ListActiveSessions()

	line1, line1Target := m.buildLine1(activeKinds)
	line2 := m.buildLine2(activeKinds, line1Target)
	shortcuts := renderShortcutsPlain(len(m.selectedItems))

	combined := line1 + "\x00" + line2 + "\x00" + shortcuts

	// Always enable the status bar — it hosts the shortcuts hint even when no
	// AI session is running.
	if !m.statusBarEnabled {
		asmtmux.EnableStatusBar()
		m.statusBarEnabled = true
	}
	if combined != m.lastStatusSummary {
		asmtmux.SetStatusLines(line1, line2)
		asmtmux.SetShortcutsLine(shortcuts)
		m.lastStatusSummary = combined
	}
}

// renderShortcutsPlain returns the shortcuts hint as a plain tmux format string
// (no ANSI, just `#[...]` style markers if needed).
func renderShortcutsPlain(selectedCount int) string {
	if selectedCount > 0 {
		return fmt.Sprintf(" %d selected  k: kill  x: delete  ^x: toggle  Esc: clear", selectedCount)
	}
	return " ↵: open  ^g: focus  ^t: term  ^n: new  ^]: rotate  ^x: select  ^k: task  ^p: AI  ^w: worktree  ^d: remove  ^s: settings  ^q: quit"
}

// buildLine1 returns the detailed line for the currently displayed session and
// the resolved target name (so line2 can skip it). Resolution order:
//  1. m.workingDir (AI currently in working panel)
//  2. m.termDir    (terminal currently in working panel)
//  3. first active session in scan order (AI preferred, then terminal)
func (m *PickerModel) buildLine1(activeKinds map[string]asmtmux.SessionKind) (string, string) {
	target := ""
	var targetKind asmtmux.SessionKind
	switch {
	case m.workingDir != "" && activeKinds[m.workingDir] != 0:
		target = m.workingDir
	case m.termDir != "" && activeKinds[m.termDir] != 0:
		target = m.termDir
	default:
		// Fallback: first active AI, else first active terminal, in scan order
		for _, wt := range m.directories {
			if activeKinds[wt.Name].HasAI() {
				target = wt.Name
				break
			}
		}
		if target == "" {
			for _, wt := range m.directories {
				if activeKinds[wt.Name].HasTerm() {
					target = wt.Name
					break
				}
			}
		}
	}
	if target == "" {
		return "", ""
	}
	targetKind = activeKinds[target]

	var wt *worktree.Worktree
	for i := range m.directories {
		if m.directories[i].Name == target {
			wt = &m.directories[i]
			break
		}
	}
	if wt == nil {
		return "", ""
	}

	// What is displayed determines which columns we fill. If working panel has the
	// AI session (or no panel selection and an AI exists), show AI state. Otherwise
	// the terminal is what the user sees: no task/state, but badge still reflects
	// the full kind (so a worktree with a+t shows "[a+t]" even when term is fronted).
	displayingTerm := (m.workingDir == "" && m.termDir == target) || !targetKind.HasAI()

	// Compute the task-name column width to fill the available terminal width.
	taskWidth := asmtmux.TerminalWidth() - statusLine1ChromeWidth - statusLine1NameWidth
	if taskWidth < statusLine1TaskMinWidth {
		taskWidth = statusLine1TaskMinWidth
	}

	// Folder name (fixed width, scrolled if longer)
	folder := m.scrollPadName(wt.Name, statusLine1NameWidth)

	// Kind badge (padded to fixed width to avoid jitter)
	badge := padToWidth(renderKindBadgeTmux(targetKind), statusLine2KindWidth)

	// Task name (dynamic width, scrolled if longer). Empty if not resolved.
	// Task metadata belongs to the worktree, so we show it regardless of which
	// session (AI vs terminal) is currently fronted.
	taskRaw := ""
	if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" {
		taskRaw = info.Name
	}
	taskPart := m.scrollPadName(taskRaw, taskWidth)

	// State + color (blank when terminal is displayed — terminal has no provider state)
	statePart, stateColor := m.statePaddedColumn(wt.Name, !displayingTerm)

	// Elapsed — AI session time when AI is displayed, else terminal open time.
	elapsed := ""
	if displayingTerm {
		if t, ok := m.terminalStartTimes[wt.Name]; ok {
			elapsed = formatElapsed(time.Since(t))
		}
	} else {
		if t, ok := m.sessionStartTimes[wt.Name]; ok {
			elapsed = formatElapsed(time.Since(t))
		}
	}
	elapsedPart := padToWidth(elapsed, 6)

	// Render with tmux format codes
	icon := "#[fg=colour81,bold]▶#[default]"
	sep := "#[fg=colour240]│#[default]"
	return fmt.Sprintf(" %s #[fg=colour252,bold]%s#[default] %s %s #[fg=colour141]%s#[default] %s #[fg=%s,bold]%s#[default] #[fg=colour244]%s#[default] ",
		icon, folder, badge, sep, taskPart, sep, stateColor, statePart, elapsedPart), target
}

// buildLine2 returns the other-active-sessions overview (excludes the session
// shown on line 1 to avoid duplication) with fixed-width items. One row per
// worktree: a worktree with both AI and terminal collapses into a single
// "[a+t]" item.
//
// When the set of items doesn't fit on one row, items are paginated and the
// visible page rotates every statusLine2TicksPerPage scroll ticks (e.g. 9
// sessions on a 3-per-page terminal becomes 1-3 → 4-6 → 7-9 → 1-3 …). A
// "(p/N)" indicator is prepended so the user can see rotation is happening.
func (m *PickerModel) buildLine2(activeKinds map[string]asmtmux.SessionKind, line1Target string) string {
	var items []string
	for _, wt := range m.directories {
		kind := activeKinds[wt.Name]
		if kind == 0 {
			continue
		}
		if wt.Name == line1Target {
			continue
		}
		items = append(items, m.renderStatusItem(wt, kind))
	}
	if len(items) == 0 {
		return ""
	}
	sep := "#[fg=colour240] │ #[default]"

	// How many items fit on a single row of the current terminal.
	// Solve: itemWidth*p + sepWidth*(p-1) + chrome <= termWidth
	//     => p <= (termWidth - chrome + sepWidth) / (itemWidth + sepWidth)
	termWidth := asmtmux.TerminalWidth()
	perPage := (termWidth - statusLine2ChromeWidth + statusLine2SepWidth) /
		(statusLine2ItemWidth + statusLine2SepWidth)
	if perPage < 1 {
		perPage = 1
	}

	if len(items) <= perPage {
		return " " + strings.Join(items, sep) + " "
	}

	numPages := (len(items) + perPage - 1) / perPage
	page := (m.scrollTick / statusLine2TicksPerPage) % numPages
	start := page * perPage
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	indicator := fmt.Sprintf("#[fg=colour240](%d/%d)#[default] ", page+1, numPages)
	return " " + indicator + strings.Join(items[start:end], sep) + " "
}

// renderStatusItem renders one active session as a fixed-width tmux format string.
// kind is the full bitmask for the worktree; used to render the [a]/[t]/[a+t] badge.
// Provider state is shown only when the worktree has an AI session.
func (m *PickerModel) renderStatusItem(wt worktree.Worktree, kind asmtmux.SessionKind) string {
	// Name: task name if resolved, otherwise folder name
	rawName := wt.Name
	if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" {
		rawName = info.Name
	}
	displayName := m.scrollPadName(rawName, statusLine2NameWidth)

	// State only meaningful for AI sessions; terminal-only rows leave it blank.
	statePadded, stateColor := m.statePaddedColumn(wt.Name, kind.HasAI())

	badge := padToWidth(renderKindBadgeTmux(kind), statusLine2KindWidth)

	iconPart := "#[fg=colour42]●#[default]"
	nameColor := "colour252"

	return fmt.Sprintf("%s #[fg=%s]%s#[default] %s #[fg=%s]%s#[default]",
		iconPart, nameColor, displayName, badge, stateColor, statePadded)
}

// scrollPadName renders a name column for the status bar: scrolls the text
// when longer than the column width, escapes tmux format characters, and
// right-pads the result to exactly `width` display columns. Used on both
// status-bar lines to keep the column boundaries stable as names animate.
func (m *PickerModel) scrollPadName(raw string, width int) string {
	return padToWidth(tmuxEscape(scrollText(raw, width, m.scrollTick)), width)
}

// statePaddedColumn returns the padded state label and its tmux color for a
// session. When `showState` is false (e.g. for terminal-only rows or when the
// terminal is fronted), the label is blank and the color is neutral.
func (m *PickerModel) statePaddedColumn(dirName string, showState bool) (string, string) {
	label, color := "", "colour244"
	if showState {
		label, color = m.stateLabelColor(dirName)
	}
	return padToWidth(label, statusLine2StateWidth), color
}

// stateLabelColor returns the provider-state label and tmux color code for a session.
func (m *PickerModel) stateLabelColor(dirName string) (string, string) {
	if _, flashing := m.flashItems[dirName]; flashing {
		return "done", "colour42"
	}
	st, ok := m.providerStates[dirName]
	if !ok {
		return "", "colour244"
	}
	switch st {
	case provider.StateThinking:
		return st.Label(), "colour220"
	case provider.StateResponding:
		return st.Label(), "colour114"
	case provider.StateToolUse:
		return st.Label(), "colour81"
	case provider.StateBusy:
		return st.Label(), "colour220"
	case provider.StateIdle:
		return st.Label(), "colour244"
	}
	return st.Label(), "colour244"
}

// tmuxEscape doubles '#' so tmux format parser treats it as a literal.
func tmuxEscape(s string) string {
	return strings.ReplaceAll(s, "#", "##")
}

// padToWidth right-pads s with spaces to reach the given visual width (in columns).
// Uses lipgloss.Width which handles CJK full-width characters.
func padToWidth(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vw)
}

// stabilizeCursor repositions m.cursor onto the given worktree name after a sort change.
// No-op if name is empty or not found.
func (m *PickerModel) stabilizeCursor(wtName string) {
	if wtName == "" {
		return
	}
	filtered := m.filteredDirectories()
	for fi, wi := range filtered {
		if m.directories[wi].Name == wtName {
			if m.cursor != fi {
				m.cursor = fi
				m.adjustViewTop()
			}
			return
		}
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

	// Batch confirm takes over the full pane (like the worktree/settings
	// dialogs) instead of overlaying a small centered box on the picker.
	if m.batchConfirm.IsVisible() {
		return m.batchConfirm.View()
	}

	var title string
	if m.focused {
		title = headerStyle.Render(filepath.Base(m.rootPath))
	} else {
		title = lipgloss.NewStyle().Foreground(dimColor).Padding(0, 1).Render(filepath.Base(m.rootPath))
	}

	activeKinds := asmtmux.ListActiveSessions()

	filtered := m.filteredDirectories()

	// Render all filtered items and count lines per item
	type renderedItem struct {
		text      string
		lineCount int
	}
	items := make([]renderedItem, len(filtered))
	for fi, wi := range filtered {
		wt := m.directories[wi]
		text := m.renderItem(fi, wt, activeKinds[wt.Name])
		items[fi] = renderedItem{text: text, lineCount: strings.Count(text, "\n") + 1}
	}

	// Available lines for the list (height - title - margin). Shortcuts bar is
	// rendered by tmux at the bottom of the terminal, outside this pane.
	maxListLines := m.height - 2
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

	// Build view: title (fixed) + search bar + list + padding
	var viewLines []string
	viewLines = append(viewLines, title)
	if m.searchQuery != "" {
		searchLine := lipgloss.NewStyle().Foreground(primaryColor).Padding(0, 1).Render("/ " + m.searchQuery)
		viewLines = append(viewLines, searchLine)
	}
	for _, row := range visibleRows {
		viewLines = append(viewLines, strings.Split(row, "\n")...)
	}
	targetLines := m.height
	for len(viewLines) < targetLines {
		viewLines = append(viewLines, "")
	}
	if len(viewLines) > targetLines {
		viewLines = viewLines[:targetLines]
	}
	view := strings.Join(viewLines, "\n")

	if m.err != "" {
		errDialog := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dangerColor).
			Padding(1, 2).
			Width(min(50, m.width-4)).
			Render(
				lipgloss.NewStyle().Bold(true).Foreground(dangerColor).Render("Error") + "\n\n" +
					lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(m.err) + "\n\n" +
					statusBarStyle.Render("Press any key to dismiss"))
		view = m.overlayCenter(view, errDialog)
	}

	return view
}

func (m PickerModel) renderItem(index int, wt worktree.Worktree, kind asmtmux.SessionKind) string {
	isSelected := index == m.cursor
	hasSession := kind != 0

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

	taskInfo, hasTask := m.taskInfos[wt.Path]
	hasTask = hasTask && taskInfo.Name != ""
	taskName := taskInfo.Name
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

	// Kind badge is rendered on the state sub-line (see below), not on the
	// primary name line, to keep the name row uncluttered.
	kindBadge := renderKindBadge(kind)

	// Calculate available width for primary name
	prefixWidth := 2 // indicator(1) + space(1)
	if inSelectionMode {
		prefixWidth += 2 // checkbox(1) + space(1)
	}
	maxNameWidth := m.width - prefixWidth
	if maxNameWidth < 1 {
		maxNameWidth = 1
	}

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
	switch {
	case kind.HasAI():
		if _, flashing := m.flashItems[wt.Name]; flashing {
			stateLine = CompletionFlashStyle.Render("✓ done!")
		} else if state, ok := m.providerStates[wt.Name]; ok {
			stateLine = m.renderProviderState(state, wt.Name, m.spinnerFrame)
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
	case kind.HasTerm():
		// Terminal-only session: show elapsed time from terminal open.
		if startTime, ok := m.terminalStartTimes[wt.Name]; ok {
			stateLine = ElapsedTimeStyle.Render(formatElapsed(time.Since(startTime)))
		}
	default:
		stateLine = ClosedStateStyle.Render("closed")
	}
	if kindBadge != "" {
		if stateLine != "" {
			stateLine = kindBadge + " " + stateLine
		} else {
			stateLine = kindBadge
		}
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
	cache := m.taskCache
	return func() tea.Msg {
		wts, _ := worktree.Scan(m.rootPath)
		var tasks map[string]tracker.TaskInfo
		var branches map[string]string
		if cache != nil {
			tasks = make(map[string]tracker.TaskInfo, len(wts))
			branches = make(map[string]string, len(wts))
			for _, wt := range wts {
				if e, ok := cache.GetEntry(wt.Path); ok {
					tasks[wt.Path] = e.Info
					branches[wt.Path] = e.Branch
				}
			}
		}
		return DirectoriesScannedMsg{
			Directories:    wts,
			CachedTasks:    tasks,
			CachedBranches: branches,
		}
	}
}

func (m PickerModel) fetchGitStatus(path string) tea.Cmd {
	return func() tea.Msg {
		gs := worktree.GetGitStatus(path)
		return GitStatusUpdatedMsg{Path: path, Status: gs}
	}
}

func (m PickerModel) fetchTaskName(path string, branch string) tea.Cmd {
	t := m.tracker
	return func() tea.Msg {
		info := t.Resolve(branch)
		return TaskResolvedMsg{Path: path, Branch: branch, Info: info}
	}
}

func flashExpireCmd(dirName string, startedAt time.Time, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(t time.Time) tea.Msg {
		return flashExpiredMsg{DirName: dirName, StartedAt: startedAt}
	})
}

// notifyCompletionCmd sends a desktop notification when an AI session finishes.
// displayName is the resolved title (task name preferred, falling back to folder).
func notifyCompletionCmd(dirName, displayName, currentWorkingDir string) tea.Cmd {
	return func() tea.Msg {
		isDisplayed := currentWorkingDir == dirName
		content, err := asmtmux.CapturePaneHistory(dirName, isDisplayed, 80)
		if err != nil {
			notification.Send("ASM – "+displayName, "done")
			return nil
		}
		snippet := extractLastResponse(content)
		title := "ASM – " + displayName
		if snippet != "" {
			notification.Send(title, snippet)
		} else {
			notification.Send(title, "done")
		}
		return nil
	}
}

// extractLastResponse extracts a short snippet of the last meaningful AI
// response text from pane content, stripping UI chrome (box borders, prompt
// indicators, Claude CLI footer banners, etc.).
func extractLastResponse(content string) string {
	lines := strings.Split(content, "\n")

	// Walk from the bottom up, skipping noise and the user-input box, collecting
	// meaningful lines from the last AI response.
	var meaningful []string
	for i := len(lines) - 1; i >= 0 && len(meaningful) < 8; i-- {
		stripped := stripBoxBorders(lines[i])
		if stripped == "" {
			continue
		}
		if isNoiseeLine(stripped) {
			continue
		}
		meaningful = append(meaningful, stripped)
	}

	if len(meaningful) == 0 {
		return ""
	}

	// Take up to 3 lines (more = richer preview), reverse back to original order.
	take := min(3, len(meaningful))
	var result []string
	for i := take - 1; i >= 0; i-- {
		result = append(result, meaningful[i])
	}

	text := strings.Join(result, " ")
	text = strings.Join(strings.Fields(text), " ") // collapse whitespace

	runes := []rune(text)
	if len(runes) > 140 {
		return string(runes[:140]) + "…"
	}
	return text
}

// stripBoxBorders trims whitespace and removes leading/trailing box-drawing
// chars (`│`, `┃`, `|`) and a leading `●` bullet used by Claude for AI
// messages. Keeps the inner text so it can be evaluated as content.
func stripBoxBorders(line string) string {
	line = strings.TrimSpace(line)
	for {
		runes := []rune(line)
		if len(runes) == 0 {
			return ""
		}
		first := runes[0]
		if first == '│' || first == '┃' || first == '|' || first == '●' || first == '•' || first == '>' {
			line = strings.TrimSpace(string(runes[1:]))
			continue
		}
		break
	}
	for {
		runes := []rune(line)
		if len(runes) == 0 {
			return ""
		}
		last := runes[len(runes)-1]
		if last == '│' || last == '┃' || last == '|' {
			line = strings.TrimSpace(string(runes[:len(runes)-1]))
			continue
		}
		break
	}
	return line
}

// isNoiseeLine returns true if the line is a prompt, separator, banner, or UI
// decoration with no useful content.
func isNoiseeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len([]rune(trimmed)) <= 2 {
		return true
	}
	lower := strings.ToLower(trimmed)
	// Claude CLI footer / status banners
	if strings.Contains(trimmed, "⏵") ||
		strings.Contains(lower, "bypass permissions") ||
		strings.Contains(lower, "for shortcuts") ||
		strings.Contains(lower, "esc to interrupt") ||
		strings.Contains(lower, "accept edits") ||
		strings.Contains(lower, "plan mode") {
		return true
	}
	// Lines made entirely of decorative chars (box-drawing, dashes, pipes, etc.)
	for _, r := range trimmed {
		switch r {
		case '─', '━', '—', '-', '=', '~', '╌', '┄',
			'╭', '╮', '╰', '╯', '│', '┃', '|',
			'┌', '┐', '└', '┘', '├', '┤', '┬', '┴', '┼',
			' ':
			continue
		default:
			return false
		}
	}
	return true
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*5, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func providerStateTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return providerStateTickMsg(t)
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

func (m PickerModel) fetchProviderState(dirName string) tea.Cmd {
	currentWT := m.workingDir
	providerName := m.worktreeProviders[dirName]
	p := m.registry.Get(providerName)
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		isDisplayed := currentWT == dirName
		title, err := asmtmux.GetPaneTitle(dirName, isDisplayed)
		if err != nil {
			return ProviderStateUpdatedMsg{Name: dirName, State: provider.StateUnknown}
		}

		var content string
		if p.NeedsContent(title) {
			content, _ = asmtmux.CapturePaneContent(dirName, isDisplayed)
		}

		state := p.DetectState(title, content)
		return ProviderStateUpdatedMsg{Name: dirName, State: state}
	}
}

func (m PickerModel) renderProviderState(state provider.State, dirName string, frame int) string {
	if state == provider.StateIdle {
		return IdleStateStyle.Render(state.Label())
	}
	if !state.IsBusy() {
		return ""
	}

	spinner := spinnerFrames[frame%len(spinnerFrames)]
	var style lipgloss.Style
	switch state {
	case provider.StateThinking:
		style = ThinkingStateStyle
	case provider.StateToolUse:
		style = ToolUseStateStyle
	case provider.StateResponding:
		style = RespondingStateStyle
	default:
		style = BusyStateStyle
	}

	label := state.Label()
	// Show provider name when multiple providers are active
	if m.registry.Count() > 1 {
		if pName := m.worktreeProviders[dirName]; pName != "" {
			if p := m.registry.Get(pName); p != nil {
				label = p.DisplayName() + " " + label
			}
		}
	}
	return style.Render(spinner + " " + label)
}

