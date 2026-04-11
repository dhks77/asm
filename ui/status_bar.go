package ui

import (
	"github.com/charmbracelet/lipgloss"
)

func RenderStatusBar(width int, focused bool) string {
	keys := " ^g: focus  ^t: term  ^n: new  ^s: settings  ^w: worktree  ^d: remove  ^q: quit"

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
