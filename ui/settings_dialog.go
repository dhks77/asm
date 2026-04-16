package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/ide"
	"github.com/nhn/asm/plugincfg"
	"github.com/nhn/asm/worktree"
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
//
// IDE-related kinds:
//
//	"ide-name", "ide-cmd", "ide-args" — editable fields. section is the
//	  index into m.ideEntries.
//	"ide-add" — a single trailing row. Enter appends a new ideEditEntry.
type flatItem struct {
	kind     string // "scope", "select", "text", "number", "action", "ide-name", "ide-cmd", "ide-args", "ide-add"
	section  int    // -1 for general, >=0 for plugin index (or IDE index when kind starts with "ide-")
	fieldIdx int
}

var scopeOptions = []string{"global", "local"}
var autoZoomOptions = []string{"on", "off"}
var templateConflictOptions = []string{"skip", "overwrite"}

const pickerWidthMin = 10
const pickerWidthMax = 50

// General field keys used for per-field project-override tracking.
const (
	fieldProvider         = "provider"
	fieldTracker          = "tracker"
	fieldAutoZoom         = "autoZoom"
	fieldPickerWidth      = "pickerWidth"
	fieldIDE              = "ide"
	fieldTemplateConflict = "templateConflict"
	fieldWorktreeBasePath = "worktreeBasePath"
)

// Stable identifiers for general-section rows. These are intentionally
// decoupled from on-screen order because some rows are conditionally hidden
// (for example, global-only fields disappear in local scope).
const (
	generalFieldProvider = iota
	generalFieldTracker
	generalFieldAutoZoom
	generalFieldPickerWidth
	generalFieldIDE
	generalFieldTemplateConflict
	generalFieldOpenTemplatesDir
	generalFieldWorktreeBasePath
)

// ideNoneLabel is the sentinel shown at index 0 of the Default IDE
// select, meaning "don't pick a default — always show the selector".
const ideNoneLabel = "(none)"

type SettingsModel struct {
	userCfg    *config.Config // user-level raw config
	projectCfg *config.Config // project-level raw config
	rootPath   string

	entries []settingsEntry // plugin entries (user scope only)
	// ideEntries are the editable IDE launchers shown in the Settings UI.
	// Built-ins are always present; user-added entries follow. On save
	// they're serialized back into cfg.IDEs.
	ideEntries []ideEditEntry

	providerNames []string
	trackerNames  []string
	// ideNames is the display list for Default IDE. Index 0 is always
	// ideNoneLabel; indices 1.. are the configured IDEs in order.
	ideNames            []string
	selectedProvider    int
	selectedTracker     int
	selectedIDE         int    // 0 = none, 1+ = ideNames[i]
	autoZoomIdx         int    // 0=on, 1=off
	pickerWidthStr      string // free-form percentage input (e.g. "22")
	templateConflictIdx int    // 0=skip, 1=overwrite
	worktreeBasePathStr string // free-form path for repo-mode worktree creation

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

func NewSettingsModel(_ *config.Config, rootPath string, providerNames []string, trackerNames []string, ideNames []string, plugins []plugincfg.Entry) SettingsModel {
	userCfg, _ := config.LoadScope(config.ScopeUser, rootPath)
	projectCfg, _ := config.LoadScope(config.ScopeProject, rootPath)

	entries := make([]settingsEntry, len(plugins))
	for i, p := range plugins {
		entries[i] = settingsEntry{entry: p}
	}

	// Prepend "(none)" so the user can explicitly choose "no default" and
	// always see the IDE selector.
	displayIDEs := append([]string{ideNoneLabel}, ideNames...)

	m := SettingsModel{
		userCfg:       userCfg,
		projectCfg:    projectCfg,
		rootPath:      rootPath,
		entries:       entries,
		ideEntries:    loadIDEEntries(userCfg),
		providerNames: providerNames,
		trackerNames:  trackerNames,
		ideNames:      displayIDEs,
		projectOverrides: map[string]bool{
			fieldProvider:         projectCfg.DefaultProvider != "",
			fieldTracker:          projectCfg.DefaultTracker != "",
			fieldAutoZoom:         projectCfg.AutoZoom != nil,
			fieldPickerWidth:      projectCfg.PickerWidth != 0,
			fieldIDE:              projectCfg.DefaultIDE != "",
			fieldTemplateConflict: projectCfg.WorktreeTemplate.OnConflict != "",
			fieldWorktreeBasePath: projectCfg.WorktreeBasePath != "",
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

	ideName := m.projectCfg.DefaultIDE
	if !isProject || !m.projectOverrides[fieldIDE] {
		ideName = m.userCfg.DefaultIDE
	}
	m.selectedIDE = 0 // default to "(none)"
	if ideName != "" {
		for i, n := range m.ideNames {
			if n == ideName {
				m.selectedIDE = i
				break
			}
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

	tcPolicy := m.userCfg.TemplateConflictPolicy()
	if isProject && m.projectOverrides[fieldTemplateConflict] {
		tcPolicy = m.projectCfg.TemplateConflictPolicy()
	}
	m.templateConflictIdx = 0
	for i, opt := range templateConflictOptions {
		if opt == tcPolicy {
			m.templateConflictIdx = i
			break
		}
	}

	wtBase := m.userCfg.WorktreeBasePath
	if isProject && m.projectOverrides[fieldWorktreeBasePath] {
		wtBase = m.projectCfg.WorktreeBasePath
	}
	m.worktreeBasePathStr = wtBase
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
		if m.projectOverrides[fieldIDE] && m.selectedIDE > 0 {
			cfg.DefaultIDE = m.ideNames[m.selectedIDE]
		} else {
			cfg.DefaultIDE = ""
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
		if m.projectOverrides[fieldTemplateConflict] {
			cfg.WorktreeTemplate.OnConflict = templateConflictOptions[m.templateConflictIdx]
		} else {
			cfg.WorktreeTemplate.OnConflict = ""
		}
		if m.projectOverrides[fieldWorktreeBasePath] {
			cfg.WorktreeBasePath = strings.TrimSpace(m.worktreeBasePathStr)
		} else {
			cfg.WorktreeBasePath = ""
		}
	} else {
		if len(m.providerNames) > 0 {
			cfg.DefaultProvider = m.providerNames[m.selectedProvider]
		}
		if len(m.trackerNames) > 0 {
			cfg.DefaultTracker = m.trackerNames[m.selectedTracker]
		}
		if m.selectedIDE > 0 {
			cfg.DefaultIDE = m.ideNames[m.selectedIDE]
		} else {
			cfg.DefaultIDE = ""
		}
		azOn := m.autoZoomIdx == 0
		cfg.AutoZoom = &azOn
		if w := parsePickerWidth(m.pickerWidthStr); w > 0 {
			cfg.PickerWidth = w
		}
		cfg.WorktreeTemplate.OnConflict = templateConflictOptions[m.templateConflictIdx]
		cfg.WorktreeBasePath = strings.TrimSpace(m.worktreeBasePathStr)
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
	if key := generalFieldKey(fieldIdx); key != "" {
		m.projectOverrides[key] = true
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
	key := generalFieldKey(fieldIdx)
	if key == "" {
		return ""
	}
	if m.projectOverrides[key] {
		return lipgloss.NewStyle().Foreground(primaryColor).Render("  ● local")
	}
	// Inheriting — also surface the user value.
	userVal := m.generalInheritedValue(fieldIdx)
	suffix := "  ○ inherit"
	if userVal != "" {
		suffix += " (global: " + userVal + ")"
	}
	return lipgloss.NewStyle().Foreground(dimColor).Render(suffix)
}

func generalFieldKey(fieldIdx int) string {
	switch fieldIdx {
	case generalFieldProvider:
		return fieldProvider
	case generalFieldTracker:
		return fieldTracker
	case generalFieldAutoZoom:
		return fieldAutoZoom
	case generalFieldPickerWidth:
		return fieldPickerWidth
	case generalFieldIDE:
		return fieldIDE
	case generalFieldTemplateConflict:
		return fieldTemplateConflict
	case generalFieldWorktreeBasePath:
		return fieldWorktreeBasePath
	default:
		return ""
	}
}

func (m *SettingsModel) generalInheritedValue(fieldIdx int) string {
	switch fieldIdx {
	case generalFieldProvider:
		return m.userCfg.DefaultProvider
	case generalFieldTracker:
		return m.userCfg.DefaultTracker
	case generalFieldAutoZoom:
		if m.userCfg.IsAutoZoomEnabled() {
			return "on"
		}
		return "off"
	case generalFieldPickerWidth:
		return fmt.Sprintf("%d%%", m.userCfg.GetPickerWidth())
	case generalFieldIDE:
		if m.userCfg.DefaultIDE != "" {
			return m.userCfg.DefaultIDE
		}
		return ideNoneLabel
	case generalFieldTemplateConflict:
		return m.userCfg.TemplateConflictPolicy()
	case generalFieldWorktreeBasePath:
		return m.userCfg.WorktreeBasePath
	default:
		return ""
	}
}

// trackerFieldStateMarker returns a state marker for a tracker/plugin field
// in project scope. Empty string otherwise.
func (m *SettingsModel) trackerFieldStateMarker(entryName, key, currentValue string, isSecret bool) string {
	if m.currentScope() != config.ScopeProject {
		return ""
	}
	if currentValue != "" {
		return lipgloss.NewStyle().Foreground(primaryColor).Render("  ● local")
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
		suffix += " (global: " + display + ")"
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
	isLocal := m.currentScope() == config.ScopeProject
	showWorktree := isLocal && worktree.IsRepoMode(m.rootPath)

	// Scope selector
	m.items = append(m.items, flatItem{kind: "scope", section: -1, fieldIdx: -1})

	// General: default provider
	if len(m.providerNames) > 0 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: generalFieldProvider})
	}
	if len(m.trackerNames) > 0 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: generalFieldTracker})
	}
	// Default IDE — (none) at index 0 disables the default and always
	// shows the selector on Ctrl+e.
	if len(m.ideNames) > 1 {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: generalFieldIDE})
	}
	if !isLocal {
		// Auto zoom and picker width are global-only UI settings.
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: generalFieldAutoZoom})
		m.items = append(m.items, flatItem{kind: "number", section: -1, fieldIdx: generalFieldPickerWidth})
	}

	if showWorktree {
		m.items = append(m.items, flatItem{kind: "select", section: -1, fieldIdx: generalFieldTemplateConflict})
		m.items = append(m.items, flatItem{kind: "text", section: -1, fieldIdx: generalFieldWorktreeBasePath})
		m.items = append(m.items, flatItem{kind: "action", section: -1, fieldIdx: generalFieldOpenTemplatesDir})
	}

	if !isLocal {
		// Plugin fields and IDE launcher definitions are global-only.
		for ei, e := range m.entries {
			for fi := range e.fields {
				m.items = append(m.items, flatItem{kind: "text", section: ei, fieldIdx: fi})
			}
		}

		// IDE entries. Each custom entry exposes all three fields (name,
		// command, args); built-ins hide the name field since renaming a
		// builtin would just create a phantom entry on save.
		for i, e := range m.ideEntries {
			if !e.IsBuiltin {
				m.items = append(m.items, flatItem{kind: "ide-name", section: i})
			}
			m.items = append(m.items, flatItem{kind: "ide-cmd", section: i})
			m.items = append(m.items, flatItem{kind: "ide-args", section: i})
		}
		m.items = append(m.items, flatItem{kind: "ide-add"})
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
			if p := m.generalTextPtr(item); p != nil {
				*p += string(msg.Runes)
				m.markGeneralOverride(item.fieldIdx)
			} else if item.kind == "text" {
				e := &m.entries[item.section]
				key := e.fields[item.fieldIdx].Key
				e.values[key] += string(msg.Runes)
			} else if isIDEField(item.kind) {
				if p := m.ideFieldPtr(item.section, item.kind); p != nil {
					*p += string(msg.Runes)
				}
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
				m.rebuildItems()
				if m.cursor >= len(m.items) {
					m.cursor = max(0, len(m.items)-1)
				}
			} else if item.kind == "select" && item.section == -1 {
				if item.fieldIdx == generalFieldProvider && len(m.providerNames) > 0 {
					if key == "right" {
						m.selectedProvider = (m.selectedProvider + 1) % len(m.providerNames)
					} else {
						m.selectedProvider = (m.selectedProvider - 1 + len(m.providerNames)) % len(m.providerNames)
					}
				} else if item.fieldIdx == generalFieldTracker && len(m.trackerNames) > 0 {
					if key == "right" {
						m.selectedTracker = (m.selectedTracker + 1) % len(m.trackerNames)
					} else {
						m.selectedTracker = (m.selectedTracker - 1 + len(m.trackerNames)) % len(m.trackerNames)
					}
				} else if item.fieldIdx == generalFieldAutoZoom {
					if key == "right" {
						m.autoZoomIdx = (m.autoZoomIdx + 1) % len(autoZoomOptions)
					} else {
						m.autoZoomIdx = (m.autoZoomIdx - 1 + len(autoZoomOptions)) % len(autoZoomOptions)
					}
				} else if item.fieldIdx == generalFieldIDE && len(m.ideNames) > 0 {
					if key == "right" {
						m.selectedIDE = (m.selectedIDE + 1) % len(m.ideNames)
					} else {
						m.selectedIDE = (m.selectedIDE - 1 + len(m.ideNames)) % len(m.ideNames)
					}
				} else if item.fieldIdx == generalFieldTemplateConflict {
					if key == "right" {
						m.templateConflictIdx = (m.templateConflictIdx + 1) % len(templateConflictOptions)
					} else {
						m.templateConflictIdx = (m.templateConflictIdx - 1 + len(templateConflictOptions)) % len(templateConflictOptions)
					}
				}
				m.markGeneralOverride(item.fieldIdx)
			} else if item.kind == "number" && item.section == -1 && item.fieldIdx == generalFieldPickerWidth {
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

	case "ctrl+s":
		return m, m.save()

	case "enter":
		// Enter on an "action" row invokes that action instead of saving.
		if m.cursor < len(m.items) && m.items[m.cursor].kind == "action" {
			item := m.items[m.cursor]
			if item.section == -1 && item.fieldIdx == generalFieldOpenTemplatesDir {
				m.err = ""
				if _, err := worktree.OpenTemplatesDir(config.ProjectRoot(m.rootPath)); err != nil {
					m.err = fmt.Sprintf("open templates dir failed: %v", err)
				}
			}
			return m, nil
		}
		// Enter on the "+ Add IDE" row appends a new editable entry;
		// anywhere else it still saves.
		if m.cursor < len(m.items) && m.items[m.cursor].kind == "ide-add" {
			m.ideEntries = append(m.ideEntries, ideEditEntry{IsNew: true})
			newIdx := len(m.ideEntries) - 1
			m.rebuildItems()
			// Move cursor onto the new entry's name field.
			for i, it := range m.items {
				if it.kind == "ide-name" && it.section == newIdx {
					m.cursor = i
					break
				}
			}
			return m, nil
		}
		return m, m.save()

	case "ctrl+x":
		// Delete the current IDE entry. For built-ins this just clears
		// any override (next load restores the default command/args).
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if isIDEField(item.kind) && item.section >= 0 && item.section < len(m.ideEntries) {
				e := m.ideEntries[item.section]
				if e.IsBuiltin {
					// Reset the builtin's fields back to the default.
					defaults := ide.Builtins(nil)
					for _, b := range defaults {
						if b.Name == e.Name {
							m.ideEntries[item.section].CommandStr = b.Command
							m.ideEntries[item.section].ArgsStr = formatArgs(b.Args)
							break
						}
					}
				} else {
					m.ideEntries = append(m.ideEntries[:item.section], m.ideEntries[item.section+1:]...)
				}
				m.rebuildItems()
				if m.cursor >= len(m.items) {
					m.cursor = len(m.items) - 1
				}
			}
		}

	case "ctrl+r":
		// Clear the current field's project override (inherit from user).
		// Only meaningful in project scope.
		if m.currentScope() != config.ScopeProject || m.cursor >= len(m.items) {
			return m, nil
		}
		item := m.items[m.cursor]
		if item.section == -1 && (item.kind == "select" || item.kind == "number") {
			if key := generalFieldKey(item.fieldIdx); key != "" {
				m.projectOverrides[key] = false
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
			if p := m.generalTextPtr(item); p != nil {
				if len(*p) > 0 {
					*p = (*p)[:len(*p)-1]
					m.markGeneralOverride(item.fieldIdx)
				}
			} else if item.kind == "text" {
				e := &m.entries[item.section]
				k := e.fields[item.fieldIdx].Key
				v := e.values[k]
				if len(v) > 0 {
					e.values[k] = v[:len(v)-1]
				}
			} else if item.kind == "number" && len(m.pickerWidthStr) > 0 {
				m.pickerWidthStr = m.pickerWidthStr[:len(m.pickerWidthStr)-1]
				m.markGeneralOverride(item.fieldIdx)
			} else if isIDEField(item.kind) {
				if p := m.ideFieldPtr(item.section, item.kind); p != nil && len(*p) > 0 {
					*p = (*p)[:len(*p)-1]
				}
			}
		}

	case "ctrl+u":
		if m.cursor < len(m.items) {
			item := m.items[m.cursor]
			if p := m.generalTextPtr(item); p != nil {
				*p = ""
				m.markGeneralOverride(item.fieldIdx)
			} else if item.kind == "text" {
				e := &m.entries[item.section]
				k := e.fields[item.fieldIdx].Key
				e.values[k] = ""
			} else if item.kind == "number" {
				m.pickerWidthStr = ""
				m.markGeneralOverride(item.fieldIdx)
			} else if isIDEField(item.kind) {
				if p := m.ideFieldPtr(item.section, item.kind); p != nil {
					*p = ""
				}
			}
		}

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				if p := m.generalTextPtr(item); p != nil {
					*p += key
					m.markGeneralOverride(item.fieldIdx)
				} else if item.kind == "text" {
					e := &m.entries[item.section]
					k := e.fields[item.fieldIdx].Key
					e.values[k] += key
				} else if item.kind == "number" && key[0] >= '0' && key[0] <= '9' {
					// Cap length at 3 digits
					if len(m.pickerWidthStr) < 3 {
						m.pickerWidthStr += key
					}
					m.markGeneralOverride(item.fieldIdx)
				} else if isIDEField(item.kind) {
					if p := m.ideFieldPtr(item.section, item.kind); p != nil {
						*p += key
					}
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

	// IDEs are always global-scoped in the settings UI — target-local
	// per-IDE overrides aren't worth the complexity right now.
	saveIDEEntries(m.userCfg, m.ideEntries)

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
	isLocal := m.currentScope() == config.ScopeProject
	showWorktree := isLocal && worktree.IsRepoMode(m.rootPath)

	itemIdx := 0
	var sections []string

	// Scope selector
	sections = append(sections, m.renderSelectField(itemIdx, "Scope", scopeOptions, m.scopeIdx))
	itemIdx++
	sections = append(sections, "")

	// General section
	{
		header := lipgloss.NewStyle().Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render("General"),
		)
		sections = append(sections, header)

		if len(m.providerNames) > 0 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default Provider", m.providerNames, m.selectedProvider)+m.generalStateMarker(generalFieldProvider))
			itemIdx++
		}
		if len(m.trackerNames) > 0 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default Tracker", m.trackerNames, m.selectedTracker)+m.generalStateMarker(generalFieldTracker))
			itemIdx++
		}
		if len(m.ideNames) > 1 {
			sections = append(sections, m.renderSelectField(itemIdx, "Default IDE", m.ideNames, m.selectedIDE)+m.generalStateMarker(generalFieldIDE))
			itemIdx++
		}
		if !isLocal {
			sections = append(sections, m.renderSelectField(itemIdx, "Hide Picker On Open", autoZoomOptions, m.autoZoomIdx))
			itemIdx++
			sections = append(sections, m.renderNumberField(itemIdx, "Picker Width", m.pickerWidthStr, "%"))
			itemIdx++
		}
		sections = append(sections, "")
	}

	// Worktree section
	if showWorktree {
		header := lipgloss.NewStyle().Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render("Worktree"),
		)
		sections = append(sections, header)

		sections = append(sections, m.renderSelectField(itemIdx, "Template on Conflict", templateConflictOptions, m.templateConflictIdx)+m.generalStateMarker(generalFieldTemplateConflict))
		itemIdx++
		sections = append(sections, m.renderTextField(itemIdx, "Worktree Base Path", m.worktreeBasePathStr, "default: ~/worktrees/{repo} — use {repo} to group by repo name")+m.generalStateMarker(generalFieldWorktreeBasePath))
		itemIdx++
		sections = append(sections, m.renderActionField(itemIdx, "Open templates directory", worktree.TemplatesRoot(config.ProjectRoot(m.rootPath))))
		itemIdx++
		sections = append(sections, "")
	}

	// Plugin/built-in sections
	if !isLocal {
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
	}

	// IDEs section — editable list of command/args, plus a trailing
	// "+ Add IDE" row. Built-ins show (builtin) tag; new (unsaved)
	// entries show (new). Deleting a custom entry is Ctrl+X; on a
	// built-in Ctrl+X restores the default command/args.
	if !isLocal {
		header := lipgloss.NewStyle().Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true).Render("IDEs") + " " +
				lipgloss.NewStyle().Foreground(dimColor).Render("(global scope)"),
		)
		sections = append(sections, header)

		for i, e := range m.ideEntries {
			tag := ""
			switch {
			case e.IsNew:
				tag = " " + lipgloss.NewStyle().Foreground(primaryColor).Render("(new)")
			case e.IsBuiltin:
				tag = " " + lipgloss.NewStyle().Foreground(dimColor).Render("(builtin)")
			}

			// Name header (editable only for custom/new entries)
			if !e.IsBuiltin {
				isCursor := itemIdx == m.cursor
				indicator, labelStyle := fieldRowCursor(isCursor)
				nameVal := e.Name
				if nameVal == "" && !isCursor {
					nameVal = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(name, e.g. cursor)")
				}
				if isCursor {
					nameVal += lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
				}
				sections = append(sections, "  "+indicator+labelStyle.Render("Name:    ")+nameVal+tag)
				itemIdx++
			} else {
				// Built-in name is static — just a label row.
				sections = append(sections, lipgloss.NewStyle().Padding(0, 4).Render(
					lipgloss.NewStyle().Foreground(whiteColor).Render(e.Name)+tag,
				))
			}

			// Command
			isCursor := itemIdx == m.cursor
			indicator, labelStyle := fieldRowCursor(isCursor)
			cmdVal := e.CommandStr
			if cmdVal == "" && !isCursor {
				cmdVal = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(executable, e.g. code)")
			}
			if isCursor {
				cmdVal += lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
			}
			sections = append(sections, "  "+indicator+labelStyle.Render("Command: ")+cmdVal)
			itemIdx++

			// Args
			isCursor = itemIdx == m.cursor
			indicator, labelStyle = fieldRowCursor(isCursor)
			argsVal := e.ArgsStr
			if argsVal == "" && !isCursor {
				argsVal = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(`(e.g. -a "Visual Studio Code")`)
			}
			if isCursor {
				argsVal += lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
			}
			sections = append(sections, "  "+indicator+labelStyle.Render("Args:    ")+argsVal)
			itemIdx++

			sections = append(sections, "")
			_ = i
		}

		// "+ Add IDE" row
		isCursor := itemIdx == m.cursor
		indicator, _ := fieldRowCursor(isCursor)
		add := lipgloss.NewStyle().Foreground(primaryColor).Render("+ Add IDE")
		sections = append(sections, "  "+indicator+add)
		itemIdx++
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
		" ↑↓/Tab: navigate  ←→: select  Ctrl+R: inherit  Ctrl+X: delete IDE  Enter: save/add  Esc: cancel")
	return content + "\n" + statusBar
}

// generalTextPtr returns a pointer to the string backing a general-section
// text field (section == -1, kind == "text"), or nil when the item isn't one.
// Keeps all general text-field bookkeeping in one place instead of scattering
// `item.kind == "text" && item.section == -1 && item.fieldIdx == N` across
// paste / typing / backspace / ctrl+u / ctrl+r handlers.
func (m *SettingsModel) generalTextPtr(item flatItem) *string {
	if item.kind != "text" || item.section != -1 {
		return nil
	}
	switch item.fieldIdx {
	case generalFieldWorktreeBasePath:
		return &m.worktreeBasePathStr
	}
	return nil
}

// renderTextField draws a free-form text input row used for general-section
// path/string fields. Mirrors renderNumberField but without the numeric
// suffix or range hint.
func (m SettingsModel) renderTextField(itemIdx int, label, value, placeholder string) string {
	isCursor := itemIdx == m.cursor
	indicator, labelStyle := fieldRowCursor(isCursor)

	display := value
	if display == "" && !isCursor {
		if placeholder != "" {
			display = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(" + placeholder + ")")
		} else {
			display = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("-")
		}
	} else {
		display = lipgloss.NewStyle().Foreground(whiteColor).Render(display)
	}
	if isCursor {
		display += lipgloss.NewStyle().Foreground(primaryColor).Render("▎")
	}
	return "  " + indicator + labelStyle.Render(label+": ") + display
}

// renderActionField renders an "invokable" row: a label followed by a path or
// descriptive suffix, activated with Enter. When highlighted it reads like a
// button so users know it does something.
func (m SettingsModel) renderActionField(itemIdx int, label, suffix string) string {
	isCursor := itemIdx == m.cursor
	indicator, labelStyle := fieldRowCursor(isCursor)
	labelPart := labelStyle.Render(label)
	suffixPart := lipgloss.NewStyle().Foreground(dimColor).Render("  " + suffix)
	hint := ""
	if isCursor {
		hint = lipgloss.NewStyle().Foreground(primaryColor).Render("  [Enter]")
	}
	return "  " + indicator + labelPart + suffixPart + hint
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
