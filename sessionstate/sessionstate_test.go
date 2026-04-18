package sessionstate

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadDeleteSnapshot(t *testing.T) {
	oldDir := snapshotDir
	oldLast := lastSessionIDPath
	tmp := t.TempDir()
	snapshotDir = func() string { return filepath.Join(tmp, "sessions") }
	lastSessionIDPath = func() string { return filepath.Join(tmp, "last-session-id") }
	defer func() {
		snapshotDir = oldDir
		lastSessionIDPath = oldLast
	}()

	sessionID := "project-a"
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

	if err := Save(sessionID, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(sessionID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got == nil {
		t.Fatal("Load() returned nil snapshot")
	}
	if got.SessionID != sessionID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, sessionID)
	}
	if got.FrontPath != want.FrontPath || got.FrontKind != want.FrontKind || got.FocusedPane != want.FocusedPane || got.WorkingZoomed != want.WorkingZoomed {
		t.Fatalf("loaded snapshot metadata mismatch: got %+v want %+v", *got, want)
	}
	if len(got.Targets) != len(want.Targets) {
		t.Fatalf("len(Targets) = %d, want %d", len(got.Targets), len(want.Targets))
	}

	if err := Delete(sessionID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	got, err = Load(sessionID)
	if err != nil {
		t.Fatalf("Load() after delete error = %v", err)
	}
	if got != nil {
		t.Fatalf("Load() after delete = %+v, want nil", *got)
	}
}

func TestSaveEmptySnapshotDeletesFile(t *testing.T) {
	oldDir := snapshotDir
	oldLast := lastSessionIDPath
	tmp := t.TempDir()
	snapshotDir = func() string { return filepath.Join(tmp, "sessions") }
	lastSessionIDPath = func() string { return filepath.Join(tmp, "last-session-id") }
	defer func() {
		snapshotDir = oldDir
		lastSessionIDPath = oldLast
	}()

	sessionID := "project-b"
	if err := Save(sessionID, Snapshot{Targets: []TargetSnapshot{{Path: "/tmp/project-b", HasAI: true}}}); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	if err := Save(sessionID, Snapshot{}); err != nil {
		t.Fatalf("Save(empty) error = %v", err)
	}
	got, err := Load(sessionID)
	if err != nil {
		t.Fatalf("Load() after empty save error = %v", err)
	}
	if got != nil {
		t.Fatalf("Load() after empty save = %+v, want nil", *got)
	}
}

func TestSaveLoadDeleteLastSessionID(t *testing.T) {
	oldDir := snapshotDir
	oldLast := lastSessionIDPath
	tmp := t.TempDir()
	snapshotDir = func() string { return filepath.Join(tmp, "sessions") }
	lastSessionIDPath = func() string { return filepath.Join(tmp, "last-session-id") }
	defer func() {
		snapshotDir = oldDir
		lastSessionIDPath = oldLast
	}()

	if err := SaveLastSessionID("alpha-1"); err != nil {
		t.Fatalf("SaveLastSessionID() error = %v", err)
	}
	got, err := LoadLastSessionID()
	if err != nil {
		t.Fatalf("LoadLastSessionID() error = %v", err)
	}
	if got != "alpha-1" {
		t.Fatalf("LoadLastSessionID() = %q, want %q", got, "alpha-1")
	}
	if err := DeleteLastSessionID(); err != nil {
		t.Fatalf("DeleteLastSessionID() error = %v", err)
	}
	got, err = LoadLastSessionID()
	if err != nil {
		t.Fatalf("LoadLastSessionID() after delete error = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadLastSessionID() after delete = %q, want empty", got)
	}
}
