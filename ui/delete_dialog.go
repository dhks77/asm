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

// DeleteModel is a standalone tea.Model for the right pane.
type DeleteModel struct {
	worktreeName string
	taskName     string
	dirty        bool
	cursor       int
	Confirmed    bool
	width        int
	height       int
}

func NewDeleteModel(worktreeName, taskName string, dirty bool) DeleteModel {
	return DeleteModel{
		worktreeName: worktreeName,
		taskName:     taskName,
		dirty:        dirty,
		cursor:       1, // default to Cancel
	}
}

func (m DeleteModel) Init() tea.Cmd {
	return nil
}

func (m DeleteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

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
			m.Confirmed = m.cursor == 0
			return m, tea.Quit
		case "esc", "q", "n", "ctrl+c":
			m.Confirmed = false
			return m, tea.Quit
		case "y":
			m.Confirmed = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m DeleteModel) View() string {
	dangerColor := lipgloss.Color("196")
	warnColor := lipgloss.Color("220")

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(dangerColor).
		Padding(1, 2).
		Render("Remove Worktree")

	var info string
	if m.taskName != "" {
		info = lipgloss.NewStyle().Padding(0, 2).Foreground(primaryColor).Render(m.taskName) + "\n"
	}
	info += lipgloss.NewStyle().Padding(0, 2).Foreground(whiteColor).Bold(true).Render(m.worktreeName)

	var warning string
	if m.dirty {
		warning = "\n\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(warnColor).Bold(true).
			Render("⚠ Modified or untracked files exist") + "\n" +
			lipgloss.NewStyle().Padding(0, 2).Foreground(warnColor).
				Render("  Uncommitted changes will be lost")
	}

	question := lipgloss.NewStyle().Padding(1, 2).Foreground(dimColor).
		Render(fmt.Sprintf("Remove worktree '%s'?", m.worktreeName))

	options := []string{"Remove", "Cancel"}
	var buttons []string
	for i, opt := range options {
		style := lipgloss.NewStyle().Padding(0, 3)
		if i == m.cursor {
			if i == 0 {
				style = style.
					Background(dangerColor).
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

	buttonRow := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.JoinHorizontal(lipgloss.Center, buttons...),
	)

	content := title + "\n" + info + warning + "\n" + question + "\n\n" + buttonRow

	// Fill remaining height
	lines := lipgloss.Height(content)
	contentHeight := m.height - 3
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	hint := " ←→: select  Enter: confirm  y/n  Esc: cancel"
	statusBar := statusBarStyle.
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(hint)

	return content + "\n" + statusBar
}
