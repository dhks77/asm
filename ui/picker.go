package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/asmlog"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/ide"
	"github.com/nhn/asm/notification"
	"github.com/nhn/asm/platform"
	"github.com/nhn/asm/provider"
	"github.com/nhn/asm/recent"
	"github.com/nhn/asm/sessionstate"
	"github.com/nhn/asm/shelljoin"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/worktree"
)

// Messages
type DirectoriesScannedMsg struct {
	Directories []worktree.Worktree
	// CachedTasks seeds m.taskInfos on the first frame so task names don't
	// trickle in over multiple renders. Keyed by worktree path.
	CachedTasks map[string]tracker.TaskInfo
	// CachedBranches records the branch each cached entry was observed
	// under; used to invalidate stale names once the branch is re-resolved.
	CachedBranches map[string]string
	// RepoRoots maps target path -> stable project root/grouping key.
	RepoRoots map[string]string
	// RepoLabels maps target path -> human-facing repo label shown in headers.
	RepoLabels map[string]string
	// RepoColors maps project root -> configured terminal color value.
	// Values may be presets, ANSI 0-255, or hex/rgb forms.
	RepoColors map[string]string
}

// BranchResolvedMsg is emitted after a one-off per-worktree branch lookup.
// Branch is empty if the lookup failed or timed out.
type BranchResolvedMsg struct {
	Path   string
	Branch string
}

type TaskResolvedMsg struct {
	Path   string
	Branch string
	Info   tracker.TaskInfo
}

type providerStateTickMsg time.Time

type ProviderStateUpdatedMsg struct {
	Path  string
	State provider.State
}

type spinnerTickMsg time.Time
type scrollTickMsg time.Time

// terminalLayoutTickMsg drives periodic tmux client-width refreshes. We can't
// rely on Bubble Tea's WindowSizeMsg alone because the picker pane may stay at
// a fixed cell width after a prior resize-pane -x, so the pane itself doesn't
// necessarily get a SIGWINCH when the outer terminal changes size.
type terminalLayoutTickMsg time.Time

// terminalLayoutResolvedMsg delivers the attached tmux client width plus the
// main-window zoom flag, measured off the Update goroutine.
type terminalLayoutResolvedMsg struct {
	width         int
	zoomed        bool
	clientFocused bool
}

// statusSummaryWrittenMsg is returned by writeStatusSummaryCmd once its
// tmux set-option calls have finished. It clears the inflight flag so the
// next scrollTick can issue another write.
type statusSummaryWrittenMsg struct{}

// sessionHealthTickMsg fires periodically to trigger a background
// "is my tmux session still alive?" probe.
type sessionHealthTickMsg time.Time

// sessionHealthResultMsg carries the outcome of that probe. On a negative
// result the picker exits — a dangling picker process whose pane has been
// torn down does nothing useful and can fast-spin on failing tmux execs
// (classic orphan-picker leak that piled up to 100+MB RSS in the field).
type sessionHealthResultMsg struct{ alive bool }

type sessionExitedMsg struct {
	Path string
}

type flashExpiredMsg struct {
	Path      string
	StartedAt time.Time
}

type batchKillCompletedMsg struct{ count int }
type batchDeleteCompletedMsg struct{ count int }
type launcherExitedMsg struct {
	Path string
}

type restoreSnapshotDoneMsg struct {
	Targets       []sessionstate.TargetSnapshot
	FrontPath     string
	FrontKind     string
	FocusedPane   string
	WorkingZoomed bool
	Errors        []string
}

type queuedTaskResolve struct {
	Path   string
	Branch string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type PickerModel struct {
	cfg                 *config.Config
	rootPath            string
	directories         []worktree.Worktree
	branches            map[string]string // worktree path -> branch name (one-shot)
	taskInfos           map[string]tracker.TaskInfo
	repoRoots           map[string]string // target path -> group root
	repoLabels          map[string]string // target path -> display label
	repoColors          map[string]string // project root -> configured terminal color value
	providerStates      map[string]provider.State
	prevProviderStates  map[string]provider.State
	providerNotifyReady map[string]bool
	worktreeProviders   map[string]string // worktree name -> provider name
	registry            *provider.Registry
	sessionStartTimes   map[string]time.Time
	terminalStartTimes  map[string]time.Time
	flashItems          map[string]time.Time
	// providerStatePending counts outstanding DetectState calls from the
	// current cycle. The next detect-state tick is only scheduled once
	// this reaches zero, so slow providers/tmux never cause fan-out.
	providerStatePending int
	// activeKinds caches the last tmux-derived session map (per worktree)
	// so the hot scrollTick status-bar render can run without blocking
	// Update on a tmux exec. Refreshed every providerStateTick (1s).
	activeKinds map[string]asmtmux.SessionKind
	// terminalWidth caches the tmux client width so status-bar layout
	// computations don't call tmux synchronously from Update. Refreshed
	// by a lightweight watcher via async tea.Cmd.
	terminalWidth int
	// terminalWidthPending gates the async tmux layout probe so repeated
	// WindowSizeMsg/watch ticks don't fan out concurrent tmux execs.
	terminalWidthPending bool
	// pickerWidthDirty means the current split width no longer matches the
	// configured picker percentage and should be re-applied once the main
	// window is not zoomed.
	pickerWidthDirty bool
	// layoutSyncPending forces a hidden-window size resync after a picker
	// WindowSizeMsg even when the tmux client width itself didn't change.
	layoutSyncPending bool
	// clientFocused tracks whether the attached tmux client is currently
	// focused. While unfocused we ignore layout churn from cmux workspace
	// switches and resync only when focus returns.
	clientFocused bool
	// statusSummaryWriting is true while a writeStatusSummaryCmd goroutine
	// is still flushing set-option calls to tmux. scrollTick skips issuing
	// a new write while this is set, so a slow tmux server can't snowball
	// goroutines at the 200ms scroll cadence.
	statusSummaryWriting bool
	spinnerFrame         int
	scrollTick           int
	cursor               int
	viewTop              int    // first visible item index for scrolling
	workingPath          string // target path shown in working panel (AI session)
	termPath             string // target path shown in working panel (terminal)
	tracker              tracker.Tracker
	taskCache            *tracker.TaskCache
	ides                 []ide.IDE
	// cachedBranches tracks the branch each seeded taskInfo was observed
	// under; we invalidate the seed when the branch re-resolves differently.
	cachedBranches map[string]string
	// branchVerified tracks which paths have had their branch re-read from git
	// in this picker lifetime. Seeded branch values can render immediately,
	// but we still verify them lazily in the background.
	branchVerified map[string]bool
	// metadata fetch queues keep branch/task subprocesses bounded so initial
	// picker startup stays responsive even with many open sessions.
	branchFetchQueue   []string
	queuedBranches     map[string]bool
	branchFetchPending bool
	taskFetchQueue     []queuedTaskResolve
	queuedTasks        map[string]bool
	taskFetchPending   bool
	focused            bool
	width              int
	height             int
	ready              bool
	err                string
	searchQuery        string
	workingZoomed      bool
	restoreLast        bool
	selectedItems      map[string]bool

	// Top status bar (summary of all active sessions)
	lastStatusSummary string
	statusBarEnabled  bool
}

func NewPickerModel(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker, taskCache *tracker.TaskCache, ides []ide.IDE, restoreLast bool) PickerModel {
	return PickerModel{
		cfg:                  cfg,
		rootPath:             rootPath,
		branches:             make(map[string]string),
		taskInfos:            make(map[string]tracker.TaskInfo),
		repoRoots:            make(map[string]string),
		repoLabels:           make(map[string]string),
		repoColors:           make(map[string]string),
		providerStates:       make(map[string]provider.State),
		prevProviderStates:   make(map[string]provider.State),
		providerNotifyReady:  make(map[string]bool),
		worktreeProviders:    make(map[string]string),
		registry:             registry,
		sessionStartTimes:    make(map[string]time.Time),
		terminalStartTimes:   make(map[string]time.Time),
		flashItems:           make(map[string]time.Time),
		activeKinds:          make(map[string]asmtmux.SessionKind),
		terminalWidth:        120, // sane default until the first tmux query lands
		terminalWidthPending: true,
		pickerWidthDirty:     true,
		layoutSyncPending:    true,
		clientFocused:        true,
		selectedItems:        make(map[string]bool),
		tracker:              t,
		taskCache:            taskCache,
		cachedBranches:       make(map[string]string),
		branchVerified:       make(map[string]bool),
		queuedBranches:       make(map[string]bool),
		queuedTasks:          make(map[string]bool),
		ides:                 ides,
		focused:              true,
		restoreLast:          restoreLast,
	}
}

func (m *PickerModel) worktreeByPath(path string) *worktree.Worktree {
	for i := range m.directories {
		if m.directories[i].Path == path {
			return &m.directories[i]
		}
	}
	return nil
}

func taskResolveKey(path, branch string) string {
	return path + "\x00" + branch
}

func (m *PickerModel) enqueueBranchFetch(path string) {
	if path == "" || m.branchVerified[path] || m.queuedBranches[path] {
		return
	}
	m.branchFetchQueue = append(m.branchFetchQueue, path)
	m.queuedBranches[path] = true
}

func (m *PickerModel) enqueueTaskFetch(path, branch string) {
	if path == "" || branch == "" || m.tracker == nil {
		return
	}
	key := taskResolveKey(path, branch)
	if m.queuedTasks[key] {
		return
	}
	m.taskFetchQueue = append(m.taskFetchQueue, queuedTaskResolve{Path: path, Branch: branch})
	m.queuedTasks[key] = true
}

func (m *PickerModel) pruneMetadataQueues(validPaths map[string]bool) {
	for path := range m.branches {
		if !validPaths[path] {
			delete(m.branches, path)
		}
	}
	for path := range m.taskInfos {
		if !validPaths[path] {
			delete(m.taskInfos, path)
		}
	}
	for path := range m.cachedBranches {
		if !validPaths[path] {
			delete(m.cachedBranches, path)
		}
	}
	for path := range m.branchVerified {
		if !validPaths[path] {
			delete(m.branchVerified, path)
		}
	}
	filteredBranches := m.branchFetchQueue[:0]
	for _, path := range m.branchFetchQueue {
		if validPaths[path] {
			filteredBranches = append(filteredBranches, path)
		} else {
			delete(m.queuedBranches, path)
		}
	}
	m.branchFetchQueue = filteredBranches

	filteredTasks := m.taskFetchQueue[:0]
	for _, req := range m.taskFetchQueue {
		if validPaths[req.Path] {
			filteredTasks = append(filteredTasks, req)
		} else {
			delete(m.queuedTasks, taskResolveKey(req.Path, req.Branch))
		}
	}
	m.taskFetchQueue = filteredTasks
}

func (m *PickerModel) startNextBranchFetch() tea.Cmd {
	if m.branchFetchPending || len(m.branchFetchQueue) == 0 {
		return nil
	}
	path := m.branchFetchQueue[0]
	m.branchFetchQueue = m.branchFetchQueue[1:]
	delete(m.queuedBranches, path)
	m.branchFetchPending = true
	return m.fetchBranch(path)
}

func (m *PickerModel) startNextTaskFetch() tea.Cmd {
	if m.taskFetchPending || len(m.taskFetchQueue) == 0 {
		return nil
	}
	req := m.taskFetchQueue[0]
	m.taskFetchQueue = m.taskFetchQueue[1:]
	delete(m.queuedTasks, taskResolveKey(req.Path, req.Branch))
	m.taskFetchPending = true
	return m.fetchTaskName(req.Path, req.Branch)
}

func (m *PickerModel) startNextMetadataFetches() tea.Cmd {
	var cmds []tea.Cmd
	if cmd := m.startNextBranchFetch(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.startNextTaskFetch(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *PickerModel) requestTerminalLayout() tea.Cmd {
	if m.terminalWidthPending {
		return nil
	}
	m.terminalWidthPending = true
	return fetchTerminalLayoutCmd()
}

func (m *PickerModel) syncLayoutCmd(zoomed bool, syncMain bool) tea.Cmd {
	resizePicker := m.pickerWidthDirty && !zoomed && m.cfg != nil
	if !resizePicker && !syncMain {
		return nil
	}
	pickerPct := 0
	if resizePicker {
		m.pickerWidthDirty = false
		pickerPct = m.cfg.GetPickerWidth()
	}
	return func() tea.Msg {
		if syncMain {
			asmtmux.SyncMainWindowSize()
		}
		if resizePicker {
			_ = asmtmux.ResizePickerPanel(pickerPct)
		}
		if resizePicker || syncMain {
			asmtmux.SyncManagedWindowSizes()
		}
		return nil
	}
}

func restoreSnapshotCmd(sessionID string, registry *provider.Registry) tea.Cmd {
	return func() tea.Msg {
		snap, err := sessionstate.Load(sessionID)
		if err != nil || snap == nil || !snap.HasTargets() {
			return nil
		}

		msg := restoreSnapshotDoneMsg{
			FrontPath:     snap.FrontPath,
			FrontKind:     snap.FrontKind,
			FocusedPane:   snap.FocusedPane,
			WorkingZoomed: snap.WorkingZoomed,
		}

		for _, target := range snap.Targets {
			restored := sessionstate.TargetSnapshot{Path: target.Path}
			if target.HasAI {
				p := registry.Get(target.Provider)
				if p == nil {
					p = registry.Default()
				}
				args := p.Args()
				if extra := p.ResumeArgs(target.Path); len(extra) > 0 {
					args = append(append([]string(nil), extra...), args...)
				}
				if err := asmtmux.CreateDirectoryWindow(target.Path, target.Path, p.Command(), args); err != nil {
					msg.Errors = append(msg.Errors, fmt.Sprintf("%s (AI): %v", filepath.Base(target.Path), err))
				} else {
					_ = asmtmux.SetWindowOption(target.Path, "asm-provider", p.Name())
					restored.HasAI = true
					restored.Provider = p.Name()
				}
			}
			if target.HasTerm {
				if err := asmtmux.CreateTerminalWindow(target.Path, target.Path); err != nil {
					msg.Errors = append(msg.Errors, fmt.Sprintf("%s (term): %v", filepath.Base(target.Path), err))
				} else {
					restored.HasTerm = true
				}
			}
			if restored.HasAI || restored.HasTerm {
				msg.Targets = append(msg.Targets, restored)
			}
		}

		return msg
	}
}

func (m *PickerModel) buildSessionSnapshot(activeKinds map[string]asmtmux.SessionKind) sessionstate.Snapshot {
	snap := sessionstate.Snapshot{
		SessionID:     asmtmux.SessionID,
		FrontPath:     m.workingPath,
		FrontKind:     "ai",
		FocusedPane:   "working",
		WorkingZoomed: m.workingZoomed,
	}
	if m.termPath != "" {
		snap.FrontPath = m.termPath
		snap.FrontKind = "term"
	}
	if m.focused {
		snap.FocusedPane = "picker"
	}

	for _, wt := range m.directories {
		kind := activeKinds[wt.Path]
		if kind == 0 {
			continue
		}
		target := sessionstate.TargetSnapshot{
			Path:    wt.Path,
			HasAI:   kind.HasAI(),
			HasTerm: kind.HasTerm(),
		}
		if kind.HasAI() {
			target.Provider = m.worktreeProviders[wt.Path]
			if target.Provider == "" {
				target.Provider = m.registry.Default().Name()
			}
		}
		snap.Targets = append(snap.Targets, target)
	}

	return snap
}

func (m *PickerModel) persistSessionSnapshotCmd(activeKinds map[string]asmtmux.SessionKind) tea.Cmd {
	snap := m.buildSessionSnapshot(activeKinds)
	return func() tea.Msg {
		_ = sessionstate.Save(asmtmux.SessionID, snap)
		return nil
	}
}

func (m *PickerModel) focusWorkingPanel() {
	asmtmux.FocusWorkingPanel()
}

func (m *PickerModel) swapAIToWorkingPanel(targetPath string) bool {
	if !asmtmux.WindowExists(asmtmux.WindowName(targetPath)) {
		return false
	}
	asmtmux.SwapToWorkingPanel(targetPath)
	m.workingPath = targetPath
	m.termPath = ""
	m.stabilizeCursor(targetPath)
	return true
}

func (m *PickerModel) swapTermToWorkingPanel(targetPath string) bool {
	if !asmtmux.WindowExists(asmtmux.TerminalWindowName(targetPath)) {
		return false
	}
	asmtmux.SwapTermToWorkingPanel(targetPath)
	m.termPath = targetPath
	m.workingPath = ""
	m.stabilizeCursor(targetPath)
	return true
}

func (m *PickerModel) swapCurrentAIOut() bool {
	if m.workingPath == "" {
		return false
	}
	asmtmux.SwapBackFromWorkingPanel(m.workingPath)
	m.workingPath = ""
	return true
}

func (m *PickerModel) swapCurrentTermOut() bool {
	if m.termPath == "" {
		return false
	}
	asmtmux.SwapTermBackFromWorkingPanel(m.termPath)
	m.termPath = ""
	return true
}

// filteredDirectories returns indices into m.directories matching the current
// search query, grouped by repository/project root.
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
			if m.matchesSearchQuery(wt, query) {
				matched = append(matched, i)
			}
		}
	}

	activeKinds := m.activeKinds
	sort.SliceStable(matched, func(i, j int) bool {
		left := m.directories[matched[i]]
		right := m.directories[matched[j]]

		leftRoot := m.repoRootForPath(left.Path)
		rightRoot := m.repoRootForPath(right.Path)
		leftLabel := strings.ToLower(m.repoLabelForPath(left.Path))
		rightLabel := strings.ToLower(m.repoLabelForPath(right.Path))
		if leftLabel != rightLabel {
			return leftLabel < rightLabel
		}
		if leftRoot != rightRoot {
			return leftRoot < rightRoot
		}

		leftActive := activeKinds[left.Path] != 0
		rightActive := activeKinds[right.Path] != 0
		if leftActive != rightActive {
			return leftActive && !rightActive
		}

		return false
	})
	return matched
}

func (m *PickerModel) matchesSearchQuery(wt worktree.Worktree, query string) bool {
	if strings.Contains(strings.ToLower(wt.Name), query) {
		return true
	}
	if label := m.repoLabelForPath(wt.Path); label != "" && strings.Contains(strings.ToLower(label), query) {
		return true
	}
	if strings.Contains(strings.ToLower(wt.Path), query) {
		return true
	}
	if root := m.repoRootForPath(wt.Path); root != "" && strings.Contains(strings.ToLower(root), query) {
		return true
	}
	if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" && strings.Contains(strings.ToLower(info.Name), query) {
		return true
	}
	if branch, ok := m.branches[wt.Path]; ok && branch != "" && strings.Contains(strings.ToLower(branch), query) {
		return true
	}
	return false
}

func (m PickerModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.scanDirectories(),
		providerStateTickCmd(),
		spinnerTickCmd(),
		scrollTickCmd(),
		sessionHealthTickCmd(),
		fetchTerminalLayoutCmd(),
	}
	if m.restoreLast {
		cmds = append(cmds, restoreSnapshotCmd(asmtmux.SessionID, m.registry))
	}
	return tea.Batch(cmds...)
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.layoutSyncPending = true
		// Bubble Tea only reports picker-pane size changes. The outer tmux
		// client can resize without changing this pane's width, so kick the
		// async tmux-side layout probe here and also keep a background watcher.
		return m, m.requestTerminalLayout()

	case terminalLayoutTickMsg:
		return m, m.requestTerminalLayout()

	case terminalLayoutResolvedMsg:
		m.terminalWidthPending = false
		prevClientFocused := m.clientFocused
		m.clientFocused = msg.clientFocused
		if !msg.clientFocused {
			m.layoutSyncPending = true
			return m, terminalLayoutTickCmd()
		}
		prevZoomed := m.workingZoomed
		m.workingZoomed = msg.zoomed
		layoutChanged := prevZoomed != msg.zoomed || m.layoutSyncPending || !prevClientFocused
		if msg.width > 0 && msg.width != m.terminalWidth {
			m.terminalWidth = msg.width
			m.pickerWidthDirty = true
			layoutChanged = true
		} else if msg.width > 0 {
			m.terminalWidth = msg.width
		}
		m.layoutSyncPending = false
		var cmds []tea.Cmd
		if cmd := m.syncLayoutCmd(msg.zoomed, layoutChanged); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, terminalLayoutTickCmd())
		return m, tea.Batch(cmds...)

	case restoreSnapshotDoneMsg:
		var cmds []tea.Cmd
		for _, target := range msg.Targets {
			wt, extraCmds := m.ensureDirectoryTracked(target.Path)
			if wt == nil {
				continue
			}
			cmds = append(cmds, extraCmds...)
			if target.HasAI {
				providerName := target.Provider
				if providerName == "" {
					providerName = m.registry.Default().Name()
				}
				m.worktreeProviders[target.Path] = providerName
				m.sessionStartTimes[target.Path] = time.Now()
				m.activeKinds[target.Path] |= asmtmux.SessionAI
				cmds = append(cmds, waitForExitCmd(target.Path))
			}
			if target.HasTerm {
				m.terminalStartTimes[target.Path] = time.Now()
				m.activeKinds[target.Path] |= asmtmux.SessionTerm
				cmds = append(cmds, waitForTermExitCmd(target.Path))
			}
		}
		if msg.FrontPath != "" {
			if wt := m.worktreeByPath(msg.FrontPath); wt != nil {
				switch msg.FrontKind {
				case "term":
					if m.swapTermToWorkingPanel(msg.FrontPath) {
						m.focusWorkingPanel()
					}
				default:
					m.showInWorkingPanel(wt)
				}
			}
		}
		if msg.WorkingZoomed {
			m.workingZoomed = true
			_ = asmtmux.ZoomWorkingPanel()
		} else {
			m.workingZoomed = false
			if msg.FocusedPane == "working" && (m.workingPath != "" || m.termPath != "") {
				m.focusWorkingPanel()
			} else {
				asmtmux.FocusPickingPanel()
			}
		}
		if len(msg.Errors) > 0 {
			m.err = "Restore issues:\n" + strings.Join(msg.Errors, "\n")
		}
		cmds = append(cmds, m.scanDirectories())
		return m, tea.Batch(cmds...)

	case statusSummaryWrittenMsg:
		m.statusSummaryWriting = false
		return m, nil

	case sessionHealthTickMsg:
		// Dispatch the probe in a goroutine so SessionExists (3s timeout)
		// never parks Update.
		return m, func() tea.Msg {
			return sessionHealthResultMsg{alive: asmtmux.SessionExists()}
		}

	case sessionHealthResultMsg:
		if !msg.alive {
			// Our tmux session is gone — nothing left to drive. Exiting
			// releases any leaked goroutines and clears the way for a
			// fresh asm run to take over.
			return m, tea.Quit
		}
		return m, sessionHealthTickCmd()

	case tea.FocusMsg:
		m.focused = true
		// Close any utility dialogs when picker gets focus
		if asmtmux.HasUtilityWindow() {
			asmtmux.CloseUtilityPanel()
		}
		return m, nil

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case providerStateTickMsg:
		return m.handleProviderStateTick()

	case ProviderStateUpdatedMsg:
		return m.handleProviderStateUpdated(msg)

	case flashExpiredMsg:
		if startedAt, ok := m.flashItems[msg.Path]; ok {
			if startedAt.Equal(msg.StartedAt) {
				delete(m.flashItems, msg.Path)
			}
		}
		return m, nil

	case spinnerTickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTickCmd()

	case scrollTickMsg:
		m.scrollTick++
		writeCmd := m.refreshStatusSummary()
		if writeCmd == nil {
			return m, scrollTickCmd()
		}
		return m, tea.Batch(scrollTickCmd(), writeCmd)

	case sessionExitedMsg:
		return m.handleSessionExited(msg)

	case worktreeExitedMsg:
		return m.handleWorktreeExited(msg)

	case DirectoryRemovedMsg:
		return m, m.refreshDirectoriesCmd()

	case WorktreeErrorMsg:
		m.err = msg.Err
		return m, nil

	case deleteExitedMsg:
		return m.handleDeleteExited(msg)

	case BatchConfirmedMsg:
		switch msg.Action {
		case BatchKillSessions:
			m.clearSelection()
			return m, m.batchKillSessions(msg.Items)
		case BatchDeleteWorktrees:
			m.clearSelection()
			return m, m.batchDeleteWorktrees(msg.Items)
		}
		return m, nil

	case BatchCancelledMsg:
		return m, nil

	case ideSelectDoneMsg:
		if msg.IDEName == "" || msg.Path == "" {
			return m, nil
		}
		return m, m.openWorktreeInIDE(msg.Path, msg.IDEName)

	case launcherExitedMsg:
		return m.handleLauncherExited(msg)

	case providerSelectDoneMsg:
		return m.handleProviderSelectDone(msg)

	case batchKillCompletedMsg:
		return m, m.refreshDirectoriesCmd()

	case batchDeleteCompletedMsg:
		return m, m.refreshDirectoriesCmd()

	case tea.KeyMsg:
		if m.err != "" {
			m.err = ""
			return m, nil
		}
		return m.handleKey(msg)

	case DirectoriesScannedMsg:
		return m.handleDirectoriesScanned(msg)

	case BranchResolvedMsg:
		return m.handleBranchResolved(msg)

	case TaskResolvedMsg:
		return m.handleTaskResolved(msg)

	case settingsExitedMsg:
		return m.handleSettingsExited()

	case terminalExitedMsg:
		return m.handleTerminalExited(msg)

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
		if cmd := m.openOrFocusWorktree(wt); cmd != nil {
			return m, cmd
		}

	case "f12": // Ctrl+t: open / focus / toggle terminal.
		// Source-pane aware:
		//   - picker pane: target = cursor's worktree
		//   - working pane: target = whatever session owns the working panel
		// When target == current working panel session → toggle AI↔Term.
		// Otherwise → switch the working panel to target's terminal.
		if asmtmux.ActivePaneIndex() == 1 {
			// From the working pane: act on the session currently fronted.
			if m.workingPath != "" || m.termPath != "" {
				return m, m.toggleTerminal()
			}
			// Nothing in working panel — fall through to cursor behavior.
		}
		wt := m.selectedDirectory()
		if wt == nil {
			return m, nil
		}
		if m.workingPath == wt.Path || m.termPath == wt.Path {
			return m, m.toggleTerminal()
		}
		return m, m.switchToTerminal(wt)

	case "f11": // Ctrl+g: toggle left/right pane focus
		// Close any open utility dialogs (settings, worktree, delete, provider-select)
		if asmtmux.HasUtilityWindow() {
			asmtmux.CloseUtilityPanel()
			return m, nil
		}

		// If the right pane is already showing something, just move focus there.
		// Ctrl+G is a left/right toggle, not a session-kind switch.
		if m.workingPath != "" || m.termPath != "" {
			m.focusWorkingPanel()
			return m, nil
		}

		wt := m.selectedDirectory()
		if wt == nil {
			return m, nil
		}
		if cmd := m.openOrFocusWorktree(wt); cmd != nil {
			return m, cmd
		}

	case "f10": // Ctrl+n: open launcher
		return m, m.openLauncher()

	case "ctrl+l": // toggle picker panel visibility
		return m, m.togglePickerPanel()

	case "f9": // Ctrl+s: settings
		return m, m.openSettings()

	case "f8": // Ctrl+q: quit
		asmtmux.KillSession()
		return m, tea.Quit

	case "f7": // Ctrl+w: create worktree
		if !m.contextSupportsWorktree() {
			return m, nil
		}
		dir := m.contextDirectory()
		if dir == nil {
			return m, nil
		}
		return m, m.openWorktreeDialog(dir)

	case "f6": // Ctrl+d: delete active directory
		if len(m.selectedItems) > 0 {
			return m, m.openBatchDelete()
		}
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
			if m.selectedItems[wt.Path] {
				delete(m.selectedItems, wt.Path)
			} else {
				m.selectedItems[wt.Path] = true
			}
		}

	case "f1": // Ctrl+] : rotate to next active session (cyclic)
		return m, m.rotateSession(+1)

	case "f2": // Ctrl+e: open cursor worktree in an IDE (shows selector)
		wt := m.contextDirectory()
		if wt == nil || len(m.ides) == 0 {
			return m, nil
		}
		// If only one IDE is configured, skip the selector entirely.
		if len(m.ides) == 1 {
			return m, m.openWorktreeInIDE(wt.Path, m.ides[0].Name)
		}
		// If DefaultIDE is set and matches a known IDE, skip selector.
		if defaultIDE := m.defaultIDEName(wt.Path); defaultIDE != "" {
			if found := ide.Find(m.ides, defaultIDE); found != nil {
				return m, m.openWorktreeInIDE(wt.Path, found.Name)
			}
		}
		return m, m.openIDESelect(wt.Path)

	case "f3": // Ctrl+k: kill selected session
		if len(m.selectedItems) > 0 {
			return m, m.openBatchKill()
		}
		return m, m.openKillSession()

	case "ctrl+o": // Open task URL in browser
		wt := m.contextDirectory()
		if wt != nil {
			if info, ok := m.taskInfos[wt.Path]; ok && info.URL != "" {
				_ = platform.Current().OpenURL(info.URL)
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

// startSession launches the AI provider in a fresh tmux window for the given
// worktree. When resume is true and the provider advertises ResumeArgs, those
// args are prepended — asking the provider to continue its previous session
// in this worktree's cwd. Ctrl+N is the only caller that passes resume=false
// (explicit "start fresh"); every other entry point (Enter, Ctrl+G, provider
// switch) defaults to resuming so closing + reopening a window doesn't lose
// the conversation.
func (m *PickerModel) startSession(wt *worktree.Worktree, providerName string, resume bool) tea.Cmd {
	p := m.registry.Get(providerName)
	if p == nil {
		p = m.registry.Default()
	}
	args := p.Args()
	if resume {
		if extra := p.ResumeArgs(wt.Path); len(extra) > 0 {
			args = append(append([]string(nil), extra...), args...)
		}
	}
	asmlog.Debugf("picker: start-session session=%q path=%q provider=%q resume=%t args=%v",
		asmtmux.SessionName, wt.Path, p.Name(), resume, args)
	if err := asmtmux.CreateDirectoryWindow(wt.Path, wt.Path, p.Command(), args); err != nil {
		asmlog.Debugf("picker: start-session create failed path=%q provider=%q err=%v", wt.Path, p.Name(), err)
		m.err = fmt.Sprintf("Failed to launch %s: %v", p.Name(), err)
		return nil
	}
	if err := asmtmux.SetWindowOption(wt.Path, "asm-provider", p.Name()); err != nil {
		asmlog.Debugf("picker: start-session set-provider failed path=%q provider=%q err=%v", wt.Path, p.Name(), err)
	}
	m.worktreeProviders[wt.Path] = p.Name()
	m.sessionStartTimes[wt.Path] = time.Now()
	m.showInWorkingPanel(wt)
	asmlog.Debugf("picker: start-session visible working_path=%q target=%q", m.workingPath, wt.Path)
	return waitForExitCmd(wt.Path)
}

// waitForExitCmd returns a tea.Cmd that blocks until the AI process exits for a target path.
func waitForExitCmd(targetPath string) tea.Cmd {
	return func() tea.Msg {
		asmtmux.WaitForExit(targetPath)
		return sessionExitedMsg{Path: targetPath}
	}
}

type settingsExitedMsg struct{}
type worktreeExitedMsg struct {
	created bool
	path    string
}
type terminalExitedMsg struct {
	path string
}
type deleteExitedMsg struct {
	path      string
	confirmed bool
}

// runDialogInWorkingPanel is the shared boilerplate for every modal dialog
// that runs as a child `asm` invocation inside the working pane. It swaps any
// current session out, spawns the dialog, focuses the working pane,
// and returns a tea.Cmd that blocks on dialog exit and converts the exit
// code into the caller-supplied message.
//
// args is the argv slice appended to the executable path (e.g.
// []string{"--settings"}). resultMsg is invoked with
// the dialog's exit code once it terminates.
//
// The picker's context path is injected via ASM_CONTEXT_PATH for every dialog
// subprocess so local/project settings still resolve against the target the
// dialog was opened for.
func (m *PickerModel) runDialogInWorkingPanelAtPath(dialogPath, windowName string, args []string, resultMsg func(exitCode int) tea.Msg) tea.Cmd {
	if asmtmux.HasUtilityWindow() {
		asmlog.Debugf("picker: dialog request ignored because utility is already open session=%q requested=%q",
			asmtmux.SessionName, windowName)
		asmtmux.FocusWorkingPanel()
		return nil
	}

	m.swapOutWorkingPanel()

	exe, err := platform.Current().ExecutablePath()
	if err != nil {
		asmlog.Debugf("picker: dialog executable lookup failed window=%q err=%v", windowName, err)
		return nil
	}

	argv := shelljoin.Join(append([]string{
		"env",
		"ASM_SESSION_NAME=" + asmtmux.SessionName,
		"ASM_CONTEXT_PATH=" + dialogPath,
		exe,
	}, args...)...)
	asmlog.Debugf("picker: open-dialog session=%q window=%q dialog_path=%q args=%v", asmtmux.SessionName, windowName, dialogPath, args)
	if err := asmtmux.RunInWorkingPanel(windowName, argv); err != nil {
		asmlog.Debugf("picker: open-dialog failed session=%q window=%q err=%v", asmtmux.SessionName, windowName, err)
		m.err = fmt.Sprintf("Failed to open %s: %v", windowName, err)
		return nil
	}
	asmtmux.FocusWorkingPanel()

	return func() tea.Msg {
		exitCode := asmtmux.WaitAndCleanupWorkingPanel(windowName)
		asmlog.Debugf("picker: dialog-exited session=%q window=%q exit_code=%d", asmtmux.SessionName, windowName, exitCode)
		return resultMsg(exitCode)
	}
}

func (m *PickerModel) runDialogInWorkingPanel(windowName string, args []string, resultMsg func(exitCode int) tea.Msg) tea.Cmd {
	return m.runDialogInWorkingPanelAtPath(m.rootPath, windowName, args, resultMsg)
}

func (m *PickerModel) openBatchConfirmDialog(req BatchConfirmRequest, resultMsg func(exitCode int) tea.Msg) tea.Cmd {
	if err := StoreBatchConfirmRequest(req); err != nil {
		m.err = fmt.Sprintf("Failed to open confirmation: %v", err)
		return nil
	}
	cmd := m.runDialogInWorkingPanel("asm-batch-confirm", []string{"--batch-confirm"}, func(exitCode int) tea.Msg {
		if err := ClearBatchConfirmRequest(); err != nil {
			asmlog.Debugf("picker: batch-confirm clear request failed session=%q err=%v", asmtmux.SessionName, err)
		}
		return resultMsg(exitCode)
	})
	if cmd == nil {
		if err := ClearBatchConfirmRequest(); err != nil {
			asmlog.Debugf("picker: batch-confirm clear request after open failure failed session=%q err=%v", asmtmux.SessionName, err)
		}
	}
	return cmd
}

func (m *PickerModel) openProviderSelect() tea.Cmd {
	return m.runDialogInWorkingPanel("asm-provider-select", []string{"--provider-select"}, func(exitCode int) tea.Msg {
		if exitCode == 0 {
			return providerSelectDoneMsg{ProviderName: asmtmux.GetSessionOption("asm-selected-provider")}
		}
		return providerSelectDoneMsg{}
	})
}

func (m *PickerModel) openLauncher() tea.Cmd {
	launcherPath := launcherHomePath()
	asmlog.Debugf("picker: open-launcher session=%q launcher_path=%q", asmtmux.SessionName, launcherPath)
	return m.runDialogInWorkingPanelAtPath(launcherPath, "asm-launcher", []string{"--launcher"}, func(exitCode int) tea.Msg {
		asmlog.Debugf("picker: launcher-dialog result session=%q exit_code=%d", asmtmux.SessionName, exitCode)
		if exitCode == 0 {
			path := strings.TrimSpace(asmtmux.GetSessionOption("asm-selected-target-path"))
			asmlog.Debugf("picker: launcher-dialog selected path=%q session=%q", path, asmtmux.SessionName)
			if err := asmtmux.SetSessionOption("asm-selected-target-path", ""); err != nil {
				asmlog.Debugf("picker: launcher-dialog clear selected path failed session=%q err=%v", asmtmux.SessionName, err)
			}
			return launcherExitedMsg{Path: path}
		}
		return launcherExitedMsg{}
	})
}

// openIDESelect runs the IDE selector in the working panel. The selected
// IDE name comes back via a tmux session option and is wired through
// ideSelectDoneMsg — which carries the worktree path so the handler can
// launch the IDE against it.
func (m *PickerModel) openIDESelect(wtPath string) tea.Cmd {
	selectCmd := m.runDialogInWorkingPanel("asm-ide-select", []string{"--ide-select"}, func(exitCode int) tea.Msg {
		if exitCode == 0 {
			return ideSelectDoneMsg{
				IDEName: asmtmux.GetSessionOption("asm-selected-ide"),
				Path:    wtPath,
			}
		}
		return ideSelectDoneMsg{Path: wtPath}
	})
	return selectCmd
}

func (m *PickerModel) ensureDirectoryTracked(path string) (*worktree.Worktree, []tea.Cmd) {
	cleanPath := filepath.Clean(path)
	if wt := m.worktreeByPath(cleanPath); wt != nil {
		return wt, nil
	}

	if m.repoRoots == nil {
		m.repoRoots = make(map[string]string)
	}
	if m.repoLabels == nil {
		m.repoLabels = make(map[string]string)
	}
	if m.repoColors == nil {
		m.repoColors = make(map[string]string)
	}
	m.directories = append(m.directories, trackedWorktree(cleanPath))
	mergeRepoMetadataForPaths([]string{cleanPath}, m.repoRoots, m.repoLabels, m.repoColors)
	wt := &m.directories[len(m.directories)-1]

	var cmds []tea.Cmd
	if _, ok := m.branches[cleanPath]; !ok {
		cmds = append(cmds, m.queueBranchFetchForPath(cleanPath))
	}
	return wt, cmds
}

func (m *PickerModel) defaultProviderName(targetPath string) string {
	if cfg, err := config.LoadMerged(targetPath); err == nil && cfg != nil && cfg.DefaultProvider != "" {
		if m.registry.Get(cfg.DefaultProvider) != nil {
			return cfg.DefaultProvider
		}
	}
	return m.registry.Default().Name()
}

func (m *PickerModel) defaultIDEName(targetPath string) string {
	cfg, err := config.LoadMerged(targetPath)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.DefaultIDE
}

// openWorktreeInIDE launches the named IDE against the given path,
// detached. Returns a tea.Cmd so errors can surface via IDEOpenFailedMsg.
func (m *PickerModel) openWorktreeInIDE(wtPath, ideName string) tea.Cmd {
	entry := ide.Find(m.ides, ideName)
	if entry == nil {
		return func() tea.Msg {
			return WorktreeErrorMsg{Err: fmt.Sprintf("unknown IDE: %s", ideName)}
		}
	}
	e := *entry
	return func() tea.Msg {
		if err := e.Open(wtPath); err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("open %s failed: %v", e.Name, err)}
		}
		return nil
	}
}

func (m *PickerModel) openSettings() tea.Cmd {
	settingsPath := m.rootPath
	if wt := m.contextDirectory(); wt != nil {
		settingsPath = wt.Path
	}
	return m.runDialogInWorkingPanelAtPath(settingsPath, "asm-settings", []string{"--settings"}, func(int) tea.Msg {
		return settingsExitedMsg{}
	})
}

func (m *PickerModel) openWorktreeDialog(dir *worktree.Worktree) tea.Cmd {
	// ASM_CONTEXT_PATH is injected by runDialogInWorkingPanel.
	args := []string{"--worktree-create", "--worktree-dir", dir.Path}
	return m.runDialogInWorkingPanelAtPath(dir.Path, "asm-worktree", args, func(exitCode int) tea.Msg {
		path := strings.TrimSpace(asmtmux.GetSessionOption("asm-created-worktree-path"))
		if err := asmtmux.SetSessionOption("asm-created-worktree-path", ""); err != nil {
			asmlog.Debugf("picker: worktree-dialog clear created path failed session=%q err=%v", asmtmux.SessionName, err)
		}
		return worktreeExitedMsg{created: exitCode == 0, path: path}
	})
}

func (m *PickerModel) openDelete(wt *worktree.Worktree) tea.Cmd {
	taskName := ""
	if info, ok := m.taskInfos[wt.Path]; ok {
		taskName = info.Name
	}

	args := []string{"--delete", wt.Name}
	if taskName != "" {
		args = append(args, "--delete-task", taskName)
	}
	if worktree.HasChanges(wt.Path) {
		args = append(args, "--delete-dirty")
	}
	if worktree.IsWorktree(wt.Path) {
		args = append(args, "--delete-worktree")
	}

	wtPath := wt.Path
	return m.runDialogInWorkingPanel("asm-delete", args, func(exitCode int) tea.Msg {
		return deleteExitedMsg{path: wtPath, confirmed: exitCode == 0}
	})
}

// showTerminalInWorkingPanel lazily creates the terminal window for targetPath,
// swaps it into the working pane, and focuses it.
// Returns a tea.Cmd that watches for the terminal's exit (nil if the
// terminal window already existed — its watcher is already running).
func (m *PickerModel) showTerminalInWorkingPanel(targetPath, path string) tea.Cmd {
	var cmd tea.Cmd
	if !asmtmux.WindowExists(asmtmux.TerminalWindowName(targetPath)) {
		asmtmux.CreateTerminalWindow(targetPath, path)
		m.terminalStartTimes[targetPath] = time.Now()
		cmd = waitForTermExitCmd(targetPath)
	}
	if m.swapTermToWorkingPanel(targetPath) {
		m.focusWorkingPanel()
	}
	return cmd
}

func (m *PickerModel) switchToTerminal(wt *worktree.Worktree) tea.Cmd {
	// Already showing this terminal
	if m.termPath == wt.Path {
		m.stabilizeCursor(wt.Path)
		m.focusWorkingPanel()
		return nil
	}

	// Swap out whatever is in the working panel
	m.swapOutWorkingPanel()
	return m.showTerminalInWorkingPanel(wt.Path, wt.Path)
}

func (m *PickerModel) toggleTerminal() tea.Cmd {
	if m.workingPath != "" {
		// AI session is displayed → switch to terminal
		wtPath := m.workingPath
		wt := m.worktreeByPath(wtPath)
		if wt == nil {
			return nil
		}
		m.swapCurrentAIOut()
		return m.showTerminalInWorkingPanel(wtPath, wt.Path)
	} else if m.termPath != "" {
		// Terminal is displayed → switch to AI session (if exists)
		wtPath := m.termPath
		m.swapCurrentTermOut()

		if m.swapAIToWorkingPanel(wtPath) {
			m.focusWorkingPanel()
		}
	}
	return nil
}

func waitForTermExitCmd(targetPath string) tea.Cmd {
	return func() tea.Msg {
		_ = asmtmux.WaitForTermExit(targetPath)
		return terminalExitedMsg{path: targetPath}
	}
}

func (m *PickerModel) showInWorkingPanel(wt *worktree.Worktree) {
	if m.workingPath == wt.Path {
		m.stabilizeCursor(wt.Path)
		m.focusWorkingPanel()
		_ = recent.Record(wt.Path)
		return
	}
	wasZoomed := asmtmux.IsWorkingPanelZoomed()
	// Recreate working panel if it was lost
	asmtmux.EnsureWorkingPanel()
	m.swapOutWorkingPanel()
	if m.swapAIToWorkingPanel(wt.Path) {
		m.focusWorkingPanel()
		if wasZoomed {
			_ = asmtmux.ZoomWorkingPanel()
			m.workingZoomed = true
		}
		_ = recent.Record(wt.Path)
	}
}

func (m *PickerModel) openOrFocusWorktree(wt *worktree.Worktree) tea.Cmd {
	if wt == nil {
		return nil
	}
	if asmtmux.WindowExists(asmtmux.WindowName(wt.Path)) {
		m.showInWorkingPanel(wt)
		return nil
	}
	return m.startSession(wt, m.defaultProviderName(wt.Path), true)
}

func (m *PickerModel) togglePickerPanel() tea.Cmd {
	if asmtmux.IsWorkingPanelZoomed() {
		m.workingZoomed = false
		asmtmux.FocusPickingPanel()
		return nil
	}
	if m.workingPath == "" && m.termPath == "" && !asmtmux.HasUtilityWindow() {
		return nil
	}
	asmtmux.FocusWorkingPanel()
	asmtmux.ZoomWorkingPanel()
	m.workingZoomed = true
	return nil
}

func (m *PickerModel) itemHeight(wi int) int {
	h := 1
	taskInfo, hasTask := m.taskInfos[m.directories[wi].Path]
	hasTask = hasTask && taskInfo.Name != ""
	branch, hasBranch := m.branches[m.directories[wi].Path]
	hasBranch = hasBranch && branch != ""
	if hasTask {
		h += 3
	} else if hasBranch {
		h += 2
	} else {
		h += 1
	}
	return h
}

func (m *PickerModel) itemDisplayHeight(filtered []int, top, fi int) int {
	if fi < 0 || fi >= len(filtered) {
		return 0
	}
	h := m.itemHeight(filtered[fi])
	if fi == top {
		return h + 1
	}
	prevPath := m.directories[filtered[fi-1]].Path
	curPath := m.directories[filtered[fi]].Path
	if m.repoRootForPath(prevPath) != m.repoRootForPath(curPath) {
		h++
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

	for m.viewTop < m.cursor {
		linesUsed := 0
		for fi := m.viewTop; fi <= m.cursor && fi < len(filtered); fi++ {
			linesUsed += m.itemDisplayHeight(filtered, m.viewTop, fi)
		}
		if linesUsed <= maxListLines {
			break
		}
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
		if activeSet[m.directories[i].Path] {
			active = append(active, &m.directories[i])
		}
	}
	if len(active) == 0 {
		return nil
	}

	// Find current position. If nothing is shown, jump to first/last.
	currentPath := m.workingPath
	if currentPath == "" {
		currentPath = m.termPath
	}
	idx := -1
	for i, wt := range active {
		if wt.Path == currentPath {
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
			// Only session is already shown — just focus it.
			m.stabilizeCursor(active[idx].Path)
			asmtmux.FocusWorkingPanel()
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

// Fixed widths (columns) for status bar items.
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

	// Display-column cost of the " │ " separator between items.
	statusLine2SepWidth = 3
	// Maximum number of session items shown at once on line 2.
	statusLine2MaxPerPage = 3
	// How many 200ms scroll ticks each page stays visible (5s).
	statusLine2TicksPerPage = 25
)

// refreshStatusSummary rebuilds the three-line bottom-bar (summary + shortcuts)
// and schedules the tmux writes in the background. Called on scrollTick
// (200ms) — it must be allocation-cheap and never trigger a synchronous tmux
// exec, which would block the Bubble Tea event loop.
//
// Reads come from cached state (m.activeKinds, m.terminalWidth) populated on
// slower cadences. Writes are dispatched as a tea.Cmd only when the rendered
// output actually changed AND no previous write is still in flight — the
// inflight gate keeps a slow tmux server from snowballing goroutines at the
// 5Hz scroll cadence.
func (m *PickerModel) refreshStatusSummary() tea.Cmd {
	if m.statusSummaryWriting {
		// A previous write hasn't returned yet — skip this cycle. The next
		// scrollTick (200ms later) will rebuild against the latest state;
		// the dropped frame is invisible at this cadence.
		return nil
	}

	line1, line1Target := m.buildLine1(m.activeKinds)
	line2 := m.buildLine2(m.activeKinds, line1Target)
	shortcuts := renderShortcutsPlain(len(m.selectedItems), m.contextSupportsWorktree())

	combined := line1 + "\x00" + line2 + "\x00" + shortcuts

	enable := !m.statusBarEnabled
	changed := combined != m.lastStatusSummary
	if enable {
		m.statusBarEnabled = true
	}
	if changed {
		m.lastStatusSummary = combined
	}
	if !enable && !changed {
		return nil
	}
	m.statusSummaryWriting = true
	return writeStatusSummaryCmd(enable, changed, line1, line2, shortcuts)
}

// writeStatusSummaryCmd pushes the three status-bar lines to tmux in a
// goroutine and signals completion so the next scrollTick can run. All
// underlying tmux calls have context timeouts (3s) so this goroutine is
// bounded even if tmux is unresponsive.
func writeStatusSummaryCmd(enable, changed bool, line1, line2, shortcuts string) tea.Cmd {
	return func() tea.Msg {
		if enable {
			asmtmux.EnableStatusBar()
		}
		if changed {
			asmtmux.SetStatusLines(line1, line2)
			asmtmux.SetShortcutsLine(shortcuts)
		}
		return statusSummaryWrittenMsg{}
	}
}

// renderShortcutsPlain returns the shortcuts hint as a plain tmux format string
// (no ANSI, just `#[...]` style markers if needed). When worktree creation
// is not valid for the current context, `^w: worktree` is omitted.
func renderShortcutsPlain(selectedCount int, showWorktree bool) string {
	if selectedCount > 0 {
		return fmt.Sprintf(" %d selected  ^k: kill  ^d: delete  ^l: panel  ^x: toggle  Esc: clear", selectedCount)
	}
	if showWorktree {
		return " ↵: open  ^g: focus  ^l: panel  ^k: kill  ^t: term  ^n: launch  ^]: rotate  ^x: select  ^o: task  ^e: IDE  ^p: AI  ^w: worktree  ^d: remove  ^s: settings  ^q: quit"
	}
	return " ↵: open  ^g: focus  ^l: panel  ^k: kill  ^t: term  ^n: launch  ^]: rotate  ^x: select  ^o: task  ^e: IDE  ^p: AI  ^d: remove  ^s: settings  ^q: quit"
}

// buildLine1 returns the detailed line for the currently displayed session and
// the resolved target path (so line2 can skip it). Resolution order:
//  1. m.workingPath (AI currently in working panel)
//  2. m.termPath    (terminal currently in working panel)
//  3. first active session in scan order (AI preferred, then terminal)
func (m *PickerModel) buildLine1(activeKinds map[string]asmtmux.SessionKind) (string, string) {
	target := ""
	var targetKind asmtmux.SessionKind
	switch {
	case m.workingPath != "" && activeKinds[m.workingPath] != 0:
		target = m.workingPath
	case m.termPath != "" && activeKinds[m.termPath] != 0:
		target = m.termPath
	default:
		// Fallback: first active AI, else first active terminal, in scan order
		for _, wt := range m.directories {
			if activeKinds[wt.Path].HasAI() {
				target = wt.Path
				break
			}
		}
		if target == "" {
			for _, wt := range m.directories {
				if activeKinds[wt.Path].HasTerm() {
					target = wt.Path
					break
				}
			}
		}
	}
	if target == "" {
		return "", ""
	}
	targetKind = activeKinds[target]

	wt := m.worktreeByPath(target)
	if wt == nil {
		return "", ""
	}

	// What is displayed determines which columns we fill. If working panel has the
	// AI session (or no panel selection and an AI exists), show AI state. Otherwise
	// the terminal is what the user sees: no task/state, but badge still reflects
	// the full kind (so a worktree with a+t shows "[a+t]" even when term is fronted).
	displayingTerm := (m.workingPath == "" && m.termPath == target) || !targetKind.HasAI()

	// Compute the task-name column width to fill the available terminal width.
	taskWidth := m.terminalWidth - statusLine1ChromeWidth - statusLine1NameWidth
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
	statePart, stateColor := m.statePaddedColumn(wt.Path, !displayingTerm)

	// Elapsed — AI session time when AI is displayed, else terminal open time.
	elapsed := ""
	if displayingTerm {
		if t, ok := m.terminalStartTimes[wt.Path]; ok {
			elapsed = formatElapsed(time.Since(t))
		}
	} else {
		if t, ok := m.sessionStartTimes[wt.Path]; ok {
			elapsed = formatElapsed(time.Since(t))
		}
	}
	elapsedPart := padToWidth(elapsed, 6)

	// Render with tmux format codes
	icon := "#[fg=" + tmuxToolUseColor + ",bold]▶#[default]"
	sep := "#[fg=" + tmuxDimColor + "]│#[default]"
	return fmt.Sprintf(" %s #[fg=%s,bold]%s#[default] %s %s #[fg=%s]%s#[default] %s #[fg=%s,bold]%s#[default] #[fg=%s]%s#[default] ",
		icon, tmuxTextColor, folder, badge, sep, tmuxPrimaryColor, taskPart, sep, stateColor, statePart, tmuxDimColor, elapsedPart), target
}

// buildLine2 returns the active-session overview as an adaptive strip:
//
//	1 item  -> full width
//	2 items -> half / half
//	3 items -> thirds
//	4+      -> paged in groups of 3
//
// The session already shown on line 1 is excluded.
func (m *PickerModel) buildLine2(activeKinds map[string]asmtmux.SessionKind, line1Target string) string {
	type statusLineItem struct {
		wt   worktree.Worktree
		kind asmtmux.SessionKind
	}

	var items []statusLineItem
	for _, wt := range m.directories {
		kind := activeKinds[wt.Path]
		if kind == 0 || wt.Path == line1Target {
			continue
		}
		items = append(items, statusLineItem{wt: wt, kind: kind})
	}
	if len(items) == 0 {
		return ""
	}
	sep := "#[fg=" + tmuxDimColor + "] │ #[default]"

	numPages := (len(items) + statusLine2MaxPerPage - 1) / statusLine2MaxPerPage
	page := (m.scrollTick / statusLine2TicksPerPage) % numPages
	start := page * statusLine2MaxPerPage
	end := start + statusLine2MaxPerPage
	if end > len(items) {
		end = len(items)
	}
	visible := items[start:end]

	prefix := " "
	prefixWidth := 1
	if numPages > 1 {
		rawIndicator := fmt.Sprintf("%d/%d", page+1, numPages)
		prefix += fmt.Sprintf("#[fg=%s](%s)#[default] ", tmuxDimColor, rawIndicator)
		prefixWidth += lipgloss.Width(rawIndicator) + 3
	}

	availableWidth := m.terminalWidth - prefixWidth - 1 - statusLine2SepWidth*(len(visible)-1)
	if availableWidth < len(visible) {
		availableWidth = len(visible)
	}
	cellWidths := splitEvenWidths(availableWidth, len(visible))

	parts := make([]string, 0, len(visible))
	for i, item := range visible {
		parts = append(parts, m.renderStatusItem(item.wt, item.kind, cellWidths[i]))
	}
	return prefix + strings.Join(parts, sep) + " "
}

// renderStatusItem renders one active session into an exact-width line2 cell.
// The cell expands to fill its page slot instead of using a global fixed width.
func (m *PickerModel) renderStatusItem(wt worktree.Worktree, kind asmtmux.SessionKind, width int) string {
	if width < 1 {
		width = 1
	}

	// Name: task name if resolved, otherwise folder name
	rawName := wt.Name
	if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" {
		rawName = info.Name
	}

	stateWidth := 0
	if kind.HasAI() && width >= 32 {
		stateWidth = statusLine2StateWidth
	}
	nameWidth := width - 1 - 1 - statusLine2KindWidth
	if stateWidth > 0 {
		nameWidth -= 1 + stateWidth
	}
	if nameWidth < 1 {
		nameWidth = 1
	}

	displayName := m.scrollPadName(rawName, nameWidth)
	badge := renderKindBadgeTmuxPadded(kind)

	cell := fmt.Sprintf("#[fg=%s]●#[default] #[fg=%s]%s#[default] %s",
		tmuxActiveColor, tmuxTextColor, displayName, badge)
	if stateWidth > 0 {
		stateLabel, stateColor := m.stateLabelColor(wt.Path)
		cell += fmt.Sprintf(" #[fg=%s]%s#[default]", stateColor, padToWidth(stateLabel, stateWidth))
	}
	return cell
}

func renderKindBadgeTmuxPadded(kind asmtmux.SessionKind) string {
	rawWidth := 0
	switch {
	case kind.HasAI() && kind.HasTerm():
		rawWidth = 5
	case kind.HasAI() || kind.HasTerm():
		rawWidth = 3
	}
	if rawWidth == 0 {
		return strings.Repeat(" ", statusLine2KindWidth)
	}
	return renderKindBadgeTmux(kind) + strings.Repeat(" ", statusLine2KindWidth-rawWidth)
}

func splitEvenWidths(total, parts int) []int {
	if parts <= 0 {
		return nil
	}
	if total < parts {
		total = parts
	}
	base := total / parts
	rem := total % parts
	widths := make([]int, parts)
	for i := range widths {
		widths[i] = base
		if i < rem {
			widths[i]++
		}
	}
	return widths
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
func (m *PickerModel) statePaddedColumn(targetPath string, showState bool) (string, string) {
	label, color := "", tmuxDimColor
	if showState {
		label, color = m.stateLabelColor(targetPath)
	}
	return padToWidth(label, statusLine2StateWidth), color
}

// stateLabelColor returns the provider-state label and tmux color code for a session.
func (m *PickerModel) stateLabelColor(targetPath string) (string, string) {
	if _, flashing := m.flashItems[targetPath]; flashing {
		return "done", tmuxActiveColor
	}
	st, ok := m.providerStates[targetPath]
	if !ok {
		return "", tmuxDimColor
	}
	switch st {
	case provider.StateThinking:
		return st.Label(), tmuxWarnColor
	case provider.StateResponding:
		return st.Label(), tmuxRespondingColor
	case provider.StateToolUse:
		return st.Label(), tmuxToolUseColor
	case provider.StateBusy:
		return st.Label(), tmuxWarnColor
	case provider.StateIdle:
		return st.Label(), tmuxDimColor
	}
	return st.Label(), tmuxDimColor
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

// stabilizeCursor repositions m.cursor onto the given target path after a sort change.
// No-op if path is empty or not found.
func (m *PickerModel) stabilizeCursor(targetPath string) {
	if targetPath == "" {
		return
	}
	filtered := m.filteredDirectories()
	for fi, wi := range filtered {
		if m.directories[wi].Path == targetPath {
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

func (m *PickerModel) frontDirectory() *worktree.Worktree {
	wtPath := m.workingPath
	if wtPath == "" {
		wtPath = m.termPath
	}
	if wtPath == "" {
		return nil
	}
	for i := range m.directories {
		if m.directories[i].Path == wtPath {
			return &m.directories[i]
		}
	}
	return nil
}

// contextDirectory resolves the target for context-sensitive actions.
// When the picker pane is focused, the cursor selection wins. When the
// working pane is focused, the currently fronted target wins.
func (m *PickerModel) contextDirectory() *worktree.Worktree {
	if m.focused {
		if wt := m.selectedDirectory(); wt != nil {
			return wt
		}
	}
	if wt := m.frontDirectory(); wt != nil {
		return wt
	}
	return m.selectedDirectory()
}

func (m *PickerModel) contextSupportsWorktree() bool {
	wt := m.contextDirectory()
	if wt == nil {
		return false
	}
	return worktree.IsRepoMode(wt.Path)
}

func (m PickerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// View() is re-entered for every terminal repaint; read the cached map
	// rather than spawning a tmux exec on each render.
	activeKinds := m.activeKinds

	filtered := m.filteredDirectories()
	searchLine := ""
	if m.searchQuery != "" {
		searchLine = lipgloss.NewStyle().Foreground(primaryColor).Padding(0, 1).Render("/ " + m.searchQuery)
	}
	if len(filtered) == 0 {
		var body []string
		if searchLine != "" {
			body = append(body, searchLine)
		}
		body = append(body, "")
		if len(m.directories) == 0 {
			body = append(body, "No open sessions", "", "Press Ctrl+N to launch a session")
		} else {
			body = append(body, "No matching sessions")
		}
		empty := body
		for len(empty) < m.height {
			empty = append(empty, "")
		}
		view := strings.Join(empty[:m.height], "\n")
		return m.renderErrorOverlay(view)
	}

	// Available lines for the list. Shortcuts bar is
	// rendered by tmux at the bottom of the terminal, outside this pane.
	maxListLines := m.height
	if m.searchQuery != "" {
		maxListLines-- // search bar takes one line
	}
	if maxListLines < 1 {
		maxListLines = 1
	}

	// Build visible list with viewport scrolling
	var visibleRows []string
	usedLines := 0
	for fi := m.viewTop; fi < len(filtered); fi++ {
		wi := filtered[fi]
		wt := m.directories[wi]
		text := m.renderItem(fi, wt, activeKinds[wt.Path])
		if fi == m.viewTop || m.repoRootForPath(m.directories[filtered[fi-1]].Path) != m.repoRootForPath(wt.Path) {
			text = m.renderRepoHeader(wt.Path) + "\n" + text
		}
		lineCount := strings.Count(text, "\n") + 1
		if usedLines+lineCount > maxListLines {
			break
		}
		visibleRows = append(visibleRows, text)
		usedLines += lineCount
	}

	// Build view: search bar + list + padding
	var viewLines []string
	if searchLine != "" {
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
	return m.renderErrorOverlay(view)
}

func (m PickerModel) renderItem(index int, wt worktree.Worktree, kind asmtmux.SessionKind) string {
	isSelected := index == m.cursor
	hasSession := kind != 0

	dimmed := !m.focused && !isSelected

	indicator := inactiveSessionStyle.String()
	if hasSession {
		indicator = activeSessionStyle.String()
	}
	if m.workingPath == wt.Path || m.termPath == wt.Path {
		indicator = lipgloss.NewStyle().Foreground(activeColor).Bold(true).Render("●")
	}
	if isSelected {
		indicator = lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("●")
	}
	if dimmed {
		if hasSession || m.workingPath == wt.Path || m.termPath == wt.Path {
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
	branch, hasBranch := m.branches[wt.Path]
	hasBranch = hasBranch && branch != ""
	repoAccent := m.repoAccentForPath(wt.Path)

	primaryStyle := lipgloss.NewStyle().Foreground(repoAccent)
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
		if m.selectedItems[wt.Path] {
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
			subLines = append(subLines, gitStatusStyle.Render(branch))
		}
	} else if hasBranch {
		rawName = wt.Name
		subLines = append(subLines, gitStatusStyle.Render(branch))
	} else {
		rawName = wt.Name
	}

	// State as sub-line (prepend to subLines)
	var stateLine string
	switch {
	case kind.HasAI():
		if _, flashing := m.flashItems[wt.Path]; flashing {
			stateLine = CompletionFlashStyle.Render("✓ done!")
		} else if state, ok := m.providerStates[wt.Path]; ok {
			stateLine = m.renderProviderState(state, wt.Path, m.spinnerFrame)
		}
		if startTime, ok := m.sessionStartTimes[wt.Path]; ok {
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
		if startTime, ok := m.terminalStartTimes[wt.Path]; ok {
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
		lipgloss.WithWhitespaceForeground(surfaceTextColor),
	)
}

func (m PickerModel) renderErrorOverlay(view string) string {
	if m.err == "" {
		return view
	}
	errDialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dangerColor).
		Padding(1, 2).
		Width(min(50, m.width-4)).
		Render(
			lipgloss.NewStyle().Bold(true).Foreground(dangerColor).Render("Error") + "\n\n" +
				lipgloss.NewStyle().Foreground(whiteColor).Render(m.err) + "\n\n" +
				statusBarStyle.Render("Press any key to dismiss"))
	return m.overlayCenter(view, errDialog)
}

// Commands

func (m PickerModel) scanDirectories() tea.Cmd {
	cache := m.taskCache
	return func() tea.Msg {
		activeKinds := asmtmux.ListActiveSessions()
		var paths []string
		for path := range activeKinds {
			paths = append(paths, path)
		}
		sort.Slice(paths, func(i, j int) bool {
			baseI := filepath.Base(paths[i])
			baseJ := filepath.Base(paths[j])
			if baseI == baseJ {
				return paths[i] < paths[j]
			}
			return baseI < baseJ
		})

		wts := make([]worktree.Worktree, 0, len(paths))
		for _, path := range paths {
			wts = append(wts, worktree.Worktree{
				Name: filepath.Base(path),
				Path: path,
			})
		}
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
		repoRoots, repoLabels, repoColors := repoMetadataForPaths(paths)
		return DirectoriesScannedMsg{
			Directories:    wts,
			CachedTasks:    tasks,
			CachedBranches: branches,
			RepoRoots:      repoRoots,
			RepoLabels:     repoLabels,
			RepoColors:     repoColors,
		}
	}
}

func (m PickerModel) fetchBranch(path string) tea.Cmd {
	return func() tea.Msg {
		return BranchResolvedMsg{Path: path, Branch: worktree.CurrentBranch(path)}
	}
}

func (m PickerModel) fetchTaskName(path string, branch string) tea.Cmd {
	t := m.tracker
	return func() tea.Msg {
		info := t.Resolve(branch)
		return TaskResolvedMsg{Path: path, Branch: branch, Info: info}
	}
}

func flashExpireCmd(targetPath string, startedAt time.Time, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(t time.Time) tea.Msg {
		return flashExpiredMsg{Path: targetPath, StartedAt: startedAt}
	})
}

// notifyCompletionCmd sends a desktop notification when an AI session finishes.
// displayName is the resolved title (task name preferred, falling back to folder).
func notifyCompletionCmd(targetPath, displayName, providerName, sessionName, currentWorkingPath string) tea.Cmd {
	return func() tea.Msg {
		isDisplayed := currentWorkingPath == targetPath
		content, err := asmtmux.CapturePaneHistory(targetPath, isDisplayed, 80)
		title := "ASM – " + displayName
		if err != nil {
			notification.SendRequest(notification.Request{
				Title:       title,
				Body:        "done",
				Provider:    providerName,
				SessionName: sessionName,
			})
			return nil
		}
		snippet := extractLastResponse(content)
		body := "done"
		if snippet != "" {
			body = snippet
		}
		notification.SendRequest(notification.Request{
			Title:       title,
			Body:        body,
			Provider:    providerName,
			SessionName: sessionName,
		})
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
		raw := strings.TrimSpace(lines[i])
		if raw == "" {
			continue
		}
		if isNoiseeLine(raw) {
			continue
		}
		stripped := stripBoxBorders(raw)
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
	text = strings.ToValidUTF8(text, "")

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
	if isPromptLine(trimmed) || isModelFooterLine(trimmed) {
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

func isPromptLine(line string) bool {
	for _, prefix := range []string{"›", "❯", ">"} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return rest != ""
	}
	return false
}

func isModelFooterLine(line string) bool {
	return strings.Contains(line, "· ~/") || strings.Contains(line, "· /")
}

// providerStateDelay is the fixed gap between a detect-state cycle finishing
// and the next one starting. Using a post-completion delay (as opposed to a
// pure tea.Tick every second) prevents fan-out when DetectState or tmux is
// slow: a new cycle only begins after every in-flight probe has reported.
const providerStateDelay = time.Second

func providerStateTickCmd() tea.Cmd {
	return tea.Tick(providerStateDelay, func(t time.Time) tea.Msg {
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

// sessionHealthTickCmd schedules the next tmux-session liveness probe. 5s is
// frequent enough to catch an orphaned picker quickly, cheap enough that the
// extra tmux exec is negligible.
func sessionHealthTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return sessionHealthTickMsg(t)
	})
}

func terminalLayoutTickCmd() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(t time.Time) tea.Msg {
		return terminalLayoutTickMsg(t)
	})
}

// fetchTerminalLayoutCmd measures the tmux client width and current zoom flag
// in a goroutine. On width lookup failure width is 0 and the model keeps its
// previous width; the zoom flag still helps us decide whether a deferred pane
// resize can be applied now.
func fetchTerminalLayoutCmd() tea.Cmd {
	return func() tea.Msg {
		return terminalLayoutResolvedMsg{
			width:         asmtmux.TerminalWidth(),
			zoomed:        asmtmux.IsWorkingPanelZoomed(),
			clientFocused: asmtmux.ClientFocused(),
		}
	}
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

func (m PickerModel) fetchProviderState(targetPath string) tea.Cmd {
	currentWT := m.workingPath
	providerName := m.worktreeProviders[targetPath]
	p := m.registry.Get(providerName)
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		isDisplayed := currentWT == targetPath
		title, err := asmtmux.GetPaneTitle(targetPath, isDisplayed)
		if err != nil {
			return ProviderStateUpdatedMsg{Path: targetPath, State: provider.StateUnknown}
		}

		var content string
		if p.NeedsContent(title) {
			content, _ = asmtmux.CapturePaneContent(targetPath, isDisplayed)
		}

		state := p.DetectState(title, content)
		return ProviderStateUpdatedMsg{Path: targetPath, State: state}
	}
}

func (m PickerModel) renderProviderState(state provider.State, targetPath string, frame int) string {
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
		if pName := m.worktreeProviders[targetPath]; pName != "" {
			if p := m.registry.Get(pName); p != nil {
				label = p.DisplayName() + " " + label
			}
		}
	}
	return style.Render(spinner + " " + label)
}
