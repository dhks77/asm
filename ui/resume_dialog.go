package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/csm/session"
)

type ResumeDialogModel struct {
	sessions []session.ClaudeSession
	cursor   int
	visible  bool
	width    int
	height   int
}

type ResumeSelectedMsg struct {
	SessionID string
}

type ResumeCancelledMsg struct{}

func NewResumeDialogModel() ResumeDialogModel {
	return ResumeDialogModel{}
}

func (m *ResumeDialogModel) Show(sessions []session.ClaudeSession) {
	m.sessions = sessions
	m.cursor = 0
	m.visible = true
}

func (m *ResumeDialogModel) Hide() {
	m.visible = false
	m.sessions = nil
}

func (m ResumeDialogModel) Update(msg tea.Msg) (ResumeDialogModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.sessions) > 0 {
				selected := m.sessions[m.cursor]
				m.Hide()
				return m, func() tea.Msg {
					return ResumeSelectedMsg{SessionID: selected.SessionID}
				}
			}
		case "esc", "q":
			m.Hide()
			return m, func() tea.Msg {
				return ResumeCancelledMsg{}
			}
		}
	}

	return m, nil
}

func (m ResumeDialogModel) View() string {
	if !m.visible || len(m.sessions) == 0 {
		return ""
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("Resume Session")

	var rows []string
	for i, sess := range m.sessions {
		cursor := "  "
		style := normalItemStyle
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
			style = selectedItemStyle
		}

		timeStr := sess.StartedAtTime().Format("2006-01-02 15:04")
		line := fmt.Sprintf("%s%s  %s",
			cursor,
			style.Render(sess.SessionID[:8]+"..."),
			gitStatusStyle.Render(timeStr),
		)
		rows = append(rows, line)
	}

	content := title + "\n\n" + strings.Join(rows, "\n") + "\n\n" +
		statusBarStyle.Render("Enter: select  Esc: cancel")

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(min(50, m.width-4))

	return dialogStyle.Render(content)
}

func (m *ResumeDialogModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m ResumeDialogModel) IsVisible() bool {
	return m.visible
}
