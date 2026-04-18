package tmux

import "testing"

func TestSessionNameFollowsSessionID(t *testing.T) {
	SetSessionID("alpha_1")
	if SessionID != "alpha_1" {
		t.Fatalf("SessionID = %q, want %q", SessionID, "alpha_1")
	}
	if SessionName != "asm-alpha_1" {
		t.Fatalf("SessionName = %q, want %q", SessionName, "asm-alpha_1")
	}
}

func TestSessionIDFromName(t *testing.T) {
	if got := SessionIDFromName("asm-billing"); got != "billing" {
		t.Fatalf("SessionIDFromName() = %q, want %q", got, "billing")
	}
	if got := SessionIDFromName("plain-tmux"); got != "" {
		t.Fatalf("SessionIDFromName(non-asm) = %q, want empty", got)
	}
}

func TestValidateSessionID(t *testing.T) {
	if err := ValidateSessionID("billing_123"); err != nil {
		t.Fatalf("ValidateSessionID(valid) error = %v", err)
	}
	if err := ValidateSessionID("bad/id"); err == nil {
		t.Fatal("ValidateSessionID(invalid) returned nil error")
	}
}

func TestParseASMSessionIDs(t *testing.T) {
	out := "asm-billing\nplain-shell\nasm-ops_1\n\n"
	got := parseASMSessionIDs(out)
	if len(got) != 2 {
		t.Fatalf("len(parseASMSessionIDs()) = %d, want 2", len(got))
	}
	if got[0] != "billing" || got[1] != "ops_1" {
		t.Fatalf("parseASMSessionIDs() = %#v, want [billing ops_1]", got)
	}
}

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

func TestSanitizeCapturedOutputDropsInvalidUTF8(t *testing.T) {
	got := sanitizeCapturedOutput([]byte{'a', 0xff, 'b', 0xfe, 'c'})
	if got != "abc" {
		t.Fatalf("sanitizeCapturedOutput() = %q, want %q", got, "abc")
	}
}
