package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type stubPlatform struct {
	name string
	ides []IDEEntry
}

func (s stubPlatform) Name() string { return s.name }
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
func (s stubPlatform) Notify(title, body string)     {}
func (s stubPlatform) MoveToTrash(path string) error { return nil }
func (s stubPlatform) OpenURL(url string) error      { return nil }
func (s stubPlatform) RevealPath(path string) error  { return nil }
func (s stubPlatform) BuiltinIDEs() []IDEEntry       { return append([]IDEEntry(nil), s.ides...) }
func (s stubPlatform) PrepareIDEOpen(name, command string, args []string, path string) (string, []string) {
	return appendPath(command, args, path)
}

func TestCurrentCanBeOverriddenForTesting(t *testing.T) {
	restore := SetCurrentForTesting(stubPlatform{name: "stub"})
	defer restore()

	if got := Current().Name(); got != "stub" {
		t.Fatalf("Current().Name() = %q, want %q", got, "stub")
	}
}

func TestDarwinPrepareIDEOpen_IntelliJUsesOpenWithArgs(t *testing.T) {
	p := newDarwinPlatform()
	command, args := p.PrepareIDEOpen("intellij", "open", []string{"-a", "IntelliJ IDEA"}, "/tmp/project")
	if command != "open" {
		t.Fatalf("PrepareIDEOpen() command = %q, want %q", command, "open")
	}
	want := []string{"-n", "-a", "IntelliJ IDEA", "--args", "/tmp/project"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("PrepareIDEOpen() args = %#v, want %#v", args, want)
	}
}

func TestMoveByRenameUsesExpectedTrashDirNaming(t *testing.T) {
	home := t.TempDir()
	srcParent := filepath.Join(home, "workspace")
	src := filepath.Join(srcParent, "project")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	trashDir := filepath.Join(home, ".Trash")
	if err := moveByRename(src, trashDir, nil); err != nil {
		t.Fatalf("moveByRename() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(trashDir, "project", "note.txt")); err != nil {
		t.Fatalf("trashed file missing: %v", err)
	}
}

func TestPlatformImplUserConfigDirFallsBackToTempDir(t *testing.T) {
	p := &platformImpl{
		name: "stub",
		homeDir: func() (string, error) {
			return "", os.ErrNotExist
		},
		tempDir: func() string {
			return "/tmp/fallback"
		},
	}
	if got := p.UserConfigDir(); got != "/tmp/fallback/.asm" {
		t.Fatalf("UserConfigDir() = %q, want %q", got, "/tmp/fallback/.asm")
	}
}
