package provider

// Provider defines the interface for an AI CLI tool.
type Provider interface {
	Name() string                           // e.g. "claude", "codex", "aider"
	DisplayName() string                    // e.g. "Claude", "Codex", "Aider"
	Command() string                        // executable binary
	Args() []string                         // CLI arguments for a fresh session
	ResumeArgs() []string                   // extra args prepended to Args() to resume the prior session in CWD; nil if not supported
	DetectState(title, content string) State // detect state from pane title and content
	NeedsContent(title string) bool         // whether content capture is needed (optimization)
}
