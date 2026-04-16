package tmux

import "testing"

func TestTargetIDStableForCleanPath(t *testing.T) {
	pathA := "/tmp/project-a"
	pathB := "/tmp/project-a/../project-a"

	gotA := TargetID(pathA)
	gotB := TargetID(pathB)
	if gotA == "" {
		t.Fatal("TargetID returned empty string")
	}
	if gotA != gotB {
		t.Fatalf("TargetID should be stable for cleaned paths: %q != %q", gotA, gotB)
	}
}

func TestWindowNamesArePathBased(t *testing.T) {
	pathA := "/tmp/worktrees/api-4012"
	pathB := "/tmp/other/api-4012"

	if WindowName(pathA) == WindowName(pathB) {
		t.Fatalf("WindowName should differ for different paths: %q", WindowName(pathA))
	}
	if TerminalWindowName(pathA) == TerminalWindowName(pathB) {
		t.Fatalf("TerminalWindowName should differ for different paths: %q", TerminalWindowName(pathA))
	}
	if ExitSignalName(pathA) == ExitSignalName(pathB) {
		t.Fatalf("ExitSignalName should differ for different paths: %q", ExitSignalName(pathA))
	}
}
