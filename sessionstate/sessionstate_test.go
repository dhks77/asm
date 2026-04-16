package sessionstate

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadDeleteSnapshot(t *testing.T) {
	oldDir := snapshotDir
	tmp := t.TempDir()
	snapshotDir = func() string { return filepath.Join(tmp, "sessions") }
	defer func() { snapshotDir = oldDir }()

	root := "/tmp/project-a"
	want := Snapshot{
		FrontPath:     "/tmp/project-a/api",
		FrontKind:     "ai",
		FocusedPane:   "working",
		WorkingZoomed: true,
		Targets: []TargetSnapshot{
			{Path: "/tmp/project-a/api", HasAI: true, Provider: "claude"},
			{Path: "/tmp/project-a/shell", HasTerm: true},
		},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got == nil {
		t.Fatal("Load() returned nil snapshot")
	}
	if got.RootPath != filepath.Clean(root) {
		t.Fatalf("RootPath = %q, want %q", got.RootPath, filepath.Clean(root))
	}
	if got.FrontPath != want.FrontPath || got.FrontKind != want.FrontKind || got.FocusedPane != want.FocusedPane || got.WorkingZoomed != want.WorkingZoomed {
		t.Fatalf("loaded snapshot metadata mismatch: got %+v want %+v", *got, want)
	}
	if len(got.Targets) != len(want.Targets) {
		t.Fatalf("len(Targets) = %d, want %d", len(got.Targets), len(want.Targets))
	}

	if err := Delete(root); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	got, err = Load(root)
	if err != nil {
		t.Fatalf("Load() after delete error = %v", err)
	}
	if got != nil {
		t.Fatalf("Load() after delete = %+v, want nil", *got)
	}
}

func TestSaveEmptySnapshotDeletesFile(t *testing.T) {
	oldDir := snapshotDir
	tmp := t.TempDir()
	snapshotDir = func() string { return filepath.Join(tmp, "sessions") }
	defer func() { snapshotDir = oldDir }()

	root := "/tmp/project-b"
	if err := Save(root, Snapshot{Targets: []TargetSnapshot{{Path: root, HasAI: true}}}); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	if err := Save(root, Snapshot{}); err != nil {
		t.Fatalf("Save(empty) error = %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load() after empty save error = %v", err)
	}
	if got != nil {
		t.Fatalf("Load() after empty save = %+v, want nil", *got)
	}
}
