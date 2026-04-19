package ui

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/asm/asmlog"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/ide"
	"github.com/nhn/asm/provider"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/tracker"
)

func (m PickerModel) handleProviderStateTick() (tea.Model, tea.Cmd) {
	cursorPath := ""
	if wt := m.selectedDirectory(); wt != nil {
		cursorPath = wt.Path
	}

	var cmds []tea.Cmd
	pending := 0
	activeKinds := asmtmux.ListActiveSessions()
	m.activeKinds = activeKinds
	for _, wt := range m.directories {
		kind := activeKinds[wt.Path]
		if kind.HasAI() {
			if _, tracked := m.sessionStartTimes[wt.Path]; !tracked {
				m.sessionStartTimes[wt.Path] = time.Now()
			}
			if _, known := m.worktreeProviders[wt.Path]; !known {
				stored := asmtmux.GetWindowOption(wt.Path, "asm-provider")
				if stored != "" {
					m.worktreeProviders[wt.Path] = stored
				} else {
					m.worktreeProviders[wt.Path] = m.registry.Default().Name()
				}
			}
			if cmd := m.fetchProviderState(wt.Path); cmd != nil {
				cmds = append(cmds, cmd)
				pending++
			}
		}
		if kind.HasTerm() {
			if _, tracked := m.terminalStartTimes[wt.Path]; !tracked {
				m.terminalStartTimes[wt.Path] = time.Now()
			}
		}
	}
	m.stabilizeCursor(cursorPath)
	if cmd := m.persistSessionSnapshotCmd(activeKinds); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if pending == 0 {
		cmds = append(cmds, providerStateTickCmd())
		return m, tea.Batch(cmds...)
	}
	m.providerStatePending = pending
	return m, tea.Batch(cmds...)
}

func (m PickerModel) handleProviderStateUpdated(msg ProviderStateUpdatedMsg) (tea.Model, tea.Cmd) {
	var extraCmds []tea.Cmd
	if msg.State != provider.StateUnknown {
		prevState := m.prevProviderStates[msg.Path]
		m.providerStates[msg.Path] = msg.State
		m.prevProviderStates[msg.Path] = msg.State
		notifyReady := m.providerNotifyReady[msg.Path]

		if msg.State == provider.StateIdle && !notifyReady {
			m.providerNotifyReady[msg.Path] = true
		}

		if prevState.IsBusy() && msg.State == provider.StateIdle {
			if notifyReady {
				now := time.Now()
				m.flashItems[msg.Path] = now
				extraCmds = append(extraCmds, flashExpireCmd(msg.Path, now, 3*time.Second))
				if m.cfg.IsDesktopNotificationsEnabled() {
					displayName := filepath.Base(msg.Path)
					if wt := m.worktreeByPath(msg.Path); wt != nil {
						displayName = wt.Name
						if info, ok := m.taskInfos[wt.Path]; ok && info.Name != "" {
							displayName = info.Name
						}
					}
					providerName := m.worktreeProviders[msg.Path]
					if providerName == "" && m.registry != nil && m.registry.Default() != nil {
						providerName = m.registry.Default().Name()
					}
					extraCmds = append(extraCmds, notifyCompletionCmd(msg.Path, displayName, providerName, m.notificationHookForProvider(providerName), asmtmux.SessionName, m.workingPath))
				}
			}
		}
	}
	if m.providerStatePending > 0 {
		m.providerStatePending--
		if m.providerStatePending == 0 {
			extraCmds = append(extraCmds, providerStateTickCmd())
		}
	}
	if len(extraCmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(extraCmds...)
}

func (m PickerModel) handleSessionExited(msg sessionExitedMsg) (tea.Model, tea.Cmd) {
	m.cleanupSessionState(msg.Path)
	isDisplayed := m.workingPath == msg.Path
	asmtmux.CleanupExitedWindow(msg.Path, isDisplayed)
	if isDisplayed {
		m.workingPath = ""
		if !m.swapTermToWorkingPanel(msg.Path) {
			asmtmux.FocusPickingPanel()
		}
	}
	return m, m.refreshDirectoriesCmd()
}

func (m PickerModel) handleWorktreeExited(msg worktreeExitedMsg) (tea.Model, tea.Cmd) {
	if !msg.created {
		return m, nil
	}
	if w := asmtmux.GetSessionOption("asm-worktree-warnings"); w != "" {
		m.err = "Template copy issues:\n" + w
	}
	_ = asmtmux.SetSessionOption("asm-worktree-warnings", "")
	_ = asmtmux.SetSessionOption("asm-worktree-copied", "")
	if msg.path == "" {
		return m, m.refreshDirectoriesCmd()
	}
	wt, extraCmds := m.ensureDirectoryTracked(msg.path)
	if wt == nil {
		return m, m.refreshDirectoriesCmd()
	}
	var cmds []tea.Cmd
	cmds = append(cmds, extraCmds...)
	if cmd := m.openOrFocusWorktree(wt); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m PickerModel) handleDeleteExited(msg deleteExitedMsg) (tea.Model, tea.Cmd) {
	if !msg.confirmed {
		return m, nil
	}
	wt := m.worktreeByPath(msg.path)
	if wt == nil {
		return m, nil
	}
	_, focusedTargetKilled := m.killTargetSessions(wt.Path)
	if focusedTargetKilled {
		asmtmux.FocusPickingPanel()
	}
	return m, m.removeDirectory(wt)
}

func (m PickerModel) handleLauncherExited(msg launcherExitedMsg) (tea.Model, tea.Cmd) {
	asmlog.Debugf("picker: launcher-exited session=%q path=%q", asmtmux.SessionName, msg.Path)
	if msg.Path == "" {
		return m, nil
	}
	wt, extraCmds := m.ensureDirectoryTracked(msg.Path)
	if wt == nil {
		asmlog.Debugf("picker: launcher-exited ignored untrackable path=%q", msg.Path)
		return m, nil
	}
	windowExists := asmtmux.WindowExists(asmtmux.WindowName(wt.Path))
	asmlog.Debugf("picker: launcher target=%q window_exists=%t extra_cmds=%d provider=%q",
		wt.Path, windowExists, len(extraCmds), m.defaultProviderName(wt.Path))
	if cmd := m.openOrFocusWorktree(wt); cmd != nil {
		extraCmds = append(extraCmds, cmd)
	}
	if len(extraCmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(extraCmds...)
}

func (m PickerModel) handleProviderSelectDone(msg providerSelectDoneMsg) (tea.Model, tea.Cmd) {
	if msg.ProviderName == "" {
		return m, nil
	}
	wt := m.contextDirectory()
	if wt == nil {
		return m, nil
	}
	winName := asmtmux.WindowName(wt.Path)
	if asmtmux.WindowExists(winName) {
		if m.workingPath == wt.Path {
			m.swapCurrentAIOut()
		}
		asmtmux.KillDirectoryWindow(wt.Path)
	}
	m.cleanupSessionState(wt.Path)
	return m, m.startSession(wt, msg.ProviderName, true)
}

func (m PickerModel) handleDirectoriesScanned(msg DirectoriesScannedMsg) (tea.Model, tea.Cmd) {
	m.directories = msg.Directories
	m.repoRoots = msg.RepoRoots
	m.repoLabels = msg.RepoLabels
	m.repoColors = msg.RepoColors

	validPaths := make(map[string]bool, len(msg.Directories))
	for _, wt := range msg.Directories {
		validPaths[wt.Path] = true
	}
	for p, info := range msg.CachedTasks {
		if validPaths[p] {
			if _, exists := m.taskInfos[p]; !exists {
				m.taskInfos[p] = info
			}
		}
	}
	for p, b := range msg.CachedBranches {
		if validPaths[p] {
			m.cachedBranches[p] = b
			if b != "" && m.branches[p] == "" {
				m.branches[p] = b
			}
		}
	}
	m.pruneMetadataQueues(validPaths)

	for path := range m.selectedItems {
		if !validPaths[path] {
			delete(m.selectedItems, path)
		}
	}

	filtered := m.filteredDirectories()
	if m.cursor >= len(filtered) {
		m.cursor = max(0, len(filtered)-1)
	}
	m.viewTop = 0
	for _, wt := range msg.Directories {
		m.enqueueBranchFetch(wt.Path)
	}
	return m, m.startNextMetadataFetches()
}

func (m PickerModel) handleBranchResolved(msg BranchResolvedMsg) (tea.Model, tea.Cmd) {
	m.branchFetchPending = false
	if m.worktreeByPath(msg.Path) == nil {
		return m, m.startNextMetadataFetches()
	}
	m.branchVerified[msg.Path] = true
	if msg.Branch == "" {
		delete(m.branches, msg.Path)
		return m, m.startNextMetadataFetches()
	}
	m.branches[msg.Path] = msg.Branch
	if m.tracker != nil || m.trackerService != nil {
		if seeded, ok := m.cachedBranches[msg.Path]; ok && seeded != msg.Branch {
			delete(m.taskInfos, msg.Path)
			delete(m.cachedBranches, msg.Path)
			if m.trackerService != nil {
				m.trackerService.Delete(msg.Path)
				if info, ok := m.trackerService.Peek(msg.Path, msg.Branch); ok {
					m.taskInfos[msg.Path] = info
					m.cachedBranches[msg.Path] = msg.Branch
					m.trackerService.Set(msg.Path, msg.Branch, info)
				}
			} else if m.taskCache != nil {
				m.taskCache.Delete(msg.Path)
				if info, ok := m.taskCache.Peek(msg.Branch); ok {
					m.taskInfos[msg.Path] = info
					m.cachedBranches[msg.Path] = msg.Branch
					m.taskCache.Set(msg.Path, msg.Branch, info)
				}
			}
		}
		if _, ok := m.taskInfos[msg.Path]; !ok {
			m.enqueueTaskFetch(msg.Path, msg.Branch)
		}
	}
	return m, m.startNextMetadataFetches()
}

func (m PickerModel) handleTaskPoll() (tea.Model, tea.Cmd) {
	m.taskPollScheduled = false
	for _, result := range m.taskResults.Drain() {
		if !m.taskFetch.Finish(queuedTaskResolve{Path: result.Path, Branch: result.Branch}) {
			continue
		}
		m.applyTaskResolved(result)
	}
	var cmds []tea.Cmd
	if cmd := m.startNextMetadataFetches(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *PickerModel) applyTaskResolved(msg TaskResolvedMsg) {
	if m.worktreeByPath(msg.Path) == nil {
		return
	}
	if currentBranch, ok := m.branches[msg.Path]; ok {
		if msg.Branch != "" && currentBranch != msg.Branch {
			return
		}
	} else if m.branchVerified[msg.Path] {
		return
	}
	if msg.Info.Name != "" {
		m.taskInfos[msg.Path] = msg.Info
		if msg.Branch != "" {
			m.cachedBranches[msg.Path] = msg.Branch
			if m.trackerService != nil {
				m.trackerService.Set(msg.Path, msg.Branch, msg.Info)
			} else if m.taskCache != nil {
				m.taskCache.Set(msg.Path, msg.Branch, msg.Info)
			}
		}
	}
}

func (m PickerModel) handleSettingsExited() (tea.Model, tea.Cmd) {
	settingsPath := m.rootPath
	if wt := m.contextDirectory(); wt != nil {
		settingsPath = wt.Path
	}
	newCfg, err := config.LoadMerged(settingsPath)
	if err != nil {
		return m, nil
	}
	ApplyTheme(newCfg.ThemeMode())
	m.cfg = newCfg
	defaultName := newCfg.DefaultProvider
	if defaultName == "" {
		defaultName = provider.DefaultProviderName
	}
	m.registry.SetDefault(defaultName)
	overrides := make(map[string]ide.Override, len(newCfg.IDEs))
	for name, c := range newCfg.IDEs {
		overrides[name] = ide.Override{Command: c.Command, Args: c.Args}
	}
	m.ides = ide.Builtins(overrides)
	m.taskInfos = make(map[string]tracker.TaskInfo)
	m.cachedBranches = make(map[string]string)
	m.branchVerified = make(map[string]bool)
	if m.taskResults != nil {
		m.taskResults.Clear()
	}
	m.taskPollScheduled = false
	m.pickerWidthDirty = true
	return m, m.refreshLayoutAndDirectoriesCmd()
}

func (m PickerModel) handleTerminalExited(msg terminalExitedMsg) (tea.Model, tea.Cmd) {
	isDisplayed := m.termPath == msg.path
	if isDisplayed {
		m.swapCurrentTermOut()
	}
	asmtmux.KillTerminalWindow(msg.path)
	delete(m.terminalStartTimes, msg.path)
	if isDisplayed {
		if m.swapAIToWorkingPanel(msg.path) {
			m.focusWorkingPanel()
		} else {
			asmtmux.FocusPickingPanel()
		}
	}
	return m, m.refreshDirectoriesCmd()
}
