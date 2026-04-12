package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProviderDialogModel presents a list of AI providers for selection.
type ProviderDialogModel struct {
	providers []string
	cursor    int
	visible   bool
	width     int
}

// ProviderSelectedMsg is sent when a provider is selected.
type ProviderSelectedMsg struct {
	ProviderName string
}

// ProviderCancelledMsg is sent when the dialog is cancelled.
type ProviderCancelledMsg struct{}

func NewProviderDialogModel() ProviderDialogModel {
	return ProviderDialogModel{}
}

func (m *ProviderDialogModel) Show(providerNames []string) {
	m.providers = providerNames
	m.cursor = 0
	m.visible = true
}

func (m *ProviderDialogModel) Hide() {
	m.visible = false
	m.providers = nil
}

func (m ProviderDialogModel) Update(msg tea.Msg) (ProviderDialogModel, tea.Cmd) {
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
			if m.cursor < len(m.providers)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.providers) > 0 {
				selected := m.providers[m.cursor]
				m.Hide()
				return m, func() tea.Msg {
					return ProviderSelectedMsg{ProviderName: selected}
				}
			}
		case "esc", "q":
			m.Hide()
			return m, func() tea.Msg {
				return ProviderCancelledMsg{}
			}
		}
	}

	return m, nil
}

func (m ProviderDialogModel) View() string {
	if !m.visible || len(m.providers) == 0 {
		return ""
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("Select AI Provider")

	var rows []string
	for i, name := range m.providers {
		cursor := "  "
		style := normalItemStyle
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
			style = selectedItemStyle
		}
		rows = append(rows, fmt.Sprintf("%s%s", cursor, style.Render(name)))
	}

	content := title + "\n\n" + strings.Join(rows, "\n") + "\n\n" +
		statusBarStyle.Render("Enter: select  Esc: cancel")

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(min(40, m.width-4))

	return dialogStyle.Render(content)
}

func (m *ProviderDialogModel) SetSize(w int) {
	m.width = w
}

func (m ProviderDialogModel) IsVisible() bool {
	return m.visible
}
