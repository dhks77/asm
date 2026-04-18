package ide

import (
	"reflect"
	"testing"

	"github.com/nhn/asm/platform"
)

type stubPlatform struct {
	ides []platform.IDEEntry
}

func (s stubPlatform) Name() string { return "stub" }
func (s stubPlatform) HomeDir() (string, error) {
	return "/tmp/home", nil
}
func (s stubPlatform) WorkingDir() (string, error) {
	return "/tmp/cwd", nil
}
func (s stubPlatform) TempDir() string {
	return "/tmp"
}
func (s stubPlatform) ExecutablePath() (string, error) {
	return "/tmp/asm", nil
}
func (s stubPlatform) UserConfigDir() string         { return "/tmp/home/.asm" }
func (s stubPlatform) MoveToTrash(path string) error { return nil }
func (s stubPlatform) OpenURL(url string) error      { return nil }
func (s stubPlatform) RevealPath(path string) error  { return nil }
func (s stubPlatform) BuiltinIDEs() []platform.IDEEntry {
	return append([]platform.IDEEntry(nil), s.ides...)
}
func (s stubPlatform) PrepareIDEOpen(name, command string, args []string, path string) (string, []string) {
	return "wrapped-" + command, append(append([]string(nil), args...), path)
}

func TestBuiltinsUsesCurrentPlatformDefaults(t *testing.T) {
	restore := platform.SetCurrentForTesting(stubPlatform{
		ides: []platform.IDEEntry{
			{Name: "intellij", Command: "open", Args: []string{"-a", "IntelliJ IDEA"}},
			{Name: "vscode", Command: "open", Args: []string{"-a", "Visual Studio Code"}},
		},
	})
	defer restore()

	got := Builtins(nil)
	if len(got) != 2 {
		t.Fatalf("len(Builtins()) = %d, want 2", len(got))
	}
	if got[0].Command != "open" || got[1].Command != "open" {
		t.Fatalf("Builtins() = %#v, want platform-provided open launchers", got)
	}
}

func TestLaunchCommandDelegatesToCurrentPlatform(t *testing.T) {
	restore := platform.SetCurrentForTesting(stubPlatform{})
	defer restore()

	ide := IDE{Name: "vscode", Command: "code", Args: []string{"--reuse-window"}}
	command, args := ide.launchCommand("/tmp/project")
	if command != "wrapped-code" {
		t.Fatalf("launchCommand() command = %q, want %q", command, "wrapped-code")
	}
	want := []string{"--reuse-window", "/tmp/project"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("launchCommand() args = %#v, want %#v", args, want)
	}
}
