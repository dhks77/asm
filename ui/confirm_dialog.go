package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type QuitAction int

const (
	QuitKeep QuitAction = iota
	QuitTerminate
	QuitCancel
)

type ConfirmDialogModel struct {
	visible      bool
	cursor       int
	activeCount  int
	width        int
}

type QuitConfirmedMsg struct {
	Action QuitAction
}

func NewConfirmDialogModel() ConfirmDialogModel {
	return ConfirmDialogModel{}
}

func (m *ConfirmDialogModel) Show(activeCount int) {
	m.visible = true
	m.cursor = 0
	m.activeCount = activeCount
}

func (m *ConfirmDialogModel) Hide() {
	m.visible = false
}

func (m ConfirmDialogModel) Update(msg tea.Msg) (ConfirmDialogModel, tea.Cmd) {
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
			if m.cursor < 2 {
				m.cursor++
			}
		case "enter":
			action := QuitAction(m.cursor)
			m.Hide()
			return m, func() tea.Msg {
				return QuitConfirmedMsg{Action: action}
			}
		case "esc", "q":
			m.Hide()
			return m, func() tea.Msg {
				return QuitConfirmedMsg{Action: QuitCancel}
			}
		}
	}

	return m, nil
}

func (m ConfirmDialogModel) View() string {
	if !m.visible {
		return ""
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render(fmt.Sprintf("%d active session(s)", m.activeCount))

	question := "\nWhat would you like to do?\n"

	options := []string{"Keep", "Terminate All", "Cancel"}
	var buttons []string
	for i, opt := range options {
		style := lipgloss.NewStyle().Padding(0, 2)
		if i == m.cursor {
			style = style.
				Background(primaryColor).
				Foreground(lipgloss.Color("0")).
				Bold(true)
		} else {
			style = style.
				Foreground(dimColor)
		}
		buttons = append(buttons, style.Render(opt))
	}

	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, buttons...)

	content := title + question + "\n" + buttonRow

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(min(50, m.width-4))

	return dialogStyle.Render(content)
}

func (m *ConfirmDialogModel) SetSize(w int) {
	m.width = w
}

func (m ConfirmDialogModel) IsVisible() bool {
	return m.visible
}
