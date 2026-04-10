package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DeleteAction int

const (
	DeleteConfirm DeleteAction = iota
	DeleteCancel
)

type DeleteConfirmedMsg struct {
	Action DeleteAction
}

type DeleteDialogModel struct {
	visible      bool
	cursor       int
	worktreeName string
	width        int
}

func NewDeleteDialogModel() DeleteDialogModel {
	return DeleteDialogModel{}
}

func (m *DeleteDialogModel) Show(worktreeName string) {
	m.visible = true
	m.cursor = 1 // default to Cancel
	m.worktreeName = worktreeName
}

func (m *DeleteDialogModel) Hide() {
	m.visible = false
}

func (m *DeleteDialogModel) SetSize(w int) {
	m.width = w
}

func (m DeleteDialogModel) IsVisible() bool {
	return m.visible
}

func (m DeleteDialogModel) Update(msg tea.Msg) (DeleteDialogModel, tea.Cmd) {
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
			action := DeleteCancel
			if m.cursor == 0 {
				action = DeleteConfirm
			}
			m.Hide()
			return m, func() tea.Msg {
				return DeleteConfirmedMsg{Action: action}
			}
		case "esc", "q", "n":
			m.Hide()
			return m, func() tea.Msg {
				return DeleteConfirmedMsg{Action: DeleteCancel}
			}
		case "y":
			m.Hide()
			return m, func() tea.Msg {
				return DeleteConfirmedMsg{Action: DeleteConfirm}
			}
		}
	}

	return m, nil
}

func (m DeleteDialogModel) View() string {
	if !m.visible {
		return ""
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("196")).
		Render("Remove Worktree")

	question := fmt.Sprintf("\nRemove '%s'?\n", m.worktreeName)

	options := []string{"Remove", "Cancel"}
	var buttons []string
	for i, opt := range options {
		style := lipgloss.NewStyle().Padding(0, 2)
		if i == m.cursor {
			if i == 0 {
				style = style.
					Background(lipgloss.Color("196")).
					Foreground(lipgloss.Color("0")).
					Bold(true)
			} else {
				style = style.
					Background(primaryColor).
					Foreground(lipgloss.Color("0")).
					Bold(true)
			}
		} else {
			style = style.Foreground(dimColor)
		}
		buttons = append(buttons, style.Render(opt))
	}

	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, buttons...)

	content := title + question + "\n" + buttonRow

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 2).
		Width(min(45, m.width-4))

	return dialogStyle.Render(content)
}
