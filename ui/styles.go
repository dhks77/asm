package ui

import "github.com/charmbracelet/lipgloss"

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
)
