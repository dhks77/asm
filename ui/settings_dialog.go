package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/config"
)

// SettingsSavedMsg is sent to the picker when settings are saved via working panel.
type SettingsSavedMsg struct{}
type SettingsCancelledMsg struct{}

const (
	fieldToken = iota
	fieldProjectID
	fieldAPIBaseURL
	fieldPattern
	fieldCount
)

var fieldLabels = [fieldCount]string{
	"Token",
	"Project ID",
	"API Base URL",
	"Task Number Pattern",
}

// SettingsModel is a standalone tea.Model for the working panel.
type SettingsModel struct {
	cursor   int
	fields   [fieldCount]string
	rootPath string
	width    int
	height   int
	err      string
}

func NewSettingsModel(rootPath string) SettingsModel {
	m := SettingsModel{rootPath: rootPath}

	settings, err := config.LoadProjectSettings(rootPath)
	if err != nil {
		m.err = err.Error()
		return m
	}

	m.fields[fieldToken] = settings.Dooray.Token
	m.fields[fieldProjectID] = settings.Dooray.ProjectID
	m.fields[fieldAPIBaseURL] = settings.Dooray.APIBaseURL
	m.fields[fieldPattern] = settings.Dooray.TaskNumberPattern

	return m
}

func (m SettingsModel) Init() tea.Cmd {
	return nil
}

func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m SettingsModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle paste (Cmd+V)
	if msg.Paste {
		m.fields[m.cursor] += string(msg.Runes)
		return m, nil
	}

	key := msg.String()

	switch key {
	case "esc", "ctrl+c":
		return m, tea.Quit

	case "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down":
		if m.cursor < fieldCount-1 {
			m.cursor++
		}

	case "tab":
		m.cursor = (m.cursor + 1) % fieldCount

	case "shift+tab":
		m.cursor = (m.cursor - 1 + fieldCount) % fieldCount

	case "ctrl+s":
		return m.save()

	case "backspace":
		f := m.fields[m.cursor]
		if len(f) > 0 {
			m.fields[m.cursor] = f[:len(f)-1]
		}

	case "ctrl+u":
		m.fields[m.cursor] = ""

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.fields[m.cursor] += key
		}
	}

	return m, nil
}

func (m SettingsModel) save() (tea.Model, tea.Cmd) {
	dooray := config.DooraySettings{
		Token:             strings.TrimSpace(m.fields[fieldToken]),
		ProjectID:         strings.TrimSpace(m.fields[fieldProjectID]),
		APIBaseURL:        strings.TrimSpace(m.fields[fieldAPIBaseURL]),
		TaskNumberPattern: strings.TrimSpace(m.fields[fieldPattern]),
	}

	settings := &config.ProjectSettings{Dooray: dooray}
	if err := config.SaveProjectSettings(m.rootPath, settings); err != nil {
		m.err = err.Error()
		return m, nil
	}

	return m, tea.Quit
}

func (m SettingsModel) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(1, 2).
		Render("Settings")

	subtitle := lipgloss.NewStyle().
		Foreground(dimColor).
		Padding(0, 2).
		Render("Dooray Integration")

	var rows []string
	if m.err != "" {
		rows = append(rows, lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("196")).Render(m.err))
		rows = append(rows, "")
	}

	labelWidth := 0
	for _, l := range fieldLabels {
		if len(l) > labelWidth {
			labelWidth = len(l)
		}
	}

	for i := 0; i < fieldCount; i++ {
		label := fieldLabels[i]
		padding := strings.Repeat(" ", labelWidth-len(label))

		labelStyle := lipgloss.NewStyle().Foreground(dimColor)
		indicator := "  "
		if i == m.cursor {
			labelStyle = lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
			indicator = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
		}

		value := m.fields[i]
		if i == fieldToken && len(value) > 0 && i != m.cursor {
			value = strings.Repeat("*", min(len(value), 8)) + value[max(0, len(value)-4):]
		}

		valueStr := value
		if i == m.cursor {
			valueStr = value + lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
		}
		if value == "" && i != m.cursor {
			if i == fieldPattern {
				valueStr = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(default: last number)")
			} else {
				valueStr = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("-")
			}
		}

		row := "  " + indicator + labelStyle.Render(padding+label+": ") + valueStr
		rows = append(rows, row)
	}

	content := title + "\n" + subtitle + "\n\n" + strings.Join(rows, "\n")

	// Fill remaining height
	lines := strings.Count(content, "\n") + 1
	contentHeight := m.height - 3
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	hint := " ↑↓/Tab: field  Ctrl+S: save  Esc: cancel"
	statusBar := statusBarStyle.
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(hint)

	return content + "\n" + statusBar
}
