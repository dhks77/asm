package provider

import (
	"strings"
	"unicode"
)

// ClaudeProvider implements the Provider interface for Claude Code.
type ClaudeProvider struct {
	command string
	args    []string
}

// NewClaudeProvider creates a Claude provider with the given command and args.
func NewClaudeProvider(command string, args []string) *ClaudeProvider {
	if command == "" {
		command = "claude"
	}
	return &ClaudeProvider{command: command, args: args}
}

func (p *ClaudeProvider) Name() string        { return "claude" }
func (p *ClaudeProvider) DisplayName() string  { return "Claude" }
func (p *ClaudeProvider) Command() string      { return p.command }
func (p *ClaudeProvider) Args() []string       { return p.args }

// ResumeArgs returns ["--continue"], which tells Claude Code to resume the
// most recent conversation in the current working directory. Claude keys
// conversations by project (CWD) so each worktree, launched with its own
// cwd, gets its own resume target without asm tracking session IDs.
// Safe when no prior conversation exists — Claude falls back to a new one.
func (p *ClaudeProvider) ResumeArgs() []string { return []string{"--continue"} }

// NeedsContent returns true when the pane title indicates busy state,
// meaning content capture is needed to determine the specific busy detail.
func (p *ClaudeProvider) NeedsContent(title string) bool {
	for _, r := range title {
		if unicode.IsSpace(r) {
			continue
		}
		// Braille pattern block: U+2800..U+28FF (spinner characters = busy)
		return r >= 0x2800 && r <= 0x28FF
	}
	return false
}

// DetectState determines the session state from tmux pane title and content.
// Claude Code sets the pane title with a leading icon:
//   - ✳ (U+2733) = idle
//   - Braille spinner chars (U+2800..U+28FF) = busy/working
func (p *ClaudeProvider) DetectState(title, content string) State {
	state := detectClaudeStateFromTitle(title)
	if state == StateBusy && content != "" {
		if detail := detectClaudeBusyDetail(content); detail != StateBusy {
			return detail
		}
	}
	return state
}

func detectClaudeStateFromTitle(title string) State {
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
		if r >= 0x2800 && r <= 0x28FF {
			return StateBusy
		}
		return StateUnknown
	}
	return StateUnknown
}

func detectClaudeBusyDetail(content string) State {
	if strings.TrimSpace(content) == "" {
		return StateBusy
	}

	lines := strings.Split(content, "\n")

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

	for i := 0; i < min(8, len(bottom)); i++ {
		lower := strings.ToLower(bottom[i])
		if strings.Contains(lower, "thinking") {
			return StateThinking
		}
	}

	for i := 0; i < min(10, len(bottom)); i++ {
		if strings.Contains(bottom[i], "⏺") || strings.Contains(bottom[i], "⎿") {
			return StateToolUse
		}
	}

	return StateResponding
}
