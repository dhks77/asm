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
