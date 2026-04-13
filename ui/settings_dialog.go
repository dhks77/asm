package ui

import (
	"fmt"
	"strconv"
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
	values map[string]string // current UI values (user + scope changes)
}

// flatItem represents a navigable item in the settings UI.
type flatItem struct {
	kind     string // "scope", "select", "text"
	section  int    // -1 for general, >=0 for plugin index
	fieldIdx int
}

var scopeOptions = []string{"user", "project"}
var autoZoomOptions = []string{"on", "off"}

const pickerWidthMin = 10
const pickerWidthMax = 50

// General field keys used for per-field project-override tracking.
const (
	fieldProvider    = "provider"
	fieldTracker     = "tracker"
	fieldAutoZoom    = "autoZoom"
	fieldPickerWidth = "pickerWidth"
)

type SettingsModel struct {
	userCfg    *config.Config // user-level raw config
	projectCfg *config.Config // project-level raw config
	rootPath   string

	entries []settingsEntry // plugin entries (user scope only)

	providerNames    []string
	trackerNames     []string
	selectedProvider int
	selectedTracker  int
	autoZoomIdx      int    // 0=on, 1=off
	pickerWidthStr   string // free-form percentage input (e.g. "22")

	// Per-field project-scope overrides. true = explicit value in project;
	// false = inherits from user. Only consulted when scopeIdx == 1.
	projectOverrides map[string]bool

	scopeIdx int // 0=user, 1=project

	items  []flatItem
	cursor int
	width  int
	height int
	err    string
}

func NewSettingsModel(_ *config.Config, rootPath string, providerNames []string, trackerNames []string, plugins []plugincfg.Entry) SettingsModel {
	userCfg, _ := config.LoadScope(config.ScopeUser, rootPath)
	projectCfg, _ := config.LoadScope(config.ScopeProject, rootPath)

	entries := make([]settingsEntry, len(plugins))
	for i, p := range plugins {
		entries[i] = settingsEntry{entry: p}
	}

	m := SettingsModel{
		userCfg:       userCfg,
		projectCfg:    projectCfg,
		rootPath:      rootPath,
		entries:       entries,
		providerNames: providerNames,
		trackerNames:  trackerNames,
		projectOverrides: map[string]bool{
			fieldProvider:    projectCfg.DefaultProvider != "",
			fieldTracker:     projectCfg.DefaultTracker != "",
			fieldAutoZoom:    projectCfg.AutoZoom != nil,
			fieldPickerWidth: projectCfg.PickerWidth != 0,
		},
	}
	// Default to project scope when a project context exists so overrides
	// are front-and-center; fall back to user scope otherwise.
	if rootPath != "" {
		m.scopeIdx = 1
	}
	m.loadGeneralFromScope()
	m.rebuildItems()
	return m
}

// loadGeneralFromScope syncs the general-section UI state (provider, tracker,
// auto zoom, picker width) with the currently selected scope. For project
// scope, fields that are not explicitly overridden show the user-scope value
// so the user can see what they would inherit.
func (m *SettingsModel) loadGeneralFromScope() {
	isProject := m.currentScope() == config.ScopeProject

	providerName := m.projectCfg.DefaultProvider
	if !isProject || !m.projectOverrides[fieldProvider] {
		providerName = m.userCfg.DefaultProvider
	}
	m.selectedProvider = 0
	for i, n := range m.providerNames {
		if n == providerName {
			m.selectedProvider = i
			break
		}
	}

	trackerName := m.projectCfg.DefaultTracker
	if !isProject || !m.projectOverrides[fieldTracker] {
		trackerName = m.userCfg.DefaultTracker
	}
	m.selectedTracker = 0
	for i, n := range m.trackerNames {
		if n == trackerName {
			m.selectedTracker = i
			break
		}
	}

	azEnabled := m.userCfg.IsAutoZoomEnabled()
	if isProject && m.projectOverrides[fieldAutoZoom] {
		azEnabled = m.projectCfg.IsAutoZoomEnabled()
	}
	if azEnabled {
		m.autoZoomIdx = 0
	} else {
		m.autoZoomIdx = 1
	}

	pw := m.userCfg.GetPickerWidth()
	if isProject && m.projectOverrides[fieldPickerWidth] {
		pw = m.projectCfg.GetPickerWidth()
	}
	m.pickerWidthStr = fmt.Sprintf("%d", pw)
}

// persistGeneralToScope writes the current UI state back into the currently
// selected scope's in-memory *config.Config. This lets scope switches and
// the final save preserve edits made across both scopes in one session.
func (m *SettingsModel) persistGeneralToScope() {
	scope := m.currentScope()
	cfg := m.currentCfg()

	if scope == config.ScopeProject {
		if m.projectOverrides[fieldProvider] && len(m.providerNames) > 0 {
			cfg.DefaultProvider = m.providerNames[m.selectedProvider]
		} else {
			cfg.DefaultProvider = ""
		}
		if m.projectOverrides[fieldTracker] && len(m.trackerNames) > 0 {
			cfg.DefaultTracker = m.trackerNames[m.selectedTracker]
		} else {
			cfg.DefaultTracker = ""
		}
		if m.projectOverrides[fieldAutoZoom] {
			azOn := m.autoZoomIdx == 0
			cfg.AutoZoom = &azOn
		} else {
			cfg.AutoZoom = nil
		}
		if m.projectOverrides[fieldPickerWidth] {
			if w := parsePickerWidth(m.pickerWidthStr); w > 0 {
				cfg.PickerWidth = w
			} else {
				cfg.PickerWidth = 0
			}
		} else {
			cfg.PickerWidth = 0
		}
	} else {
		if len(m.providerNames) > 0 {
			cfg.DefaultProvider = m.providerNames[m.selectedProvider]
		}
		if len(m.trackerNames) > 0 {
			cfg.DefaultTracker = m.trackerNames[m.selectedTracker]
		}
		azOn := m.autoZoomIdx == 0
		cfg.AutoZoom = &azOn
		if w := parsePickerWidth(m.pickerWidthStr); w > 0 {
			cfg.PickerWidth = w
		}
	}

	// Persist tracker-entry edits for built-in trackers into the scope's cfg.
	if cfg.Trackers == nil {
		cfg.Trackers = make(map[string]map[string]string)
	}
	for _, e := range m.entries {
		if e.entry.Source == nil || e.entry.Category != "tracker" {
			continue
		}
		if scope == config.ScopeProject {
			filtered := make(map[string]string, len(e.values))
			for k, v := range e.values {
				if v != "" {
					filtered[k] = v
				}
			}
			if len(filtered) == 0 {
				delete(cfg.Trackers, e.entry.Name)
			} else {
				cfg.Trackers[e.entry.Name] = filtered
			}
		} else {
			cfg.Trackers[e.entry.Name] = copyMap(e.values)
		}
	}
}

// markGeneralOverride marks a general field as explicitly overridden when in
// project scope. No-op in user scope.
func (m *SettingsModel) markGeneralOverride(fieldIdx int) {
	if m.currentScope() != config.ScopeProject {
		return
	}
	switch fieldIdx {
	case 0:
		m.projectOverrides[fieldProvider] = true
	case 1:
		m.projectOverrides[fieldTracker] = true
	case 2:
		m.projectOverrides[fieldAutoZoom] = true
	case 3:
		m.projectOverrides[fieldPickerWidth] = true
	}
}

// generalStateMarker returns a state marker suffix for a general field in
// project scope:
//   - "● project"                      → explicitly overridden in project
//   - "○ inherit (user: <value>)"      → inheriting from user scope
//
// Returns empty string in user scope.
func (m *SettingsModel) generalStateMarker(fieldIdx int) string {
	if m.currentScope() != config.ScopeProject {
		return ""
	}
	key := ""
	switch fieldIdx {
	case 0:
		key = fieldProvider
	case 1:
		key = fieldTracker
	case 2:
		key = fieldAutoZoom
	case 3:
		key = fieldPickerWidth
	}
	if key == "" {
		return ""
	}
	if m.projectOverrides[key] {
		return lipgloss.NewStyle().Foreground(primaryColor).Render("  ● project")
	}
	// Inheriting — also surface the user value.
	userVal := ""
	switch fieldIdx {
	case 0:
		userVal = m.userCfg.DefaultProvider
	case 1:
		userVal = m.userCfg.DefaultTracker
	case 2:
		if m.userCfg.IsAutoZoomEnabled() {
			userVal = "on"
		} else {
			userVal = "off"
		}
	case 3:
		userVal = fmt.Sprintf("%d%%", m.userCfg.GetPickerWidth())
	}
	suffix := "  ○ inherit"
	if userVal != "" {
		suffix += " (user: " + userVal + ")"
	}
	return lipgloss.NewStyle().Foreground(dimColor).Render(suffix)
}

// trackerFieldStateMarker returns a state marker for a tracker/plugin field
// in project scope. Empty string otherwise.
func (m *SettingsModel) trackerFieldStateMarker(entryName, key, currentValue string, isSecret bool) string {
	if m.currentScope() != config.ScopeProject {
		return ""
	}
	if currentValue != "" {
		return lipgloss.NewStyle().Foreground(primaryColor).Render("  ● project")
	}
	userVal := ""
	if fields, ok := m.userCfg.Trackers[entryName]; ok {
		userVal = fields[key]
	}
	suffix := "  ○ inherit"
	if userVal != "" {
		display := userVal
		if isSecret {
			visible := userVal[max(0, len(userVal)-4):]
			display = strings.Repeat("*", min(len(userVal), 8)) + visible
		}
		suffix += " (user: " + display + ")"
	} else {
		suffix += " (unset)"
	}
	return lipgloss.NewStyle().Foreground(dimColor).Render(suffix)
}

func (m *SettingsModel) currentScope() config.Scope {
	if m.scopeIdx == 1 {
		return config.ScopeProject
	}
	return config.ScopeUser
}

func (m *SettingsModel) currentCfg() *config.Config {
	if m.currentScope() == config.ScopeProject {
		return m.projectCfg
	}
	return m.userCfg
}

func (m *SettingsModel) rebuildItems() {
	m.items = nil

	// Scope selector
	m.items = append(m.items, flatItem{kind: "scope", section: -1, fieldIdx: -1})

	// General: default provider
	if len(m.providerNames) > 0 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: 0})
	}
	if len(m.trackerNames) > 0 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: 1})
	}
	// Auto zoom toggle
	m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: 2})
	// Picker width (free-form number input)
	m.items = append(m.items, flatItem{kind: "number", section: -1, fieldIdx: 3})

	// Plugin fields
	for ei, e := range m.entries {
		for fi := range e.fields {
			m.items = append(m.items, flatItem{kind: "text", section: ei, fieldIdx: fi})
		}
	}
}

// builtinValuesFromCfg returns the current scope's values for a built-in entry.
func (m *SettingsModel) builtinValuesFromCfg(entry plugincfg.Entry) map[string]string {
	cfg := m.currentCfg()
	if entry.Category == "tracker" {
		if v, ok := cfg.Trackers[entry.Name]; ok {
			return copyMap(v)
		}
		// Also match "dooray" display
		for name, fields := range cfg.Trackers {
			if strings.EqualFold(name, entry.Name) {
				return copyMap(fields)
			}
		}
	}
	return make(map[string]string)
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (m SettingsModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for i, e := range m.entries {
		idx := i
		entry := e.entry
		// For built-in configurables, load values from current scope config
		if entry.Source != nil {
			values := m.builtinValuesFromCfg(entry)
			fields := entry.GetFields()
			cmds = append(cmds, func() tea.Msg {
				return pluginFieldsLoadedMsg{index: idx, fields: fields, values: values}
			})
		} else {
			cmds = append(cmds, func() tea.Msg {
				fields := entry.GetFields()
				values := entry.GetValues()
				return pluginFieldsLoadedMsg{index: idx, fields: fields, values: values}
			})
		}
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
			if item.kind == "scope" {
				// Save current scope's UI state into its cfg so edits are
				// preserved when we come back to it (or hit save).
				m.persistGeneralToScope()

				delta := 1
				if key == "left" {
					delta = -1
				}
				m.scopeIdx = (m.scopeIdx + delta + len(scopeOptions)) % len(scopeOptions)
				// Reload general and built-in entry values from new scope's config.
				m.loadGeneralFromScope()
				for i := range m.entries {
					if m.entries[i].entry.Source != nil {
						m.entries[i].values = m.builtinValuesFromCfg(m.entries[i].entry)
					}
				}
			} else if item.kind == "select" && item.section == -1 {
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
				} else if item.fieldIdx == 2 {
					if key == "right" {
						m.autoZoomIdx = (m.autoZoomIdx + 1) % len(autoZoomOptions)
					} else {
						m.autoZoomIdx = (m.autoZoomIdx - 1 + len(autoZoomOptions)) % len(autoZoomOptions)
					}
				}
				m.markGeneralOverride(item.fieldIdx)
			} else if item.kind == "number" && item.section == -1 && item.fieldIdx == 3 {
				// ←/→ step picker width by 1, clamped
				delta := 1
				if key == "left" {
					delta = -1
				}
				v := parsePickerWidth(m.pickerWidthStr) + delta
				if v < pickerWidthMin {
					v = pickerWidthMin
				}
				if v > pickerWidthMax {
					v = pickerWidthMax
				}
				m.pickerWidthStr = fmt.Sprintf("%d", v)
				m.markGeneralOverride(item.fieldIdx)
			}
		}

	case "ctrl+s", "enter":
		return m, m.save()

	case "ctrl+r":
		// Clear the current field's project override (inherit from user).
		// Only meaningful in project scope.
		if m.currentScope() != config.ScopeProject || m.cursor >= len(m.items) {
			return m, nil
		}
		item := m.items[m.cursor]
		if item.section == -1 && (item.kind == "select" || item.kind == "number") {
			switch item.fieldIdx {
			case 0:
				m.projectOverrides[fieldProvider] = false
			case 1:
				m.projectOverrides[fieldTracker] = false
			case 2:
				m.projectOverrides[fieldAutoZoom] = false
			case 3:
				m.projectOverrides[fieldPickerWidth] = false
			}
			m.loadGeneralFromScope()
		} else if item.kind == "text" {
			e := &m.entries[item.section]
			k := e.fields[item.fieldIdx].Key
			e.values[k] = ""
		}

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
			} else if item.kind == "number" && len(m.pickerWidthStr) > 0 {
				m.pickerWidthStr = m.pickerWidthStr[:len(m.pickerWidthStr)-1]
				m.markGeneralOverride(item.fieldIdx)
			}
		}

	case "ctrl+u":
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if item.kind == "text" {
				e := &m.entries[item.section]
				k := e.fields[item.fieldIdx].Key
				e.values[k] = ""
			} else if item.kind == "number" {
				m.pickerWidthStr = ""
				m.markGeneralOverride(item.fieldIdx)
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
				} else if item.kind == "number" && key[0] >= '0' && key[0] <= '9' {
					// Cap length at 3 digits
					if len(m.pickerWidthStr) < 3 {
						m.pickerWidthStr += key
					}
					m.markGeneralOverride(item.fieldIdx)
				}
			}
		}
	}
	return m, nil
}

func (m SettingsModel) save() tea.Cmd {
	// Persist the active scope's in-memory state. (The inactive scope's
	// cfg already holds its latest state because persistGeneralToScope
	// was called each time we switched away from it.)
	m.persistGeneralToScope()

	userCfg := *m.userCfg
	projectCfg := *m.projectCfg
	rootPath := m.rootPath

	type saveItem struct {
		entry  plugincfg.Entry
		values map[string]string
	}
	var pluginItems []saveItem
	for _, e := range m.entries {
		if e.entry.Source == nil && len(e.fields) > 0 {
			pluginItems = append(pluginItems, saveItem{entry: e.entry, values: e.values})
		}
	}

	return func() tea.Msg {
		config.SaveScope(&userCfg, config.ScopeUser, rootPath)
		if rootPath != "" {
			config.SaveScope(&projectCfg, config.ScopeProject, rootPath)
		}
		// Plugin configs are always user-scoped (plugin manages its own storage)
		for _, item := range pluginItems {
			item.entry.SetValues(item.values)
		}
		return SettingsSavedMsg{}
	}
}

func (m SettingsModel) View() string {
	title := renderDialogTitle("Settings", primaryColor)

	itemIdx := 0
	var sections []string

	// Scope selector
	sections = append(sections, m.renderSelectField(itemIdx, "Scope", scopeOptions, m.scopeIdx))
	itemIdx++
	sections = append(sections, "")

	// General section (always shown — auto_zoom is always present)
	{
		header := lipgloss.NewStyle().Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render("General"),
		)
		sections = append(sections, header)

		if len(m.providerNames) > 0 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default Provider", m.providerNames, m.selectedProvider)+m.generalStateMarker(0))
			itemIdx++
		}
		if len(m.trackerNames) > 0 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default Tracker", m.trackerNames, m.selectedTracker)+m.generalStateMarker(1))
			itemIdx++
		}
		sections = append(sections, m.renderSelectField(itemIdx, "Auto Zoom", autoZoomOptions, m.autoZoomIdx)+m.generalStateMarker(2))
		itemIdx++
		sections = append(sections, m.renderNumberField(itemIdx, "Picker Width", m.pickerWidthStr, "%")+m.generalStateMarker(3))
		itemIdx++
		sections = append(sections, "")
	}

	// Plugin/built-in sections
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
			indicator, labelStyle := fieldRowCursor(isCursor)

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
			row += m.trackerFieldStateMarker(e.entry.Name, f.Key, value, f.Secret)
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
		content += "\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dangerColor).Render(m.err)
	}

	content = padToHeight(content, m.height-3)
	statusBar := renderDialogHintBar(m.width,
		" ↑↓/Tab: navigate  ←→: select  Ctrl+R: inherit  Enter: save  Esc: cancel")
	return content + "\n" + statusBar
}

// renderNumberField renders a free-form numeric input with a trailing unit suffix.
func (m SettingsModel) renderNumberField(itemIdx int, label, value, unit string) string {
	isCursor := itemIdx == m.cursor
	indicator, labelStyle := fieldRowCursor(isCursor)

	display := value
	if display == "" {
		display = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("-")
	} else {
		display = lipgloss.NewStyle().Foreground(whiteColor).Render(display)
	}
	if isCursor {
		display += lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
	}
	display += lipgloss.NewStyle().Foreground(dimColor).Render(unit)

	hint := ""
	if isCursor {
		hint = lipgloss.NewStyle().Foreground(dimColor).Render(
			fmt.Sprintf("   (←/→ ±1, range %d–%d)", pickerWidthMin, pickerWidthMax))
	}

	return "  " + indicator + labelStyle.Render(label+": ") + display + hint
}

// parsePickerWidth parses s as an int and clamps to the picker-width range.
// Returns 0 if the input isn't a valid number.
func parsePickerWidth(s string) int {
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0
	}
	if v < pickerWidthMin {
		return pickerWidthMin
	}
	if v > pickerWidthMax {
		return pickerWidthMax
	}
	return v
}

func (m SettingsModel) renderSelectField(itemIdx int, label string, options []string, selected int) string {
	isCursor := itemIdx == m.cursor
	indicator, labelStyle := fieldRowCursor(isCursor)

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
