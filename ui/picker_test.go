package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/worktree"
)

func TestPickerTypingOStartsSearch(t *testing.T) {
	m := PickerModel{
		directories:   []worktree.Worktree{{Name: "alpha", Path: "/tmp/alpha"}},
		branches:      map[string]string{},
		taskInfos:     map[string]tracker.TaskInfo{},
		selectedItems: map[string]bool{},
	}

	model, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	got := model.(PickerModel)
	if got.searchQuery != "o" {
		t.Fatalf("searchQuery = %q, want %q", got.searchQuery, "o")
	}
}

func TestPickerViewShowsSearchLineWhenNoMatches(t *testing.T) {
	m := PickerModel{
		rootPath:      "/tmp/root",
		directories:   []worktree.Worktree{{Name: "alpha", Path: "/tmp/alpha"}},
		branches:      map[string]string{},
		taskInfos:     map[string]tracker.TaskInfo{},
		selectedItems: map[string]bool{},
		width:         80,
		height:        8,
		ready:         true,
		focused:       true,
		searchQuery:   "zzz",
	}

	view := m.View()
	if !strings.Contains(view, "/ zzz") {
		t.Fatalf("view should contain search line, got:\n%s", view)
	}
	if !strings.Contains(view, "No matching sessions") {
		t.Fatalf("view should contain empty-state message, got:\n%s", view)
	}
}
