package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProviderSelectModel is a standalone tea.Model for the working panel.
type ProviderSelectModel struct {
	providers []string
	cursor    int
	Selected  string // set on selection, read after Run()
	width     int
	height    int
}

// providerSelectDoneMsg is the result of provider selection in the working panel.
type providerSelectDoneMsg struct {
	ProviderName string
}

func NewProviderSelectModel(providerNames []string) ProviderSelectModel {
	return ProviderSelectModel{providers: providerNames}
}

func (m ProviderSelectModel) Init() tea.Cmd { return nil }

func (m ProviderSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.providers)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.providers) > 0 {
				m.Selected = m.providers[m.cursor]
			}
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m ProviderSelectModel) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(1, 2).
		Render("Select AI Provider")

	var rows []string
	for i, name := range m.providers {
		cursor := "  "
		style := normalItemStyle
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
			style = selectedItemStyle
		}
		rows = append(rows, "  "+cursor+style.Render(name))
	}

	content := title + "\n\n" + strings.Join(rows, "\n")

	lines := lipgloss.Height(content)
	contentHeight := m.height - 3
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	statusBar := statusBarStyle.
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(" ↑↓: navigate  Enter: select  Esc: cancel")

	return content + "\n" + statusBar
}
