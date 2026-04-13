package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Shared rendering helpers for the standalone dialog models that run in the
// working pane (settings, delete, provider-select, worktree) plus the inline
// batch-confirm dialog. These centralize three patterns that used to be
// copy-pasted across every dialog's View():
//
//  1. A dark-grey footer bar with a key hint.
//  2. Padding the content with blank lines so the footer sits at the bottom.
//  3. The "▸ bold/white when under cursor, two-space dim label otherwise"
//     field row prefix used by settings.
//
// A `renderDialogTitle` helper gives a single source of truth for the bold,
// padded-1x2 title style shared by provider-select and delete.

// renderDialogHintBar renders the dark-grey footer hint line used by every
// working-pane dialog. `hint` should already include the leading space that
// aligns with the 1,2 padding used by most dialog bodies.
func renderDialogHintBar(width int, hint string) string {
	return statusBarStyle.
		Width(width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(hint)
}

// padToHeight appends blank lines so `content` occupies at least `target`
// rows. Used to push the footer hint bar to the bottom of the terminal when
// the dialog body is shorter than the available height.
func padToHeight(content string, target int) string {
	lines := lipgloss.Height(content)
	if lines >= target {
		return content
	}
	return content + strings.Repeat("\n", target-lines)
}

// renderDialogTitle renders a dialog title with the standard bold + 1,2
// padding treatment, colored via `color` (e.g. primaryColor, dangerColor).
func renderDialogTitle(text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(color).
		Padding(1, 2).
		Render(text)
}

// fieldRowCursor returns the indicator prefix and label style for a dialog
// field row. When `active`, the row shows a primary-colored "▸ " arrow and a
// bold white label; otherwise a two-space gap and a dim label. Settings
// reuses this shape for every field (select, number, plugin/tracker).
func fieldRowCursor(active bool) (indicator string, labelStyle lipgloss.Style) {
	if active {
		return lipgloss.NewStyle().Foreground(primaryColor).Render("▸ "),
			lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
	}
	return "  ", lipgloss.NewStyle().Foreground(dimColor)
}
