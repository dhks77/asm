package provider

// DefaultProviderName is the default provider when none is configured.
const DefaultProviderName = "claude"

// BuiltinOverride holds optional command/args overrides for a built-in provider.
type BuiltinOverride struct {
	Command string
	Args    []string
}

// Builtins returns built-in providers with optional config overrides applied.
func Builtins(overrides map[string]BuiltinOverride) []Provider {
	claudeCfg := overrides["claude"]
	codexCfg := overrides["codex"]
	return []Provider{
		NewClaudeProvider(claudeCfg.Command, claudeCfg.Args),
		NewCodexProvider(codexCfg.Command, codexCfg.Args),
	}
}
