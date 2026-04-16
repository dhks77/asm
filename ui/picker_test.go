package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	asmtmux "github.com/nhn/asm/tmux"
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

func TestPickerFilteredDirectoriesGroupsByRepo(t *testing.T) {
	m := PickerModel{
		directories: []worktree.Worktree{
			{Name: "billing-a", Path: "/tmp/billing/a"},
			{Name: "accounts-a", Path: "/tmp/accounts/a"},
			{Name: "billing-b", Path: "/tmp/billing/b"},
			{Name: "accounts-b", Path: "/tmp/accounts/b"},
		},
		repoRoots: map[string]string{
			"/tmp/billing/a":  "billing",
			"/tmp/accounts/a": "accounts",
			"/tmp/billing/b":  "billing",
			"/tmp/accounts/b": "accounts",
		},
		repoColors: map[string]string{},
		activeKinds: map[string]asmtmux.SessionKind{
			"/tmp/accounts/b": asmtmux.SessionAI,
			"/tmp/billing/a":  asmtmux.SessionAI,
		},
		branches:      map[string]string{},
		taskInfos:     map[string]tracker.TaskInfo{},
		selectedItems: map[string]bool{},
	}

	filtered := m.filteredDirectories()
	var got []string
	for _, idx := range filtered {
		got = append(got, m.directories[idx].Path)
	}

	want := []string{
		"/tmp/accounts/b",
		"/tmp/accounts/a",
		"/tmp/billing/a",
		"/tmp/billing/b",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("filtered order = %v, want %v", got, want)
	}
}

func TestPickerSeedsCachedBranchAndQueuesMetadataSequentially(t *testing.T) {
	m := PickerModel{
		branches:       map[string]string{},
		taskInfos:      map[string]tracker.TaskInfo{},
		cachedBranches: map[string]string{},
		branchVerified: map[string]bool{},
		queuedBranches: map[string]bool{},
		queuedTasks:    map[string]bool{},
		selectedItems:  map[string]bool{},
	}

	model, cmd := m.Update(DirectoriesScannedMsg{
		Directories: []worktree.Worktree{
			{Name: "a", Path: "/tmp/a"},
			{Name: "b", Path: "/tmp/b"},
		},
		CachedTasks: map[string]tracker.TaskInfo{
			"/tmp/a": {Name: "Task A"},
		},
		CachedBranches: map[string]string{
			"/tmp/a": "feature/a",
		},
		RepoRoots:  map[string]string{},
		RepoColors: map[string]string{},
	})
	got := model.(PickerModel)

	if got.branches["/tmp/a"] != "feature/a" {
		t.Fatalf("cached branch was not seeded: %#v", got.branches)
	}
	if !got.branchFetchPending {
		t.Fatalf("expected first branch fetch to start immediately")
	}
	if got.taskFetchPending {
		t.Fatalf("task fetch should not start before branch verification")
	}
	if len(got.branchFetchQueue) != 1 || got.branchFetchQueue[0] != "/tmp/b" {
		t.Fatalf("unexpected branch queue: %#v", got.branchFetchQueue)
	}
	if cmd == nil {
		t.Fatalf("expected metadata fetch command")
	}
}

func TestEnsureDirectoryTrackedSeedsRepoMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := filepath.Join(home, "tc-dcm-new")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	configData := "[remote \"origin\"]\n\turl = git@github.com:nhn/tc-dcm.git\n"
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "config"), []byte(configData), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}

	m := PickerModel{
		branches:       map[string]string{},
		repoRoots:      map[string]string{},
		repoColors:     map[string]string{},
		queuedBranches: map[string]bool{},
		branchVerified: map[string]bool{},
	}

	wt, _ := m.ensureDirectoryTracked(repoPath)
	if wt == nil {
		t.Fatalf("ensureDirectoryTracked returned nil")
	}
	if got := m.repoRoots[repoPath]; got != "tc-dcm" {
		t.Fatalf("repoRoots[%q] = %q, want %q", repoPath, got, "tc-dcm")
	}
	if got := m.repoColors["tc-dcm"]; got == "" {
		t.Fatalf("repoColors should be populated for %q", "tc-dcm")
	}
}
