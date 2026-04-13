package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// IDESelectModel is a standalone tea.Model for the working panel, used
// when the user asks to open the cursor worktree in an IDE.
type IDESelectModel struct {
	ides     []string
	cursor   int
	Selected string // set on selection, read after Run()
	width    int
	height   int
}

type ideSelectDoneMsg struct {
	IDEName string
	// Path is the worktree path the user wants to open. Carried through
	// because the picker's openIDESelect captures it before the dialog
	// runs; the dialog itself doesn't know about it.
	Path string
}

func NewIDESelectModel(ideNames []string) IDESelectModel {
	return IDESelectModel{ides: ideNames}
}

func (m IDESelectModel) Init() tea.Cmd { return nil }

func (m IDESelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.ides)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.ides) > 0 {
				m.Selected = m.ides[m.cursor]
			}
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m IDESelectModel) View() string {
	title := renderDialogTitle("Open in IDE", primaryColor)

	var rows []string
	for i, name := range m.ides {
		cursor := "  "
		style := normalItemStyle
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
			style = selectedItemStyle
		}
		rows = append(rows, "  "+cursor+style.Render(name))
	}

	content := padToHeight(title+"\n\n"+strings.Join(rows, "\n"), m.height-3)
	hint := renderDialogHintBar(m.width, " ↑↓: navigate  Enter: select  Esc: cancel")
	return content + "\n" + hint
}
