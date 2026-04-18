package provider

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/nhn/asm/platform"
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
func (p *ClaudeProvider) DisplayName() string { return "Claude" }
func (p *ClaudeProvider) Command() string     { return p.command }
func (p *ClaudeProvider) Args() []string      { return p.args }

// ResumeArgs returns ["--continue"] only when a prior conversation exists
// for cwd. Claude Code's --continue exits with an error when there is no
// history in the current project, so we probe ~/.claude/projects/<cwd>/
// first. Claude keys conversations by project path, so each worktree's cwd
// gets its own resume target without asm tracking session IDs.
func (p *ClaudeProvider) ResumeArgs(cwd string) []string {
	if !hasClaudeSession(cwd) {
		return nil
	}
	return []string{"--continue"}
}

// hasClaudeSession reports whether Claude has stored any conversation for the
// given cwd. Claude's on-disk layout is
// ~/.claude/projects/<cwd-with-slashes-replaced-by-dashes>/<session-id>.jsonl
// (one file per session). An empty/missing directory = no resumable history.
func hasClaudeSession(cwd string) bool {
	home, err := platform.Current().HomeDir()
	if err != nil {
		return false
	}
	dir := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
}

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
