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
	cursor    int // 0=confirm, 1=cancel
	width     int
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
	m.cursor = 1 // default to Cancel for safety
}

func (m *BatchConfirmModel) Hide() {
	m.visible = false
	m.items = nil
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
	if !m.visible || len(m.items) == 0 {
		return ""
	}

	var titleText string
	switch m.action {
	case BatchKillSessions:
		titleText = fmt.Sprintf("Kill %d session(s)?", len(m.items))
	case BatchDeleteWorktrees:
		titleText = fmt.Sprintf("Delete %d worktree(s)?", len(m.items))
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(dangerColor).
		Render(titleText)

	var body strings.Builder
	body.WriteString("\n")

	maxShow := 5
	shown := len(m.items)
	if shown > maxShow {
		shown = 3
	}
	nameStyle := lipgloss.NewStyle().Foreground(dimColor)
	for i := 0; i < shown; i++ {
		name := m.items[i]
		task := ""
		if i < len(m.taskNames) {
			task = m.taskNames[i]
		}
		if task != "" {
			body.WriteString(fmt.Sprintf("  %s  %s\n", task, nameStyle.Render("("+name+")")))
		} else {
			body.WriteString(fmt.Sprintf("  %s\n", name))
		}
	}
	if len(m.items) > maxShow {
		body.WriteString(fmt.Sprintf("  … and %d more\n", len(m.items)-shown))
	}

	if m.dirty > 0 && m.action == BatchDeleteWorktrees {
		body.WriteString(fmt.Sprintf("\n⚠ %d item(s) have uncommitted changes", m.dirty))
	}

	options := []string{"Confirm", "Cancel"}
	var buttons []string
	for i, opt := range options {
		style := lipgloss.NewStyle().Padding(0, 2)
		if i == m.cursor {
			style = style.
				Background(dangerColor).
				Foreground(lipgloss.Color("0")).
				Bold(true)
		} else {
			style = style.
				Foreground(dimColor)
		}
		buttons = append(buttons, style.Render(opt))
	}

	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, buttons...)

	content := title + body.String() + "\n" + buttonRow

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dangerColor).
		Padding(1, 2).
		Width(min(50, m.width-4))

	return dialogStyle.Render(content)
}

func (m *BatchConfirmModel) SetSize(w int) {
	m.width = w
}

func (m BatchConfirmModel) IsVisible() bool {
	return m.visible
}
