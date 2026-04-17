package favorites

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToggleAddsAndRemovesEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	added, err := Toggle(KindDir, "/tmp/demo")
	if err != nil {
		t.Fatalf("toggle add failed: %v", err)
	}
	if !added {
		t.Fatalf("expected add toggle to return true")
	}

	entries, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Kind != KindDir || entries[0].Path != "/tmp/demo" {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}

	added, err = Toggle(KindDir, "/tmp/demo")
	if err != nil {
		t.Fatalf("toggle remove failed: %v", err)
	}
	if added {
		t.Fatalf("expected remove toggle to return false")
	}

	entries, err = Load()
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries after removal, got %#v", entries)
	}
	if _, err := os.Stat(filePath()); !os.IsNotExist(err) {
		t.Fatalf("expected favorites file to be removed, err=%v", err)
	}
}

func TestToggleReplacesOtherKindForSamePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := Toggle(KindDir, "/tmp/demo"); err != nil {
		t.Fatalf("toggle dir failed: %v", err)
	}
	if _, err := Toggle(KindRepo, "/tmp/demo"); err != nil {
		t.Fatalf("toggle repo failed: %v", err)
	}

	entries, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Path != filepath.Clean("/tmp/demo") {
		t.Fatalf("unexpected path: %#v", entry)
	}
	if entry.Kind != KindRepo {
		t.Fatalf("expected repo favorite to replace dir favorite, got %#v", entries)
	}
}

func TestSortEntriesOrdersNewestFirst(t *testing.T) {
	entries := []Entry{
		{Kind: KindRepo, Path: "/b", UpdatedAt: 10},
		{Kind: KindDir, Path: "/a", UpdatedAt: 30},
		{Kind: KindDir, Path: "/c", UpdatedAt: 20},
	}

	sortEntries(entries)

	if entries[0].Path != "/a" || entries[1].Path != "/c" || entries[2].Path != "/b" {
		t.Fatalf("unexpected order: %#v", entries)
	}
}
