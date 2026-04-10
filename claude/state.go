package claude

import (
	"strings"
	"unicode"
)

// State represents the current state of a Claude session.
type State int

const (
	StateUnknown    State = iota
	StateIdle             // Waiting for user input
	StateBusy             // Generic busy (can't determine detail)
	StateThinking         // Claude is thinking/processing
	StateToolUse          // Running a tool
	StateResponding       // Streaming response text
)

// IsBusy returns true if Claude is doing any kind of work.
func (s State) IsBusy() bool {
	return s == StateBusy || s == StateThinking || s == StateToolUse || s == StateResponding
}

// Label returns a display label for the state.
func (s State) Label() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateThinking:
		return "thinking…"
	case StateToolUse:
		return "tool…"
	case StateResponding:
		return "responding…"
	case StateBusy:
		return "busy…"
	default:
		return ""
	}
}

// DetectStateFromTitle determines Claude's state from the tmux pane title.
// Claude Code sets the pane title with a leading icon:
//   - ✳ (U+2733) = idle
//   - Braille spinner chars (U+2800..U+28FF) = busy/working
func DetectStateFromTitle(title string) State {
	if title == "" {
		return StateUnknown
	}

	for _, r := range title {
		if unicode.IsSpace(r) {
			continue
		}
		if r == '✳' {
			return StateIdle
		}
		// Braille pattern block: U+2800..U+28FF (spinner characters)
		if r >= 0x2800 && r <= 0x28FF {
			return StateBusy
		}
		return StateUnknown
	}

	return StateUnknown
}

// DetectBusyDetail analyzes pane content to determine what kind of busy.
// Only called when pane title already confirmed busy state.
// Returns a more specific state or StateBusy as fallback.
func DetectBusyDetail(content string) State {
	if strings.TrimSpace(content) == "" {
		return StateBusy
	}

	lines := strings.Split(content, "\n")

	// Collect last non-empty lines from the bottom
	var bottom []string
	for i := len(lines) - 1; i >= 0 && len(bottom) < 15; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			bottom = append(bottom, trimmed)
		}
	}

	if len(bottom) == 0 {
		return StateBusy
	}

	// Check for thinking
	for i := 0; i < min(8, len(bottom)); i++ {
		lower := strings.ToLower(bottom[i])
		if strings.Contains(lower, "thinking") {
			return StateThinking
		}
	}

	// Check for tool use (⏺ markers, ⎿ continuation)
	for i := 0; i < min(10, len(bottom)); i++ {
		if strings.Contains(bottom[i], "⏺") || strings.Contains(bottom[i], "⎿") {
			return StateToolUse
		}
	}

	// We know it's busy (from pane title), so default is responding
	return StateResponding
}
