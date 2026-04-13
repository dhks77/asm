package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/plugincfg"
)

type SettingsSavedMsg struct{}

type pluginFieldsLoadedMsg struct {
	index  int
	fields []plugincfg.Field
	values map[string]string
}

type settingsEntry struct {
	entry  plugincfg.Entry
	fields []plugincfg.Field
	values map[string]string
}

// flatItem represents a navigable item in the settings UI.
type flatItem struct {
	kind     string // "select" or "text"
	section  int    // -1 for general, >=0 for plugin index
	fieldIdx int
}

type SettingsModel struct {
	cfg     *config.Config
	entries []settingsEntry

	// General settings
	providerNames  []string
	trackerNames   []string
	selectedProvider int // index into providerNames
	selectedTracker  int // index into trackerNames

	items  []flatItem // built after fields load
	cursor int
	width  int
	height int
	err    string
}

func NewSettingsModel(cfg *config.Config, providerNames []string, trackerNames []string, plugins []plugincfg.Entry) SettingsModel {
	// Find current selection indices
	selProvider := 0
	for i, n := range providerNames {
		if n == cfg.DefaultProvider {
			selProvider = i
			break
		}
	}

	selTracker := 0
	for i, n := range trackerNames {
		if n == cfg.DefaultTracker {
			selTracker = i
			break
		}
	}

	entries := make([]settingsEntry, len(plugins))
	for i, p := range plugins {
		entries[i] = settingsEntry{entry: p}
	}

	m := SettingsModel{
		cfg:              cfg,
		entries:          entries,
		providerNames:    providerNames,
		trackerNames:     trackerNames,
		selectedProvider: selProvider,
		selectedTracker:  selTracker,
	}
	m.rebuildItems()
	return m
}

func (m *SettingsModel) rebuildItems() {
	m.items = nil

	// General: default provider
	if len(m.providerNames) > 0 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: 0})
	}
	// General: default tracker
	if len(m.trackerNames) > 0 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: 1})
	}

	// Plugin fields
	for ei, e := range m.entries {
		for fi := range e.fields {
			m.items = append(m.items, flatItem{kind: "text", section: ei, fieldIdx: fi})
		}
	}
}

func (m SettingsModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for i, e := range m.entries {
		idx := i
		entry := e.entry
		cmds = append(cmds, func() tea.Msg {
			fields := entry.GetFields()
			values := entry.GetValues()
			return pluginFieldsLoadedMsg{index: idx, fields: fields, values: values}
		})
	}
	return tea.Batch(cmds...)
}

func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case pluginFieldsLoadedMsg:
		if msg.index < len(m.entries) {
			m.entries[msg.index].fields = msg.fields
			m.entries[msg.index].values = msg.values
			m.rebuildItems()
		}
		return m, nil

	case SettingsSavedMsg:
		return m, tea.Quit

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m SettingsModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Paste {
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if item.kind == "text" {
				e := &m.entries[item.section]
				key := e.fields[item.fieldIdx].Key
				e.values[key] += string(msg.Runes)
			}
		}
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
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}

	case "tab":
		if len(m.items) > 0 {
			m.cursor = (m.cursor + 1) % len(m.items)
		}

	case "shift+tab":
		if len(m.items) > 0 {
			m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
		}

	case "left", "right":
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if item.kind == "select" && item.section == -1 {
				if item.fieldIdx == 0 && len(m.providerNames) > 0 {
					if key == "right" {
						m.selectedProvider = (m.selectedProvider + 1) % len(m.providerNames)
					} else {
						m.selectedProvider = (m.selectedProvider - 1 + len(m.providerNames)) % len(m.providerNames)
					}
				} else if item.fieldIdx == 1 && len(m.trackerNames) > 0 {
					if key == "right" {
						m.selectedTracker = (m.selectedTracker + 1) % len(m.trackerNames)
					} else {
						m.selectedTracker = (m.selectedTracker - 1 + len(m.trackerNames)) % len(m.trackerNames)
					}
				}
			}
		}

	case "ctrl+s", "enter":
		return m, m.save()

	case "backspace":
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if item.kind == "text" {
				e := &m.entries[item.section]
				k := e.fields[item.fieldIdx].Key
				v := e.values[k]
				if len(v) > 0 {
					e.values[k] = v[:len(v)-1]
				}
			}
		}

	case "ctrl+u":
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if item.kind == "text" {
				e := &m.entries[item.section]
				k := e.fields[item.fieldIdx].Key
				e.values[k] = ""
			}
		}

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				if item.kind == "text" {
					e := &m.entries[item.section]
					k := e.fields[item.fieldIdx].Key
					e.values[k] += key
				}
			}
		}
	}
	return m, nil
}

func (m SettingsModel) save() tea.Cmd {
	// Update config
	if len(m.providerNames) > 0 {
		m.cfg.DefaultProvider = m.providerNames[m.selectedProvider]
	}
	if len(m.trackerNames) > 0 {
		m.cfg.DefaultTracker = m.trackerNames[m.selectedTracker]
	}

	cfgCopy := *m.cfg

	type saveItem struct {
		entry  plugincfg.Entry
		values map[string]string
	}
	var items []saveItem
	for _, e := range m.entries {
		if len(e.fields) > 0 {
			items = append(items, saveItem{entry: e.entry, values: e.values})
		}
	}

	return func() tea.Msg {
		config.Save(&cfgCopy)
		for _, item := range items {
			item.entry.SetValues(item.values)
		}
		return SettingsSavedMsg{}
	}
}

func (m SettingsModel) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(1, 2).
		Render("Settings")

	itemIdx := 0
	var sections []string

	// General section
	if len(m.providerNames) > 0 || len(m.trackerNames) > 0 {
		header := lipgloss.NewStyle().Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render("General"),
		)
		sections = append(sections, header)

		if len(m.providerNames) > 0 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default Provider", m.providerNames, m.selectedProvider))
			itemIdx++
		}
		if len(m.trackerNames) > 0 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default Tracker", m.trackerNames, m.selectedTracker))
			itemIdx++
		}
		sections = append(sections, "")
	}

	// Plugin sections
	for _, e := range m.entries {
		if len(e.fields) == 0 {
			continue
		}

		category := lipgloss.NewStyle().Foreground(dimColor).Render(e.entry.Category)
		header := lipgloss.NewStyle().Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render(e.entry.Name) + " " + category,
		)
		sections = append(sections, header)

		labelWidth := 0
		for _, f := range e.fields {
			if len(f.Label) > labelWidth {
				labelWidth = len(f.Label)
			}
		}

		for _, f := range e.fields {
			isCursor := itemIdx == m.cursor
			padding := strings.Repeat(" ", labelWidth-len(f.Label))

			labelStyle := lipgloss.NewStyle().Foreground(dimColor)
			indicator := "  "
			if isCursor {
				labelStyle = lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
				indicator = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
			}

			value := e.values[f.Key]
			valueStr := value
			if f.Secret && len(value) > 0 && !isCursor {
				visible := value[max(0, len(value)-4):]
				valueStr = strings.Repeat("*", min(len(value), 8)) + visible
			}
			if isCursor {
				valueStr += lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
			}
			if value == "" && !isCursor {
				if f.Placeholder != "" {
					valueStr = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(" + f.Placeholder + ")")
				} else {
					valueStr = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("-")
				}
			}

			row := "  " + indicator + labelStyle.Render(padding+f.Label+": ") + valueStr
			sections = append(sections, row)
			itemIdx++
		}
		sections = append(sections, "")
	}

	if len(sections) == 0 {
		sections = append(sections, lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render("No configurable plugins installed"))
	}

	content := title + "\n\n" + strings.Join(sections, "\n")

	if m.err != "" {
		content += "\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("196")).Render(m.err)
	}

	lines := strings.Count(content, "\n") + 1
	contentHeight := m.height - 3
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	hint := " ↑↓/Tab: navigate  ←→: select  Enter: save  Esc: cancel"
	statusBar := statusBarStyle.
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(hint)

	return content + "\n" + statusBar
}

func (m SettingsModel) renderSelectField(itemIdx int, label string, options []string, selected int) string {
	isCursor := itemIdx == m.cursor

	labelStyle := lipgloss.NewStyle().Foreground(dimColor)
	indicator := "  "
	if isCursor {
		labelStyle = lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
		indicator = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
	}

	value := options[selected]
	var valueStr string
	if isCursor {
		left := lipgloss.NewStyle().Foreground(primaryColor).Render("◂ ")
		right := lipgloss.NewStyle().Foreground(primaryColor).Render(" ▸")
		valueStr = left + lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render(value) + right
	} else {
		valueStr = lipgloss.NewStyle().Foreground(dimColor).Render(value)
	}

	return "  " + indicator + labelStyle.Render(label+": ") + valueStr
}
