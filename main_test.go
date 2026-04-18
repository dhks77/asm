package main

import (
	"path/filepath"
	"testing"
)

func TestParseRequestedSessionID(t *testing.T) {
	got, err := parseRequestedSessionID("alpha", "")
	if err != nil {
		t.Fatalf("parseRequestedSessionID() error = %v", err)
	}
	if got != "alpha" {
		t.Fatalf("parseRequestedSessionID() = %q, want %q", got, "alpha")
	}
}

func TestParseRequestedSessionIDRejectsConflict(t *testing.T) {
	if _, err := parseRequestedSessionID("alpha", "beta"); err == nil {
		t.Fatal("parseRequestedSessionID() returned nil error for conflicting flags")
	}
}

func TestResolveContextPathDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ASM_CONTEXT_PATH", "")

	got, err := resolveContextPath()
	if err != nil {
		t.Fatalf("resolveContextPath() error = %v", err)
	}
	if got != filepath.Clean(home) {
		t.Fatalf("resolveContextPath() = %q, want %q", got, filepath.Clean(home))
	}
}
