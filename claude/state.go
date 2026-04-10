package claude

import (
	"strings"
)

// State represents the current state of a Claude session.
type State int

const (
	StateUnknown    State = iota
	StateIdle             // Waiting for user input (> prompt visible)
	StateThinking         // Claude is thinking/processing
	StateToolUse          // Running a tool
	StateResponding       // Streaming response text
)

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
	default:
		return ""
	}
}

// DetectState analyzes captured tmux pane content to determine Claude's current state.
// It inspects the bottom of the visible pane to identify characteristic patterns.
func DetectState(content string) State {
	if strings.TrimSpace(content) == "" {
		return StateUnknown
	}

	lines := strings.Split(content, "\n")

	// Collect last non-empty lines (bottom of screen is most informative)
	var bottom []string
	for i := len(lines) - 1; i >= 0 && len(bottom) < 20; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			bottom = append(bottom, trimmed)
		}
	}

	if len(bottom) == 0 {
		return StateUnknown
	}

	// Priority: active states first, then idle.
	// ❯ prompt line stays visible on screen after user submits,
	// so idle must be checked LAST to avoid false positives.

	// 1. Thinking (highest priority when Claude is working)
	if detectThinking(bottom) {
		return StateThinking
	}

	// 2. Tool use
	if detectToolUse(bottom) {
		return StateToolUse
	}

	// 3. Idle: prompt visible at the very bottom
	if detectIdle(bottom) {
		return StateIdle
	}

	// 4. Default: unknown
	return StateUnknown
}

// detectIdle checks for Claude Code's input prompt at the bottom of the screen.
// Claude Code renders a prompt like:
//
//	─────────────────────────
//	❯
//	─────────────────────────
//	  ⏵⏵ bypass permissions on (shift+tab to cycle)
func detectIdle(bottom []string) bool {
	// Only check the last 3 non-empty lines — the prompt must be
	// at the very bottom, not scrolled up from a previous input.
	for i := 0; i < min(3, len(bottom)); i++ {
		// Claude Code prompt character
		if strings.HasPrefix(bottom[i], "❯") {
			return true
		}
		// Legacy box-style prompt
		if strings.HasPrefix(bottom[i], "╰") && strings.Contains(bottom[i], "─") {
			return true
		}
	}
	return false
}

// detectThinking checks for Claude's thinking indicator.
func detectThinking(bottom []string) bool {
	for i := 0; i < min(8, len(bottom)); i++ {
		lower := strings.ToLower(bottom[i])
		if strings.Contains(lower, "thinking") {
			return true
		}
	}
	return false
}

// detectToolUse checks for tool execution markers.
// Claude Code shows tool calls with ⏺ markers and ⎿ continuation lines.
func detectToolUse(bottom []string) bool {
	for i := 0; i < min(10, len(bottom)); i++ {
		if strings.Contains(bottom[i], "⏺") {
			return true
		}
		if strings.Contains(bottom[i], "⎿") {
			return true
		}
	}
	return false
}
