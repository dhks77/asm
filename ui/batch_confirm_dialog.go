package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type BatchAction int

const (
	BatchKillSessions BatchAction = iota
	BatchDeleteWorktrees
	// BatchNavigateRestart warns the user before the picker re-execs asm in
	// a different --path. Every active AI session in the current tmux
	// session will be killed as a side effect — they're listed so the user
	// sees what they'd lose by confirming.
	BatchNavigateRestart
)

type BatchConfirmModel struct {
	visible bool
	action  BatchAction
	items   []string
	// taskNames is a parallel slice to items (same length, empty string
	// when a directory has no resolved task). Displayed beside the
	// folder name so users can confirm by task identity, not just path.
	taskNames []string
	dirty     int // number of items with uncommitted changes
	// navigatePath carries the target --path for BatchNavigateRestart so
	// the dialog can show the user where they're about to land.
	navigatePath string
	// targetSession is the tmux session name that already exists at
	// navigatePath, if any. Empty string means the target is unoccupied.
	// Displayed as a warning because confirming will kill that session
	// too (via the post-exec orchestrator path).
	targetSession string
	cursor        int // 0=confirm, 1=cancel
	width        int
	height       int
}

type BatchConfirmedMsg struct {
	Action BatchAction
	Items  []string
}

type BatchCancelledMsg struct{}

func NewBatchConfirmModel() BatchConfirmModel {
	return BatchConfirmModel{}
}

func (m *BatchConfirmModel) Show(action BatchAction, items, taskNames []string, dirtyCount int) {
	m.visible = true
	m.action = action
	m.items = items
	m.taskNames = taskNames
	m.dirty = dirtyCount
	m.navigatePath = ""
	m.cursor = 1 // default to Cancel for safety
}

// ShowNavigate opens the dialog for a ←/→ navigation where either active
// AI sessions would be killed, or the target --path already has its own
// asm tmux session. items are the current dir's AI session names (may be
// empty if only the target-conflict case applies). targetPath is where
// the restart will land. targetSession is the existing tmux session name
// at the target, or "" when the target is free.
func (m *BatchConfirmModel) ShowNavigate(items, taskNames []string, targetPath, targetSession string) {
	m.visible = true
	m.action = BatchNavigateRestart
	m.items = items
	m.taskNames = taskNames
	m.dirty = 0
	m.navigatePath = targetPath
	m.targetSession = targetSession
	m.cursor = 1 // default to Cancel for safety
}

func (m *BatchConfirmModel) Hide() {
	m.visible = false
	m.items = nil
	m.navigatePath = ""
	m.targetSession = ""
}

func (m BatchConfirmModel) Update(msg tea.Msg) (BatchConfirmModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right", "l":
			if m.cursor < 1 {
				m.cursor++
			}
		case "enter":
			if m.cursor == 0 {
				items := m.items
				action := m.action
				m.Hide()
				return m, func() tea.Msg {
					return BatchConfirmedMsg{Action: action, Items: items}
				}
			}
			m.Hide()
			return m, func() tea.Msg {
				return BatchCancelledMsg{}
			}
		case "esc", "q":
			m.Hide()
			return m, func() tea.Msg {
				return BatchCancelledMsg{}
			}
		}
	}

	return m, nil
}

func (m BatchConfirmModel) View() string {
	// Navigate can fire with zero items (only a target-session conflict) —
	// the empty-items short-circuit still guards kill/delete actions since
	// those are meaningless without a target list.
	if !m.visible {
		return ""
	}
	if m.action != BatchNavigateRestart && len(m.items) == 0 {
		return ""
	}

	var titleText string
	switch m.action {
	case BatchKillSessions:
		titleText = fmt.Sprintf("Kill %d session(s)?", len(m.items))
	case BatchDeleteWorktrees:
		titleText = fmt.Sprintf("Delete %d worktree(s)?", len(m.items))
	case BatchNavigateRestart:
		titleText = "Restart asm at new location?"
	}

	title := renderDialogTitle(titleText, dangerColor)

	// Show all selected items — no truncation. The picker's selection cap
	// keeps this bounded in practice, and the fullscreen layout has the
	// room.
	var body strings.Builder
	body.WriteString("\n")
	if m.action == BatchNavigateRestart && m.navigatePath != "" {
		body.WriteString(lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).
			Render("→ "+m.navigatePath) + "\n")
		if m.targetSession != "" {
			body.WriteString(lipgloss.NewStyle().Padding(0, 2).Foreground(warnColor).Bold(true).
				Render(fmt.Sprintf("⚠ Target already runs an asm session (%s) — it will be killed", m.targetSession)) + "\n")
		}
		if len(m.items) > 0 {
			body.WriteString("\n")
			body.WriteString(lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).
				Render(fmt.Sprintf("These %d AI session(s) will close:", len(m.items))) + "\n")
		} else {
			body.WriteString("\n")
		}
	}
	nameStyle := lipgloss.NewStyle().Foreground(dimColor)
	taskStyle := lipgloss.NewStyle().Foreground(primaryColor)
	for i, name := range m.items {
		task := ""
		if i < len(m.taskNames) {
			task = m.taskNames[i]
		}
		row := "  "
		if task != "" {
			row += taskStyle.Render(task) + "  " + nameStyle.Render("("+name+")")
		} else {
			row += name
		}
		body.WriteString(row + "\n")
	}

	if m.dirty > 0 && m.action == BatchDeleteWorktrees {
		body.WriteString("\n")
		body.WriteString(lipgloss.NewStyle().Padding(0, 2).Foreground(warnColor).Bold(true).
			Render(fmt.Sprintf("⚠ %d item(s) have uncommitted changes", m.dirty)))
	}

	options := []string{"Confirm", "Cancel"}
	var buttons []string
	for i, opt := range options {
		style := lipgloss.NewStyle().Padding(0, 3)
		if i == m.cursor {
			style = style.
				Background(dangerColor).
				Foreground(lipgloss.Color("0")).
				Bold(true)
		} else {
			style = style.Foreground(dimColor)
		}
		buttons = append(buttons, style.Render(opt))
	}
	buttonRow := lipgloss.NewStyle().Padding(1, 2).Render(
		lipgloss.JoinHorizontal(lipgloss.Center, buttons...),
	)

	content := padToHeight(
		title+"\n"+body.String()+"\n"+buttonRow,
		m.height-3,
	)
	hint := renderDialogHintBar(m.width, " ←→: select  Enter: confirm  Esc: cancel")
	return content + "\n" + hint
}

func (m *BatchConfirmModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m BatchConfirmModel) IsVisible() bool {
	return m.visible
}
