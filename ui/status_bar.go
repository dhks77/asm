package ui

import (
	"github.com/charmbracelet/lipgloss"
)

func RenderStatusBar(width int, focused bool) string {
	keys := " n: new  r: resume  w: worktree  d: remove  s: settings  Tab: session  q: quit"

	bg := lipgloss.Color("236")
	fg := lipgloss.Color("252")
	if !focused {
		fg = lipgloss.Color("240")
	}

	return statusBarStyle.
		Width(width).
		Background(bg).
		Foreground(fg).
		Render(keys)
}
