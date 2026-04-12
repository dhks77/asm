package provider

// Provider defines the interface for an AI CLI tool.
type Provider interface {
	Name() string                           // e.g. "claude", "codex", "aider"
	DisplayName() string                    // e.g. "Claude", "Codex", "Aider"
	Command() string                        // executable binary
	Args() []string                         // CLI arguments
	DetectState(title, content string) State // detect state from pane title and content
	NeedsContent(title string) bool         // whether content capture is needed (optimization)
}
