// Package ide launches an IDE against a worktree path. Built-in launchers
// cover the common cases (IntelliJ, VSCode, Cursor); users can override
// the command/args per name or register new entries entirely via config.
package ide

import (
	"os/exec"
	"runtime"
)

// IDE is a single launcher entry.
type IDE struct {
	Name    string
	Command string
	Args    []string
}

// Override is a config-driven modification for a built-in, or a
// user-defined entry if no built-in matches the name.
type Override struct {
	Command string
	Args    []string
}

// builtinDefaults is the minimal shipped list. We only ship the two IDEs
// that cover the common case; everything else is expected to come from
// config. On macOS we use `open -a "<App>"` so the launchers work
// without the user having to install each IDE's shell CLI — on
// Linux/Windows we fall back to the CLI names (idea, code) since
// there's no equivalent universal launcher.
var builtinDefaults = defaultBuiltins()

func defaultBuiltins() []IDE {
	if runtime.GOOS == "darwin" {
		return []IDE{
			{Name: "intellij", Command: "open", Args: []string{"-a", "IntelliJ IDEA"}},
			{Name: "vscode", Command: "open", Args: []string{"-a", "Visual Studio Code"}},
		}
	}
	return []IDE{
		{Name: "intellij", Command: "idea"},
		{Name: "vscode", Command: "code"},
	}
}

// Builtins returns the list of IDE launchers with optional per-name
// config overrides applied. Overrides may also introduce entries whose
// name doesn't match any built-in — that's how a user registers a
// custom launcher (e.g. `open -a "Some Editor"`).
func Builtins(overrides map[string]Override) []IDE {
	result := make([]IDE, 0, len(builtinDefaults)+len(overrides))
	seen := make(map[string]bool, len(builtinDefaults))

	for _, d := range builtinDefaults {
		entry := d
		if o, ok := overrides[d.Name]; ok {
			if o.Command != "" {
				entry.Command = o.Command
			}
			if o.Args != nil {
				entry.Args = o.Args
			}
		}
		result = append(result, entry)
		seen[d.Name] = true
	}

	// Append user-defined entries in any order — alpha-sorted would be
	// friendlier but map iteration is random; keep insertion simple.
	for name, o := range overrides {
		if seen[name] || o.Command == "" {
			continue
		}
		result = append(result, IDE{Name: name, Command: o.Command, Args: o.Args})
	}
	return result
}

// Names returns the names in display order.
func Names(ides []IDE) []string {
	out := make([]string, len(ides))
	for i, d := range ides {
		out[i] = d.Name
	}
	return out
}

// Find returns the IDE with the given name, or nil if missing.
func Find(ides []IDE, name string) *IDE {
	for i := range ides {
		if ides[i].Name == name {
			return &ides[i]
		}
	}
	return nil
}

// Open launches the IDE against path, detached from the caller. The
// process keeps running after asm exits; the IDE's own windowing
// handles focus.
func (i IDE) Open(path string) error {
	args := append([]string(nil), i.Args...)
	args = append(args, path)
	cmd := exec.Command(i.Command, args...)
	return cmd.Start()
}
