package provider

// Provider defines the interface for an AI CLI tool.
type Provider interface {
	Name() string                           // e.g. "claude", "codex", "aider"
	DisplayName() string                    // e.g. "Claude", "Codex", "Aider"
	Command() string                        // executable binary
	Args() []string                         // CLI arguments for a fresh session
	// ResumeArgs returns extra args prepended to Args() to resume the prior
	// session in cwd. Returns nil when the provider can't resume OR when no
	// prior session exists for cwd — passing a resume flag with no prior
	// conversation causes some providers (e.g. Claude Code) to exit on
	// startup, so the check must be per-cwd.
	ResumeArgs(cwd string) []string
	DetectState(title, content string) State // detect state from pane title and content
	NeedsContent(title string) bool          // whether content capture is needed (optimization)
}
