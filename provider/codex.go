package provider

import (
	"strings"
)

// CodexProvider implements the Provider interface for OpenAI Codex CLI.
type CodexProvider struct {
	command string
	args    []string
}

// NewCodexProvider creates a Codex provider with the given command and args.
func NewCodexProvider(command string, args []string) *CodexProvider {
	if command == "" {
		command = "codex"
	}
	return &CodexProvider{command: command, args: args}
}

func (p *CodexProvider) Name() string        { return "codex" }
func (p *CodexProvider) DisplayName() string  { return "Codex" }
func (p *CodexProvider) Command() string      { return p.command }
func (p *CodexProvider) Args() []string       { return p.args }

// NeedsContent always returns true for Codex since detection is content-based.
func (p *CodexProvider) NeedsContent(title string) bool {
	return true
}

// DetectState determines state from pane content.
// Codex CLI does not reliably set pane titles, so we rely on content analysis.
func (p *CodexProvider) DetectState(title, content string) State {
	if strings.TrimSpace(content) == "" {
		return StateUnknown
	}

	lines := strings.Split(content, "\n")

	var bottom []string
	for i := len(lines) - 1; i >= 0 && len(bottom) < 10; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			bottom = append(bottom, trimmed)
		}
	}

	if len(bottom) == 0 {
		return StateUnknown
	}

	// Check for idle prompt pattern (e.g., "> " at the end)
	last := bottom[0]
	if strings.HasSuffix(last, "> ") || last == ">" {
		return StateIdle
	}

	// Check for spinner characters (busy indicator)
	spinnerChars := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	for _, r := range last {
		if strings.ContainsRune(spinnerChars, r) {
			return StateBusy
		}
	}

	// Check for thinking indicator
	for i := 0; i < min(5, len(bottom)); i++ {
		lower := strings.ToLower(bottom[i])
		if strings.Contains(lower, "thinking") {
			return StateThinking
		}
	}

	return StateUnknown
}
