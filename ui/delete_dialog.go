package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DeleteModel is a standalone tea.Model for the working panel.
type DeleteModel struct {
	dirName    string
	taskName   string
	dirty      bool
	isWorktree bool
	usesTrash  bool
	cursor     int
	Confirmed  bool
	width      int
	height     int
}

func NewDeleteModel(dirName, taskName string, dirty, isWorktree bool) DeleteModel {
	return DeleteModel{
		dirName:    dirName,
		taskName:   taskName,
		dirty:      dirty,
		isWorktree: isWorktree,
		usesTrash:  !isWorktree,
		cursor:     1, // default to Cancel
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
		case "left":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right":
			if m.cursor < 1 {
				m.cursor++
			}
		case "enter":
			m.Confirmed = m.cursor == 0
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.Confirmed = false
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m DeleteModel) View() string {
	titleText := "Move Directory to Trash"
	if m.isWorktree {
		titleText = "Remove Git Worktree"
	}
	title := renderDialogTitle(titleText, dangerColor)

	var info string
	if m.taskName != "" {
		info = lipgloss.NewStyle().Padding(0, 2).Foreground(primaryColor).Render(m.taskName) + "\n"
	}
	info += lipgloss.NewStyle().Padding(0, 2).Foreground(whiteColor).Bold(true).Render(m.dirName)

	var warning string
	if m.dirty {
		warning += "\n\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(warnColor).Bold(true).
			Render("⚠ Modified or untracked files exist")
		if m.usesTrash {
			warning += "\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).
				Render("  Uncommitted changes will move to Trash with the directory")
		} else {
			warning += "\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(warnColor).
				Render("  Uncommitted changes will be lost")
		}
	}
	if m.isWorktree {
		warning += "\n\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).
			Render("Git worktree will also be removed")
	} else {
		warning += "\n\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).
			Render("You can restore it later from Trash")
	}

	questionText := fmt.Sprintf("Move directory '%s' to Trash?", m.dirName)
	if m.isWorktree {
		questionText = fmt.Sprintf("Remove git worktree '%s'?", m.dirName)
	}
	question := lipgloss.NewStyle().Padding(1, 2).Foreground(dimColor).Render(questionText)

	options := []string{"Move to Trash", "Cancel"}
	if m.isWorktree {
		options = []string{"Remove", "Cancel"}
	}
	var buttons []string
	for i, opt := range options {
		style := lipgloss.NewStyle().Padding(0, 3)
		if i == m.cursor {
			if i == 0 {
				style = style.
					Background(dangerColor).
					Foreground(surfaceTextColor).
					Bold(true)
			} else {
				style = style.
					Background(primaryColor).
					Foreground(surfaceTextColor).
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

	content := padToHeight(
		title+"\n"+info+warning+"\n"+question+"\n\n"+buttonRow,
		m.height-3,
	)
	hint := renderDialogHintBar(m.width, " ←→: select  Enter: confirm  y/n  Esc: cancel")
	return content + "\n" + hint
}
