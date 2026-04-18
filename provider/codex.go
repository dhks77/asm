package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhn/asm/platform"
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
func (p *CodexProvider) DisplayName() string { return "Codex" }
func (p *CodexProvider) Command() string     { return p.command }
func (p *CodexProvider) Args() []string      { return p.args }

// ResumeArgs returns ["resume", "--last"] only when Codex has stored a prior
// interactive session for cwd. `codex resume --last` exits when no resumable
// session exists in the current project, so asm probes ~/.codex/sessions first.
func (p *CodexProvider) ResumeArgs(cwd string) []string {
	if !hasCodexSession(cwd) {
		return nil
	}
	return []string{"resume", "--last"}
}

type codexSessionMeta struct {
	Type    string `json:"type"`
	Payload struct {
		Cwd string `json:"cwd"`
	} `json:"payload"`
}

var errCodexSessionFound = errors.New("codex session found")

func hasCodexSession(cwd string) bool {
	home, err := platform.Current().HomeDir()
	if err != nil {
		return false
	}
	return hasCodexSessionInDir(filepath.Join(home, ".codex", "sessions"), cwd)
}

func hasCodexSessionInDir(sessionsDir, cwd string) bool {
	target := filepath.Clean(cwd)
	err := filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) != ".jsonl" {
			return nil
		}
		if codexSessionMatchesCwd(path, target) {
			return errCodexSessionFound
		}
		return nil
	})
	return errors.Is(err, errCodexSessionFound)
}

func codexSessionMatchesCwd(path, cwd string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return false
	}

	var meta codexSessionMeta
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
		return false
	}
	if meta.Type != "session_meta" || meta.Payload.Cwd == "" {
		return false
	}
	return filepath.Clean(meta.Payload.Cwd) == cwd
}

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

	bottom := codexBottomLines(content, 12)
	if len(bottom) == 0 {
		return StateUnknown
	}

	if state, ok := detectCodexBusyDetail(bottom); ok {
		return state
	}

	for i := 0; i < min(4, len(bottom)); i++ {
		if isCodexPromptLine(bottom[i]) {
			return StateIdle
		}
	}

	for i := 0; i < min(4, len(bottom)); i++ {
		if !isCodexChromeLine(bottom[i]) {
			return StateResponding
		}
	}

	return StateBusy
}

func codexBottomLines(content string, limit int) []string {
	lines := strings.Split(content, "\n")
	var bottom []string
	for i := len(lines) - 1; i >= 0 && len(bottom) < limit; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			bottom = append(bottom, trimmed)
		}
	}
	return bottom
}

func isCodexPromptLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"❯", ">", "›"} {
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		rest = strings.Trim(rest, "▌█▋▍▎▏_")
		if strings.HasPrefix(strings.ToLower(rest), "working ") {
			return false
		}
		return true
	}
	return false
}

func detectCodexBusyDetail(bottom []string) (State, bool) {
	for i := 0; i < min(6, len(bottom)); i++ {
		line := bottom[i]
		lower := strings.ToLower(line)
		if containsCodexSpinner(line) ||
			strings.Contains(lower, "thinking") ||
			strings.Contains(lower, "analyz") ||
			strings.Contains(lower, "planning") ||
			strings.Contains(lower, "reasoning") {
			return StateThinking, true
		}
		if strings.Contains(lower, "background command") ||
			strings.Contains(lower, "exec_command") ||
			strings.Contains(lower, "apply_patch") ||
			strings.Contains(lower, "write_stdin") ||
			strings.Contains(lower, "spawn_agent") ||
			strings.Contains(lower, "wait_agent") ||
			strings.Contains(lower, "task-notification") ||
			strings.Contains(lower, "running ") ||
			strings.Contains(lower, "completed (exit code") {
			return StateToolUse, true
		}
	}
	for i := 0; i < min(6, len(bottom)); i++ {
		lower := strings.ToLower(bottom[i])
		if strings.Contains(lower, "working (") ||
			strings.Contains(lower, "esc to interrupt") {
			return StateBusy, true
		}
	}
	return StateUnknown, false
}

func isCodexChromeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "esc to interrupt") ||
		strings.Contains(lower, "for shortcuts") ||
		strings.Contains(lower, "accept edits") ||
		strings.Contains(lower, "plan mode") ||
		strings.Contains(line, "· ~/") ||
		strings.Contains(line, "· /") {
		return true
	}
	if isCodexPromptLine(trimmed) {
		return true
	}
	for _, r := range trimmed {
		if strings.ContainsRune("─━—-=~╌┄╭╮╰╯│┃|┌┐└┘├┤┬┴┼ ", r) {
			continue
		}
		return false
	}
	return true
}

func containsCodexSpinner(line string) bool {
	const spinnerChars = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	for _, r := range line {
		if strings.ContainsRune(spinnerChars, r) {
			return true
		}
	}
	return false
}
