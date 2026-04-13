package ui

import (
	"strings"

	"github.com/nhn/asm/config"
	"github.com/nhn/asm/ide"
)

// ideEditEntry is the per-IDE row used by the settings dialog. It holds
// the mutable UI strings (CommandStr/ArgsStr) plus bookkeeping to tell
// built-ins from user-added entries and brand-new unsaved rows.
type ideEditEntry struct {
	Name       string
	CommandStr string
	ArgsStr    string // shell-style: `-a "Visual Studio Code"`
	IsBuiltin  bool   // true → Name can't be edited; delete resets override
	IsNew      bool   // true → row hasn't been persisted yet; Name is editable
}

// loadIDEEntries builds the editable IDE list from the current scope's
// config, overlaying any overrides onto the built-in defaults. Built-ins
// always appear; user-defined entries follow in config order.
func loadIDEEntries(cfg *config.Config) []ideEditEntry {
	overrides := cfg.IDEs
	if overrides == nil {
		overrides = map[string]config.IDEConfig{}
	}

	var out []ideEditEntry
	builtinIDEs := ide.Builtins(nil)
	seen := map[string]bool{}

	// Built-ins first, with any override applied.
	for _, b := range builtinIDEs {
		cmd := b.Command
		args := b.Args
		if o, ok := overrides[b.Name]; ok {
			if o.Command != "" {
				cmd = o.Command
			}
			if o.Args != nil {
				args = o.Args
			}
		}
		out = append(out, ideEditEntry{
			Name:       b.Name,
			CommandStr: cmd,
			ArgsStr:    formatArgs(args),
			IsBuiltin:  true,
		})
		seen[b.Name] = true
	}

	// User-defined entries.
	for name, cfg := range overrides {
		if seen[name] {
			continue
		}
		out = append(out, ideEditEntry{
			Name:       name,
			CommandStr: cfg.Command,
			ArgsStr:    formatArgs(cfg.Args),
		})
	}
	return out
}

// saveIDEEntries writes the editable entries back into the given config.
// Built-ins are only persisted when their values diverge from the default
// (so config files stay minimal). New entries with empty name or command
// are dropped.
func saveIDEEntries(cfg *config.Config, entries []ideEditEntry) {
	defaults := map[string]ide.IDE{}
	for _, b := range ide.Builtins(nil) {
		defaults[b.Name] = b
	}

	out := map[string]config.IDEConfig{}
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		cmd := strings.TrimSpace(e.CommandStr)
		if name == "" || cmd == "" {
			continue
		}
		args := parseArgs(e.ArgsStr)

		if d, ok := defaults[name]; ok {
			// Only persist a builtin override when it actually differs.
			if d.Command == cmd && argsEqual(d.Args, args) {
				continue
			}
		}
		out[name] = config.IDEConfig{Command: cmd, Args: args}
	}
	if len(out) == 0 {
		cfg.IDEs = nil
	} else {
		cfg.IDEs = out
	}
}

// parseArgs splits a shell-style argument string respecting double
// quotes so tokens like `"Visual Studio Code"` stay intact. Single
// quotes and escapes aren't supported — double quotes cover the common
// case (app names with spaces on macOS) without pulling in a full
// shell-lexer.
func parseArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// formatArgs is the inverse of parseArgs — quotes any arg that contains
// whitespace so round-tripping through the UI preserves semantics.
func formatArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			parts = append(parts, `"`+a+`"`)
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// ideFieldPtr returns a pointer to the editable string backing an IDE
// field in the flatItem list. Used by the settings key handlers so
// text-input, backspace, and paste can share a single code path across
// the three fields (name/command/args).
func (m *SettingsModel) ideFieldPtr(section int, kind string) *string {
	if section < 0 || section >= len(m.ideEntries) {
		return nil
	}
	e := &m.ideEntries[section]
	switch kind {
	case "ide-name":
		return &e.Name
	case "ide-cmd":
		return &e.CommandStr
	case "ide-args":
		return &e.ArgsStr
	}
	return nil
}

// isIDEField reports whether the given kind is one of the editable IDE
// input rows.
func isIDEField(kind string) bool {
	return kind == "ide-name" || kind == "ide-cmd" || kind == "ide-args"
}

func argsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
