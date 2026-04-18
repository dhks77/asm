package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasCodexSessionInDir(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	cwd := "/tmp/project-a"

	writeTestFile(t, filepath.Join(sessionsDir, "bad.jsonl"), `not-json`)
	writeTestFile(t, filepath.Join(sessionsDir, "2026", "04", "16", "other.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/tmp/project-b"}}`+"\n")
	writeTestFile(t, filepath.Join(sessionsDir, "2026", "04", "16", "match.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/tmp/project-a"}}`+"\n"+
			`{"type":"message","payload":{"text":"hello"}}`+"\n")

	if !hasCodexSessionInDir(sessionsDir, cwd) {
		t.Fatalf("expected session for %q", cwd)
	}
}

func TestHasCodexSessionInDirNoMatch(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	writeTestFile(t, filepath.Join(sessionsDir, "2026", "04", "16", "other.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/tmp/project-b"}}`+"\n")

	if hasCodexSessionInDir(sessionsDir, "/tmp/project-a") {
		t.Fatal("expected no session match")
	}
}

func TestCodexProviderResumeArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sessionsDir := filepath.Join(home, ".codex", "sessions", "2026", "04", "16")
	writeTestFile(t, filepath.Join(sessionsDir, "match.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/tmp/project-a"}}`+"\n")

	p := NewCodexProvider("", nil)
	if got := p.ResumeArgs("/tmp/project-a"); len(got) != 2 || got[0] != "resume" || got[1] != "--last" {
		t.Fatalf("unexpected resume args: %#v", got)
	}
	if got := p.ResumeArgs("/tmp/project-b"); got != nil {
		t.Fatalf("expected nil resume args for missing cwd, got %#v", got)
	}
}

func TestCodexDetectStateIdlePrompt(t *testing.T) {
	p := NewCodexProvider("", nil)
	content := "Done.\n› Find and fix a bug in @filename\n  gpt-5.4 xhigh · ~/Documents/project/asm\n"

	if got := p.DetectState("", content); got != StateIdle {
		t.Fatalf("DetectState() = %v, want %v", got, StateIdle)
	}
}

func TestCodexDetectStateThinking(t *testing.T) {
	p := NewCodexProvider("", nil)
	content := "Reviewing files\nThinking through the change\nEsc to interrupt\n"

	if got := p.DetectState("", content); got != StateThinking {
		t.Fatalf("DetectState() = %v, want %v", got, StateThinking)
	}
}

func TestCodexDetectStateBusyWhileWorkingPromptVisible(t *testing.T) {
	p := NewCodexProvider("", nil)
	content := "• Working (20s • esc to interrupt)\n› Find and fix a bug in @filename\n  gpt-5.4 xhigh · ~/Documents/project/asm\n"

	if got := p.DetectState("", content); got != StateBusy {
		t.Fatalf("DetectState() = %v, want %v", got, StateBusy)
	}
}

func TestCodexDetectStateToolUse(t *testing.T) {
	p := NewCodexProvider("", nil)
	content := "Background command \"go test ./...\" completed (exit code 0)\nEsc to interrupt\n"

	if got := p.DetectState("", content); got != StateToolUse {
		t.Fatalf("DetectState() = %v, want %v", got, StateToolUse)
	}
}

func TestCodexDetectStateResponding(t *testing.T) {
	p := NewCodexProvider("", nil)
	content := "I found the issue in the state detector.\nI'll patch it now.\nAccept edits\n"

	if got := p.DetectState("", content); got != StateResponding {
		t.Fatalf("DetectState() = %v, want %v", got, StateResponding)
	}
}

func TestCodexDetectStateFallsBackToBusy(t *testing.T) {
	p := NewCodexProvider("", nil)
	content := "for shortcuts\nplan mode\n"

	if got := p.DetectState("", content); got != StateBusy {
		t.Fatalf("DetectState() = %v, want %v", got, StateBusy)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
