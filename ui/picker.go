package ui

import (
	"fmt"
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

type sessionExitedMsg struct {
	WorktreeName string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type PickerModel struct {
	cfg           *config.Config
	rootPath      string
	worktrees     []worktree.Worktree
	gitStatus     map[string]worktree.GitStatus
	taskNames     map[string]string
	claudeStates  map[string]claude.State
	spinnerFrame  int
	cursor        int
	currentWT     string // worktree name currently shown in right pane
	dooray        *integration.DoorayClient
	resumeDialog  ResumeDialogModel
	confirmDialog ConfirmDialogModel
	width         int
	height        int
	ready         bool
	err           string
}

func NewPickerModel(cfg *config.Config, rootPath string) PickerModel {
	dooray := integration.NewDoorayClient(cfg.Dooray, cfg.TaskIDPattern)
	return PickerModel{
		cfg:           cfg,
		rootPath:      rootPath,
		gitStatus:     make(map[string]worktree.GitStatus),
		taskNames:     make(map[string]string),
		claudeStates:  make(map[string]claude.State),
		dooray:        dooray,
		resumeDialog:  NewResumeDialogModel(),
		confirmDialog: NewConfirmDialogModel(),
	}
}

func (m PickerModel) Init() tea.Cmd {
	return tea.Batch(m.scanWorktrees(), tickCmd(), claudeStateTickCmd(), spinnerTickCmd())
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
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

	case sessionExitedMsg:
		delete(m.claudeStates, msg.WorktreeName)
		isDisplayed := m.currentWT == msg.WorktreeName
		csmtmux.CleanupExitedWindow(msg.WorktreeName, isDisplayed)
		if isDisplayed {
			m.currentWT = ""
			csmtmux.FocusLeft()
		}
		return m, nil

	case tea.KeyMsg:
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
			if m.dooray != nil {
				cmds = append(cmds, m.fetchTaskName(wt))
			}
		}
		return m, tea.Batch(cmds...)

	case GitStatusUpdatedMsg:
		m.gitStatus[msg.Path] = msg.Status
		return m, nil

	case TaskNameResolvedMsg:
		if msg.Name != "" {
			m.taskNames[msg.Path] = msg.Name
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

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
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
		delete(m.claudeStates, wt.Name)
		if m.currentWT == wt.Name {
			csmtmux.SwapBackFromRight(wt.Name)
			m.currentWT = ""
		}
		csmtmux.KillWorktreeWindow(wt.Name)

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

// waitForExitCmd returns a tea.Cmd that blocks until claude exits in the worktree.
func waitForExitCmd(worktreeName string) tea.Cmd {
	return func() tea.Msg {
		csmtmux.WaitForExit(worktreeName)
		return sessionExitedMsg{WorktreeName: worktreeName}
	}
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

	title := headerStyle.Render("CSM")

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

	errMsg := ""
	if m.err != "" {
		errMsg = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.err)
		m.err = ""
	}

	statusBar := RenderStatusBar(m.width)

	contentHeight := m.height - 3
	content := title + "\n" + list + errMsg

	lines := strings.Count(content, "\n") + 1
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	view := content + "\n" + statusBar

	if m.confirmDialog.IsVisible() {
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
		indicator = lipgloss.NewStyle().Foreground(activeColor).Bold(true).Render("▶")
	}

	nameStyle := normalItemStyle
	if isSelected {
		nameStyle = selectedItemStyle
	}

	line1 := fmt.Sprintf(" %s %s", indicator, nameStyle.Render(wt.Name))

	if state, ok := m.claudeStates[wt.Name]; ok && hasSession {
		rendered := renderClaudeState(state, m.spinnerFrame)
		if rendered != "" {
			line1 += " " + rendered
		}
	}

	var line2 string
	if taskName, ok := m.taskNames[wt.Path]; ok && taskName != "" {
		line2 = fmt.Sprintf("     %s", taskNameStyle.Render(taskName))
	}

	var line3 string
	if gs, ok := m.gitStatus[wt.Path]; ok {
		line3 = fmt.Sprintf("     %s", gitStatusStyle.Render(gs.Summary()))
	}

	result := line1
	if line2 != "" {
		result += "\n" + line2
	}
	if line3 != "" {
		result += "\n" + line3
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

func (m PickerModel) fetchTaskName(wt worktree.Worktree) tea.Cmd {
	dooray := m.dooray
	return func() tea.Msg {
		name := dooray.ResolveTaskName(wt.Name)
		return TaskNameResolvedMsg{Path: wt.Path, Name: name}
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

