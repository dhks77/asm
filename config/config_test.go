package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRootFallsBackToTargetPathWhenNoConfigExists(t *testing.T) {
	dir := t.TempDir()
	if got := ProjectRoot(dir); got != dir {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", dir, got, dir)
	}
}

func TestProjectRootFindsNearestAsmConfig(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "child", "nested")
	if err := os.MkdirAll(filepath.Join(root, ".asm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".asm", "config.toml"), []byte("default_provider = \"codex\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := ProjectRoot(target); got != root {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", target, got, root)
	}
}
