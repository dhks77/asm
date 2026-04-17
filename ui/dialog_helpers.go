package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderDialogHintBar(width int, hint string) string {
	return statusBarStyle.
		Width(width).
		Background(dialogBgColor).
		Foreground(dialogFgColor).
		Render(hint)
}

func padToHeight(content string, target int) string {
	lines := lipgloss.Height(content)
	if lines >= target {
		return content
	}
	return content + strings.Repeat("\n", target-lines)
}

func renderDialogTitle(text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(color).
		Padding(1, 2).
		Render(text)
}

func fieldRowCursor(active bool) (indicator string, labelStyle lipgloss.Style) {
	if active {
		return lipgloss.NewStyle().Foreground(primaryColor).Render("▸ "),
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
	}
	return "  ", lipgloss.NewStyle().Foreground(dimColor)
}
