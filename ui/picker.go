package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/csm/claude"
	"github.com/nhn/csm/config"
	"github.com/nhn/csm/integration"
	"github.com/nhn/csm/session"
	csmtmux "github.com/nhn/csm/tmux"
	"github.com/nhn/csm/worktree"
)

// Messages
type WorktreesScannedMsg struct {
	Worktrees []worktree.Worktree
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
	WorktreeName string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type PickerModel struct {
	cfg            *config.Config
	rootPath       string
	worktrees      []worktree.Worktree
	gitStatus      map[string]worktree.GitStatus
	taskNames      map[string]string
	claudeStates   map[string]claude.State
	spinnerFrame   int
	scrollTick     int
	cursor         int
	currentWT      string // worktree name currently shown in right pane
	dooray         *integration.DoorayClient
	resumeDialog   ResumeDialogModel
	confirmDialog  ConfirmDialogModel
	worktreeDialog WorktreeDialogModel
	focused        bool
	width          int
	height         int
	ready          bool
	err            string
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
		claudeStates:   make(map[string]claude.State),
		dooray:         dooray,
		resumeDialog:   NewResumeDialogModel(),
		confirmDialog:  NewConfirmDialogModel(),
		worktreeDialog: NewWorktreeDialogModel(),
		focused:        true,
	}
}

func (m PickerModel) Init() tea.Cmd {
	return tea.Batch(m.scanWorktrees(), tickCmd(), claudeStateTickCmd(), spinnerTickCmd(), scrollTickCmd())
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
		for _, wt := range m.worktrees {
			cmds = append(cmds, m.fetchGitStatus(wt.Path))
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case claudeStateTickMsg:
		var cmds []tea.Cmd
		activeWindows := csmtmux.ListWorktreeWindows()
		activeSet := make(map[string]bool)
		for _, name := range activeWindows {
			activeSet[name] = true
		}
		for _, wt := range m.worktrees {
			if activeSet[wt.Name] {
				cmds = append(cmds, m.fetchClaudeState(wt.Name))
			}
		}
		cmds = append(cmds, claudeStateTickCmd())
		return m, tea.Batch(cmds...)

	case ClaudeStateUpdatedMsg:
		if msg.State != claude.StateUnknown {
			m.claudeStates[msg.Name] = msg.State
		}
		return m, nil

	case spinnerTickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTickCmd()

	case scrollTickMsg:
		m.scrollTick++
		return m, scrollTickCmd()

	case sessionExitedMsg:
		delete(m.claudeStates, msg.WorktreeName)
		isDisplayed := m.currentWT == msg.WorktreeName
		csmtmux.CleanupExitedWindow(msg.WorktreeName, isDisplayed)
		if isDisplayed {
			m.currentWT = ""
			csmtmux.FocusLeft()
		}
		return m, nil

	case BranchesLoadedMsg:
		var cmd tea.Cmd
		m.worktreeDialog, cmd = m.worktreeDialog.Update(msg)
		return m, cmd

	case WorktreeCreatedMsg, WorktreeRemovedMsg:
		return m, m.scanWorktrees()

	case WorktreeCancelledMsg:
		return m, nil

	case WorktreeErrorMsg:
		m.err = msg.Err
		return m, nil

	case deleteExitedMsg:
		if !msg.confirmed {
			return m, nil
		}
		var wt *worktree.Worktree
		for i := range m.worktrees {
			if m.worktrees[i].Name == msg.worktreeName {
				wt = &m.worktrees[i]
				break
			}
		}
		if wt == nil {
			return m, nil
		}
		delete(m.claudeStates, wt.Name)
		winName := csmtmux.WindowName(wt.Name)
		if csmtmux.WindowExists(winName) {
			csmtmux.KillWorktreeWindow(wt.Name)
		}
		return m, m.removeWorktree(wt)

	case tea.KeyMsg:
		if m.err != "" {
			m.err = ""
			return m, nil
		}
		if m.worktreeDialog.IsVisible() {
			var cmd tea.Cmd
			m.worktreeDialog, cmd = m.worktreeDialog.Update(msg)
			return m, cmd
		}
		if m.confirmDialog.IsVisible() {
			var cmd tea.Cmd
			m.confirmDialog, cmd = m.confirmDialog.Update(msg)
			return m, cmd
		}
		if m.resumeDialog.IsVisible() {
			var cmd tea.Cmd
			m.resumeDialog, cmd = m.resumeDialog.Update(msg)
			return m, cmd
		}
		return m.handleKey(msg)

	case ResumeSelectedMsg:
		wt := m.selectedWorktree()
		if wt != nil {
			return m, m.resumeSession(wt, msg.SessionID)
		}
		return m, nil

	case ResumeCancelledMsg:
		return m, nil

	case QuitConfirmedMsg:
		switch msg.Action {
		case QuitKeep:
			csmtmux.KillSession()
			return m, tea.Quit
		case QuitTerminate:
			csmtmux.KillSession()
			return m, tea.Quit
		case QuitCancel:
			return m, nil
		}

	case WorktreesScannedMsg:
		m.worktrees = msg.Worktrees
		var cmds []tea.Cmd
		for _, wt := range msg.Worktrees {
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

	case settingsExitedMsg:
		m.reloadDoorayClient()
		if m.dooray != nil {
			var cmds []tea.Cmd
			for _, wt := range m.worktrees {
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

	switch key {
	case "ctrl+c":
		csmtmux.KillSession()
		return m, tea.Quit
	case "q":
		activeWindows := csmtmux.ListWorktreeWindows()
		if len(activeWindows) > 0 {
			m.confirmDialog.SetSize(m.width)
			m.confirmDialog.Show(len(activeWindows))
			return m, nil
		}
		csmtmux.KillSession()
		return m, tea.Quit

	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
		if m.cursor < len(m.worktrees)-1 {
			m.cursor++
		}

	case "enter":
		wt := m.selectedWorktree()
		if wt == nil {
			return m, nil
		}
		winName := csmtmux.WindowName(wt.Name)
		if csmtmux.WindowExists(winName) {
			m.switchToWorktree(wt)
		} else {
			return m, m.startSession(wt)
		}

	case "n":
		wt := m.selectedWorktree()
		if wt == nil {
			return m, nil
		}
		delete(m.claudeStates, wt.Name)
		winName := csmtmux.WindowName(wt.Name)
		if csmtmux.WindowExists(winName) {
			if m.currentWT == wt.Name {
				csmtmux.SwapBackFromRight(wt.Name)
				m.currentWT = ""
			}
			csmtmux.KillWorktreeWindow(wt.Name)
		}
		return m, m.startSession(wt)

	case "r":
		wt := m.selectedWorktree()
		if wt == nil {
			return m, nil
		}
		sessions, err := session.FindSessions(wt.Path)
		if err != nil || len(sessions) == 0 {
			m.err = "No previous sessions found"
			return m, nil
		}
		m.resumeDialog.SetSize(m.width, m.height)
		m.resumeDialog.Show(sessions)

	case "d":
		wt := m.selectedWorktree()
		if wt == nil {
			return m, nil
		}
		return m, m.openDelete(wt)

	case "w":
		m.worktreeDialog.SetSize(m.width, m.height)
		return m, m.worktreeDialog.Show(m.rootPath)

	case "s":
		return m, m.openSettings()

	case "tab":
		// Focus the right tmux pane
		if m.currentWT != "" {
			csmtmux.FocusRight()
		}
	}

	return m, nil
}

func (m *PickerModel) startSession(wt *worktree.Worktree) tea.Cmd {
	claudePath := m.cfg.ResolveClaudePath()
	csmtmux.CreateWorktreeWindow(wt.Name, wt.Path, claudePath, m.cfg.ClaudeArgs)
	m.switchToWorktree(wt)
	return waitForExitCmd(wt.Name)
}

func (m *PickerModel) resumeSession(wt *worktree.Worktree, sessionID string) tea.Cmd {
	claudePath := m.cfg.ResolveClaudePath()

	winName := csmtmux.WindowName(wt.Name)
	if csmtmux.WindowExists(winName) {
		if m.currentWT == wt.Name {
			csmtmux.SwapBackFromRight(wt.Name)
			m.currentWT = ""
		}
		csmtmux.KillWorktreeWindow(wt.Name)
	}

	csmtmux.ResumeInWindow(wt.Name, wt.Path, claudePath, sessionID, m.cfg.ClaudeArgs)
	m.switchToWorktree(wt)
	return waitForExitCmd(wt.Name)
}

type WorktreeRemovedMsg struct{}

func (m *PickerModel) removeWorktree(wt *worktree.Worktree) tea.Cmd {
	rootPath := m.rootPath
	wtPath := wt.Path
	return func() tea.Msg {
		repoDir, err := worktree.FindGitRepo(rootPath)
		if err != nil {
			return WorktreeErrorMsg{Err: err.Error()}
		}
		// Try normal remove first, then force if it fails
		if err := worktree.RemoveWorktree(repoDir, wtPath, false); err != nil {
			if err2 := worktree.RemoveWorktree(repoDir, wtPath, true); err2 != nil {
				return WorktreeErrorMsg{Err: fmt.Sprintf("worktree remove failed: %v", err2)}
			}
		}
		return WorktreeRemovedMsg{}
	}
}

// waitForExitCmd returns a tea.Cmd that blocks until claude exits in the worktree.
func waitForExitCmd(worktreeName string) tea.Cmd {
	return func() tea.Msg {
		csmtmux.WaitForExit(worktreeName)
		return sessionExitedMsg{WorktreeName: worktreeName}
	}
}

type settingsExitedMsg struct{}
type deleteExitedMsg struct {
	worktreeName string
	confirmed    bool
}

func (m *PickerModel) swapOutCurrentWT() {
	if m.currentWT != "" {
		csmtmux.SwapBackFromRight(m.currentWT)
		m.currentWT = ""
	}
}

func (m *PickerModel) openSettings() tea.Cmd {
	m.swapOutCurrentWT()

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	cmd := fmt.Sprintf("%s --settings --path %s", exe, m.rootPath)
	csmtmux.RunInRightPane("csm-settings", cmd)
	csmtmux.FocusRight()

	return func() tea.Msg {
		csmtmux.WaitAndCleanupRightPane("csm-settings")
		return settingsExitedMsg{}
	}
}

func (m *PickerModel) openDelete(wt *worktree.Worktree) tea.Cmd {
	m.swapOutCurrentWT()

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	taskName := ""
	if tn, ok := m.taskNames[wt.Path]; ok {
		taskName = tn
	}

	dirty := worktree.HasChanges(wt.Path)

	cmd := fmt.Sprintf("%s --delete %s", exe, wt.Name)
	if taskName != "" {
		cmd += fmt.Sprintf(" --delete-task '%s'", taskName)
	}
	if dirty {
		cmd += " --delete-dirty"
	}
	csmtmux.RunInRightPane("csm-delete", cmd)
	csmtmux.FocusRight()

	wtName := wt.Name
	return func() tea.Msg {
		exitCode := csmtmux.WaitAndCleanupRightPane("csm-delete")
		return deleteExitedMsg{worktreeName: wtName, confirmed: exitCode == 0}
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

func (m *PickerModel) switchToWorktree(wt *worktree.Worktree) {
	if m.currentWT != "" && m.currentWT != wt.Name {
		csmtmux.SwapBackFromRight(m.currentWT)
	}
	csmtmux.SwapToRight(wt.Name)
	m.currentWT = wt.Name
	csmtmux.FocusRight()
}

func (m *PickerModel) selectedWorktree() *worktree.Worktree {
	if len(m.worktrees) == 0 || m.cursor >= len(m.worktrees) {
		return nil
	}
	return &m.worktrees[m.cursor]
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

	var rows []string
	activeWindows := csmtmux.ListWorktreeWindows()
	activeSet := make(map[string]bool)
	for _, name := range activeWindows {
		activeSet[name] = true
	}

	for i, wt := range m.worktrees {
		row := m.renderItem(i, wt, activeSet[wt.Name])
		rows = append(rows, row)
	}

	list := strings.Join(rows, "\n")

	statusBar := RenderStatusBar(m.width, m.focused)

	contentHeight := m.height - 3
	content := title + "\n" + list

	lines := strings.Count(content, "\n") + 1
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	view := content + "\n" + statusBar

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
	} else if m.worktreeDialog.IsVisible() {
		view = m.overlayCenter(view, m.worktreeDialog.View())
	} else if m.confirmDialog.IsVisible() {
		view = m.overlayCenter(view, m.confirmDialog.View())
	} else if m.resumeDialog.IsVisible() {
		view = m.overlayCenter(view, m.resumeDialog.View())
	}

	return view
}

func (m PickerModel) renderItem(index int, wt worktree.Worktree, hasSession bool) string {
	isSelected := index == m.cursor

	indicator := inactiveSessionStyle.String()
	if hasSession {
		indicator = activeSessionStyle.String()
	}
	if m.currentWT == wt.Name {
		indicator = lipgloss.NewStyle().Foreground(activeColor).Bold(true).Render("●")
	}
	if isSelected {
		indicator = lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("●")
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

	// Calculate available width for primary name
	prefixWidth := 2 // indicator(1) + space(1)
	stateStr := ""
	if state, ok := m.claudeStates[wt.Name]; ok && hasSession {
		stateStr = renderClaudeState(state, m.spinnerFrame)
	}
	stateWidth := 0
	if stateStr != "" {
		stateWidth = lipgloss.Width(stateStr) + 1
	}
	maxNameWidth := m.width - prefixWidth - stateWidth

	var rawName string
	if hasTask {
		rawName = taskName
		if hasBranch {
			subLines = append(subLines, gitStatusStyle.Render(gs.Summary()))
		}
		subLines = append(subLines, normalItemStyle.Render(wt.Name))
	} else if hasBranch {
		rawName = gs.Summary()
		subLines = append(subLines, normalItemStyle.Render(wt.Name))
	} else {
		rawName = wt.Name
	}

	displayName := scrollText(rawName, maxNameWidth, m.scrollTick)
	primaryName = primaryStyle.Render(displayName)

	line1 := fmt.Sprintf("%s %s", indicator, primaryName)

	if stateStr != "" {
		line1 += " " + stateStr
	}

	bar := "  "
	if isSelected {
		bar = lipgloss.NewStyle().Foreground(primaryColor).Render("▎") + " "
	}

	result := line1
	for _, sub := range subLines {
		result += "\n" + fmt.Sprintf("  %s%s", bar, sub)
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

func (m PickerModel) scanWorktrees() tea.Cmd {
	return func() tea.Msg {
		wts, _ := worktree.Scan(m.rootPath)
		return WorktreesScannedMsg{Worktrees: wts}
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

func (m PickerModel) fetchClaudeState(worktreeName string) tea.Cmd {
	currentWT := m.currentWT
	return func() tea.Msg {
		isDisplayed := currentWT == worktreeName
		title, err := csmtmux.GetPaneTitle(worktreeName, isDisplayed)
		if err != nil {
			return ClaudeStateUpdatedMsg{Name: worktreeName, State: claude.StateUnknown}
		}
		state := claude.DetectStateFromTitle(title)

		// If busy, refine with pane content for detail (thinking/tool/responding)
		if state == claude.StateBusy {
			content, err := csmtmux.CapturePaneContent(worktreeName, isDisplayed)
			if err == nil {
				detail := claude.DetectBusyDetail(content)
				if detail != claude.StateBusy {
					state = detail
				}
			}
		}

		return ClaudeStateUpdatedMsg{Name: worktreeName, State: state}
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

