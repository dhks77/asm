package ui

import (
	"github.com/charmbracelet/lipgloss"
)

func RenderStatusBar(width int) string {
	keys := " j/k: navigate  Enter: open  n: new  r: resume  d: delete  Tab: session  q: quit │ Tab+Tab: back"

	return statusBarStyle.
		Width(width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(keys)
}
