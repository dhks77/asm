package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func RenderStatusBar(width int, focused bool, selectedCount int) string {
	var keys string
	if selectedCount > 0 {
		keys = fmt.Sprintf(" %d selected  k: kill  x: delete  ^x: toggle  Esc: clear", selectedCount)
	} else {
		keys = " ^g: focus  ^t: term  ^n: new  ^p: AI  ^s: settings  ^w: worktree  ^d: remove  ^q: quit"
	}

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
