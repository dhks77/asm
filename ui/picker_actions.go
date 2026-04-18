package ui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/trash"
	"github.com/nhn/asm/worktree"
)

func (m *PickerModel) clearSelection() {
	m.selectedItems = make(map[string]bool)
}

func (m *PickerModel) selectedItemPaths() []string {
	var paths []string
	for path := range m.selectedItems {
		paths = append(paths, path)
	}
	return paths
}

func (m *PickerModel) openBatchKill() tea.Cmd {
	return m.openKillConfirm(m.selectedItemPaths())
}

func (m *PickerModel) openKillSession() tea.Cmd {
	wt := m.selectedDirectory()
	if wt == nil {
		return nil
	}
	return m.openKillConfirm([]string{wt.Path})
}

func (m *PickerModel) openKillConfirm(paths []string) tea.Cmd {
	req := BatchConfirmRequest{
		Action:    BatchKillSessions,
		Items:     paths,
		TaskNames: m.taskNamesFor(paths),
	}
	return m.openBatchConfirmDialog(req, func(exitCode int) tea.Msg {
		if exitCode == 0 {
			return BatchConfirmedMsg{Action: BatchKillSessions, Items: paths}
		}
		return BatchCancelledMsg{}
	})
}

func (m *PickerModel) openBatchDelete() tea.Cmd {
	paths := m.selectedItemPaths()
	req := BatchConfirmRequest{
		Action:    BatchDeleteWorktrees,
		Items:     paths,
		TaskNames: m.taskNamesFor(paths),
		Dirty:     m.countDirtyTargets(paths),
	}
	return m.openBatchConfirmDialog(req, func(exitCode int) tea.Msg {
		if exitCode == 0 {
			return BatchConfirmedMsg{Action: BatchDeleteWorktrees, Items: paths}
		}
		return BatchCancelledMsg{}
	})
}

func (m *PickerModel) countDirtyTargets(paths []string) int {
	dirtyCount := 0
	for _, path := range paths {
		if wt := m.worktreeByPath(path); wt != nil && worktree.HasChanges(wt.Path) {
			dirtyCount++
		}
	}
	return dirtyCount
}

// taskNamesFor returns a parallel slice of resolved task names for the
// given target paths (empty string when no info is cached). Used by the
// batch-confirm dialog so users see task titles, not just folder names.
func (m *PickerModel) taskNamesFor(paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		if wt := m.worktreeByPath(path); wt != nil {
			if info, ok := m.taskInfos[wt.Path]; ok {
				out[i] = info.Name
			}
		}
	}
	return out
}

func (m *PickerModel) batchKillSessions(paths []string) tea.Cmd {
	count := 0
	focusedTargetKilled := false
	for _, path := range paths {
		killed, displayed := m.killTargetSessions(path)
		count += killed
		if displayed {
			focusedTargetKilled = true
		}
	}
	if focusedTargetKilled {
		asmtmux.FocusPickingPanel()
	}
	return func() tea.Msg {
		return batchKillCompletedMsg{count: count}
	}
}

func (m *PickerModel) batchDeleteWorktrees(paths []string) tea.Cmd {
	focusedTargetKilled := false
	for _, path := range paths {
		_, displayed := m.killTargetSessions(path)
		if displayed {
			focusedTargetKilled = true
		}
	}
	if focusedTargetKilled {
		asmtmux.FocusPickingPanel()
	}

	var toRemove []worktree.Worktree
	for _, path := range paths {
		if wt := m.worktreeByPath(path); wt != nil {
			toRemove = append(toRemove, *wt)
		}
	}

	return func() tea.Msg {
		count := 0
		for _, wt := range toRemove {
			if removeTargetPath(wt.Path) == nil {
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
		if err := removeTargetPath(dirPath); err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("remove failed: %v", err)}
		}
		return DirectoryRemovedMsg{}
	}
}

func removeTargetPath(targetPath string) error {
	if worktree.IsWorktree(targetPath) {
		mainRepo, err := worktree.FindMainRepo(targetPath)
		if err == nil {
			if err := worktree.RemoveWorktree(mainRepo, targetPath, false); err != nil {
				if forceErr := worktree.RemoveWorktree(mainRepo, targetPath, true); forceErr != nil {
					return forceErr
				}
			}
			return nil
		}
	}
	return trash.Move(targetPath)
}

// cleanupSessionState removes per-session bookkeeping for the given target
// path. Called whenever a session ends or is about to be restarted so stale
// state doesn't leak into the next session (e.g. frozen provider state,
// leftover "done!" flash, old start time).
func (m *PickerModel) cleanupSessionState(path string) {
	delete(m.providerStates, path)
	delete(m.prevProviderStates, path)
	delete(m.worktreeProviders, path)
	delete(m.sessionStartTimes, path)
	delete(m.flashItems, path)
}

// killTargetSessions tears down both AI and terminal sessions for a target
// path, including any working-pane/front-state bookkeeping. Returns how many
// tmux windows were killed and whether the target had been fronted.
func (m *PickerModel) killTargetSessions(path string) (int, bool) {
	hasAI := asmtmux.WindowExists(asmtmux.WindowName(path))
	hasTerm := asmtmux.WindowExists(asmtmux.TerminalWindowName(path))
	if !hasAI && !hasTerm {
		return 0, false
	}

	m.cleanupSessionState(path)
	wasFronted := false
	if m.workingPath == path {
		m.swapCurrentAIOut()
		wasFronted = true
	}
	if m.termPath == path {
		m.swapCurrentTermOut()
		wasFronted = true
	}

	killed := 0
	if hasAI {
		asmtmux.KillDirectoryWindow(path)
		killed++
	}
	if hasTerm {
		asmtmux.KillTerminalWindow(path)
		killed++
	}
	delete(m.terminalStartTimes, path)
	return killed, wasFronted
}

func (m *PickerModel) swapOutWorkingPanel() {
	m.swapCurrentAIOut()
	m.swapCurrentTermOut()
}

func (m *PickerModel) refreshDirectoriesCmd() tea.Cmd {
	return m.scanDirectories()
}

func (m *PickerModel) refreshLayoutAndDirectoriesCmd() tea.Cmd {
	return tea.Batch(m.requestTerminalLayout(), m.scanDirectories())
}

func (m *PickerModel) queueBranchFetchForPath(path string) tea.Cmd {
	m.enqueueBranchFetch(path)
	return m.startNextMetadataFetches()
}

func trackedWorktree(path string) worktree.Worktree {
	return worktree.Worktree{
		Name: filepath.Base(path),
		Path: path,
	}
}
