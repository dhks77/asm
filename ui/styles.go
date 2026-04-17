package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	asmtmux "github.com/nhn/asm/tmux"
)

type themePalette struct {
	primary       lipgloss.Color
	secondary     lipgloss.Color
	active        lipgloss.Color
	dim           lipgloss.Color
	strongText    lipgloss.Color
	surfaceText   lipgloss.Color
	danger        lipgloss.Color
	warn          lipgloss.Color
	dialogBg      lipgloss.Color
	dialogFg      lipgloss.Color
	closedState   lipgloss.Color
	idleState     lipgloss.Color
	busyState     lipgloss.Color
	toolUseState  lipgloss.Color
	responding    lipgloss.Color
	elapsed       lipgloss.Color
	completion    lipgloss.Color
	kindAI        lipgloss.Color
	kindTerm      lipgloss.Color
	tmuxPrimary   string
	tmuxActive    string
	tmuxDim       string
	tmuxText      string
	tmuxWarn      string
	tmuxToolUse   string
	tmuxRespond   string
	tmuxKindAI    string
	tmuxKindTerm  string
	tmuxStatusBg  string
	tmuxStatusFg  string
	tmuxStatusDim string
}

var themeOptions = []string{"dark", "light"}

var (
	// Colors
	primaryColor     lipgloss.Color
	secondaryColor   lipgloss.Color
	activeColor      lipgloss.Color
	dimColor         lipgloss.Color
	whiteColor       lipgloss.Color
	surfaceTextColor lipgloss.Color
	dangerColor      lipgloss.Color
	warnColor        lipgloss.Color
	dialogBgColor    lipgloss.Color
	dialogFgColor    lipgloss.Color

	tmuxPrimaryColor    string
	tmuxActiveColor     string
	tmuxDimColor        string
	tmuxTextColor       string
	tmuxWarnColor       string
	tmuxToolUseColor    string
	tmuxRespondingColor string
	tmuxKindAIColor     string
	tmuxKindTermColor   string

	// Panel borders
	activeBorderStyle   lipgloss.Style
	inactiveBorderStyle lipgloss.Style

	// List item styles
	selectedItemStyle lipgloss.Style
	normalItemStyle   lipgloss.Style

	// Session indicator
	activeSessionStyle   lipgloss.Style
	inactiveSessionStyle lipgloss.Style

	// Git status
	gitStatusStyle lipgloss.Style

	// Task name
	taskNameStyle lipgloss.Style

	// Header
	headerStyle lipgloss.Style

	// Status bar
	statusBarStyle lipgloss.Style

	// Placeholder text in session view
	placeholderStyle lipgloss.Style

	// Provider state styles
	ClosedStateStyle     lipgloss.Style
	IdleStateStyle       lipgloss.Style
	BusyStateStyle       lipgloss.Style
	ThinkingStateStyle   lipgloss.Style
	ToolUseStateStyle    lipgloss.Style
	RespondingStateStyle lipgloss.Style

	// Elapsed time badge
	ElapsedTimeStyle lipgloss.Style

	// Completion flash (busy→idle transition)
	CompletionFlashStyle lipgloss.Style

	// Session kind badge ([a] / [t] / [a+t])
	kindAIColor    lipgloss.Color
	kindTermColor  lipgloss.Color
	kindAIStyle    lipgloss.Style
	kindTermStyle  lipgloss.Style
	kindBraceStyle lipgloss.Style
)

func init() {
	ApplyTheme("dark")
}

func normalizeThemeName(theme string) string {
	if strings.EqualFold(strings.TrimSpace(theme), "light") {
		return "light"
	}
	return "dark"
}

// ApplyTheme refreshes the package-level UI palette.
func ApplyTheme(theme string) {
	p := paletteForTheme(normalizeThemeName(theme))

	primaryColor = p.primary
	secondaryColor = p.secondary
	activeColor = p.active
	dimColor = p.dim
	whiteColor = p.strongText
	surfaceTextColor = p.surfaceText
	dangerColor = p.danger
	warnColor = p.warn
	dialogBgColor = p.dialogBg
	dialogFgColor = p.dialogFg

	tmuxPrimaryColor = p.tmuxPrimary
	tmuxActiveColor = p.tmuxActive
	tmuxDimColor = p.tmuxDim
	tmuxTextColor = p.tmuxText
	tmuxWarnColor = p.tmuxWarn
	tmuxToolUseColor = p.tmuxToolUse
	tmuxRespondingColor = p.tmuxRespond
	tmuxKindAIColor = p.tmuxKindAI
	tmuxKindTermColor = p.tmuxKindTerm

	activeBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor)
	inactiveBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor)
	selectedItemStyle = lipgloss.NewStyle().
		Foreground(whiteColor).
		Bold(true)
	normalItemStyle = lipgloss.NewStyle().
		Foreground(secondaryColor)
	activeSessionStyle = lipgloss.NewStyle().
		Foreground(activeColor).
		SetString("●")
	inactiveSessionStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		SetString("○")
	gitStatusStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		Italic(true)
	taskNameStyle = lipgloss.NewStyle().
		Foreground(primaryColor)
	headerStyle = lipgloss.NewStyle().
		Foreground(primaryColor).
		Bold(true).
		Padding(0, 1)
	statusBarStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		Padding(0, 1)
	placeholderStyle = lipgloss.NewStyle().
		Foreground(dimColor).
		Italic(true).
		Align(lipgloss.Center)
	ClosedStateStyle = lipgloss.NewStyle().Foreground(p.closedState)
	IdleStateStyle = lipgloss.NewStyle().Foreground(p.idleState)
	BusyStateStyle = lipgloss.NewStyle().Foreground(p.busyState).Bold(true)
	ThinkingStateStyle = lipgloss.NewStyle().Foreground(p.busyState).Bold(true)
	ToolUseStateStyle = lipgloss.NewStyle().Foreground(p.toolUseState).Bold(true)
	RespondingStateStyle = lipgloss.NewStyle().Foreground(p.responding)
	ElapsedTimeStyle = lipgloss.NewStyle().Foreground(p.elapsed)
	CompletionFlashStyle = lipgloss.NewStyle().Foreground(p.completion).Bold(true)
	kindAIColor = p.kindAI
	kindTermColor = p.kindTerm
	kindAIStyle = lipgloss.NewStyle().Foreground(kindAIColor)
	kindTermStyle = lipgloss.NewStyle().Foreground(kindTermColor)
	kindBraceStyle = lipgloss.NewStyle().Foreground(dimColor)

	asmtmux.ConfigureStatusTheme(p.tmuxStatusBg, p.tmuxStatusFg, p.tmuxStatusDim)
}

func paletteForTheme(theme string) themePalette {
	if theme == "light" {
		return themePalette{
			primary:       lipgloss.Color("25"),
			secondary:     lipgloss.Color("239"),
			active:        lipgloss.Color("29"),
			dim:           lipgloss.Color("243"),
			strongText:    lipgloss.Color("235"),
			surfaceText:   lipgloss.Color("255"),
			danger:        lipgloss.Color("160"),
			warn:          lipgloss.Color("172"),
			dialogBg:      lipgloss.Color("254"),
			dialogFg:      lipgloss.Color("239"),
			closedState:   lipgloss.Color("246"),
			idleState:     lipgloss.Color("242"),
			busyState:     lipgloss.Color("172"),
			toolUseState:  lipgloss.Color("31"),
			responding:    lipgloss.Color("29"),
			elapsed:       lipgloss.Color("244"),
			completion:    lipgloss.Color("29"),
			kindAI:        lipgloss.Color("25"),
			kindTerm:      lipgloss.Color("166"),
			tmuxPrimary:   "colour25",
			tmuxActive:    "colour29",
			tmuxDim:       "colour243",
			tmuxText:      "colour235",
			tmuxWarn:      "colour172",
			tmuxToolUse:   "colour31",
			tmuxRespond:   "colour29",
			tmuxKindAI:    "colour25",
			tmuxKindTerm:  "colour166",
			tmuxStatusBg:  "colour254",
			tmuxStatusFg:  "colour239",
			tmuxStatusDim: "colour243",
		}
	}
	return themePalette{
		primary:       lipgloss.Color("111"),
		secondary:     lipgloss.Color("250"),
		active:        lipgloss.Color("42"),
		dim:           lipgloss.Color("244"),
		strongText:    lipgloss.Color("255"),
		surfaceText:   lipgloss.Color("16"),
		danger:        lipgloss.Color("203"),
		warn:          lipgloss.Color("221"),
		dialogBg:      lipgloss.Color("236"),
		dialogFg:      lipgloss.Color("252"),
		closedState:   lipgloss.Color("242"),
		idleState:     lipgloss.Color("245"),
		busyState:     lipgloss.Color("221"),
		toolUseState:  lipgloss.Color("81"),
		responding:    lipgloss.Color("114"),
		elapsed:       lipgloss.Color("243"),
		completion:    lipgloss.Color("42"),
		kindAI:        lipgloss.Color("111"),
		kindTerm:      lipgloss.Color("215"),
		tmuxPrimary:   "colour111",
		tmuxActive:    "colour42",
		tmuxDim:       "colour244",
		tmuxText:      "colour252",
		tmuxWarn:      "colour221",
		tmuxToolUse:   "colour81",
		tmuxRespond:   "colour114",
		tmuxKindAI:    "colour111",
		tmuxKindTerm:  "colour215",
		tmuxStatusBg:  "colour236",
		tmuxStatusFg:  "colour252",
		tmuxStatusDim: "colour244",
	}
}

// KindBadgeMaxWidth is the visible-column width of the longest possible badge
// ("[a+t]"). Used by callers that reserve a fixed column for the badge.
const KindBadgeMaxWidth = 5

// renderKindBadge renders a lipgloss-styled session kind badge.
// Returns "" when kind is 0.
func renderKindBadge(kind asmtmux.SessionKind) string {
	if kind == 0 {
		return ""
	}
	inner := ""
	switch {
	case kind.HasAI() && kind.HasTerm():
		inner = kindAIStyle.Render("a") + kindBraceStyle.Render("+") + kindTermStyle.Render("t")
	case kind.HasAI():
		inner = kindAIStyle.Render("a")
	case kind.HasTerm():
		inner = kindTermStyle.Render("t")
	}
	return kindBraceStyle.Render("[") + inner + kindBraceStyle.Render("]")
}

// renderKindBadgeTmux renders the same badge as a tmux format string (uses
// #[fg=…] codes rather than ANSI), for the status bar.
func renderKindBadgeTmux(kind asmtmux.SessionKind) string {
	if kind == 0 {
		return ""
	}
	var inner string
	switch {
	case kind.HasAI() && kind.HasTerm():
		inner = "#[fg=" + tmuxKindAIColor + "]a#[fg=" + tmuxDimColor + "]+#[fg=" + tmuxKindTermColor + "]t"
	case kind.HasAI():
		inner = "#[fg=" + tmuxKindAIColor + "]a"
	case kind.HasTerm():
		inner = "#[fg=" + tmuxKindTermColor + "]t"
	}
	return "#[fg=" + tmuxDimColor + "][" + inner + "#[fg=" + tmuxDimColor + "]]#[default]"
}
