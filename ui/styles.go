package ui

import (
	"github.com/charmbracelet/lipgloss"
	asmtmux "github.com/nhn/asm/tmux"
)

var (
	// Colors
	primaryColor   = lipgloss.Color("141") // light purple
	secondaryColor = lipgloss.Color("249") // gray
	activeColor    = lipgloss.Color("42")  // green
	dimColor       = lipgloss.Color("245")
	whiteColor     = lipgloss.Color("255")

	// Panel borders
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor)

	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(secondaryColor)

	// List item styles
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(whiteColor).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	// Session indicator
	activeSessionStyle = lipgloss.NewStyle().
				Foreground(activeColor).
				SetString("●")

	inactiveSessionStyle = lipgloss.NewStyle().
				Foreground(dimColor).
				SetString("○")

	// Git status
	gitStatusStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true)

	// Task name
	taskNameStyle = lipgloss.NewStyle().
			Foreground(primaryColor)

	// Header
	headerStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			Padding(0, 1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Padding(0, 1)

	// Placeholder text in session view
	placeholderStyle = lipgloss.NewStyle().
				Foreground(dimColor).
				Italic(true).
				Align(lipgloss.Center)

	// Provider state styles
	ClosedStateStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	IdleStateStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	BusyStateStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	ThinkingStateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	ToolUseStateStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	RespondingStateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))

	// Elapsed time badge
	ElapsedTimeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Completion flash (busy→idle transition)
	CompletionFlashStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)

	// Session kind badge ([a] / [t] / [a+t])
	kindAIColor    = lipgloss.Color("141") // purple — AI
	kindTermColor  = lipgloss.Color("215") // orange — terminal
	kindAIStyle    = lipgloss.NewStyle().Foreground(kindAIColor)
	kindTermStyle  = lipgloss.NewStyle().Foreground(kindTermColor)
	kindBraceStyle = lipgloss.NewStyle().Foreground(dimColor)
)

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
		inner = "#[fg=colour141]a#[fg=colour245]+#[fg=colour215]t"
	case kind.HasAI():
		inner = "#[fg=colour141]a"
	case kind.HasTerm():
		inner = "#[fg=colour215]t"
	}
	return "#[fg=colour245][" + inner + "#[fg=colour245]]#[default]"
}
