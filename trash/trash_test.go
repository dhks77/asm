package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhn/asm/platform"
)

func TestMoveMovesDirectoryIntoTrash(t *testing.T) {
	if platform.Current().Name() != "darwin" {
		t.Skip("trash integration is only supported on darwin")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	srcParent := filepath.Join(home, "workspace")
	src := filepath.Join(srcParent, "project")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	if err := Move(src); err != nil {
		t.Fatalf("Move(%q) error = %v", src, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists, err=%v", err)
	}

	moved := filepath.Join(expectedTrashDir(home), "project")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("trashed dir missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(moved, "note.txt"))
	if err != nil {
		t.Fatalf("read moved file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("moved file contents = %q, want %q", string(data), "hello")
	}
}

func TestMoveAddsUniqueSuffixWhenTrashAlreadyContainsName(t *testing.T) {
	if platform.Current().Name() != "darwin" {
		t.Skip("trash integration is only supported on darwin")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	first := filepath.Join(home, "one", "project")
	second := filepath.Join(home, "two", "project")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}

	if err := Move(first); err != nil {
		t.Fatalf("Move(first) error = %v", err)
	}
	if err := Move(second); err != nil {
		t.Fatalf("Move(second) error = %v", err)
	}

	entries, err := os.ReadDir(expectedTrashDir(home))
	if err != nil {
		t.Fatalf("ReadDir(trash): %v", err)
	}

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !contains(names, "project") {
		t.Fatalf("trash entries = %v, want original name present", names)
	}
	if !containsWithPrefix(names, "project ") {
		t.Fatalf("trash entries = %v, want suffixed collision entry", names)
	}
}

func expectedTrashDir(home string) string {
	return filepath.Join(home, ".Trash")
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func containsWithPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}
