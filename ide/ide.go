// Package ide launches an IDE against a worktree path. Built-in launchers
// cover the common cases (IntelliJ, VSCode, Cursor); users can override
// the command/args per name or register new entries entirely via config.
package ide

import (
	"os/exec"

	"github.com/nhn/asm/platform"
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

// Builtins returns the list of IDE launchers with optional per-name
// config overrides applied. Overrides may also introduce entries whose
// name doesn't match any built-in — that's how a user registers a
// custom launcher (e.g. `open -a "Some Editor"`).
func Builtins(overrides map[string]Override) []IDE {
	defaults := platform.Current().BuiltinIDEs()
	result := make([]IDE, 0, len(defaults)+len(overrides))
	seen := make(map[string]bool, len(defaults))

	for _, d := range defaults {
		entry := IDE{Name: d.Name, Command: d.Command, Args: append([]string(nil), d.Args...)}
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
	command, args := i.launchCommand(path)
	cmd := exec.Command(command, args...)
	return cmd.Start()
}

func (i IDE) launchCommand(path string) (string, []string) {
	return platform.Current().PrepareIDEOpen(i.Name, i.Command, i.Args, path)
}
