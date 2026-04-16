package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/worktree"
)

func TestWorktreeDialogCtrlNStartsNewBranchFlow(t *testing.T) {
	m := WorktreeDialogModel{
		visible: true,
		mode:    wtModeSelectBranch,
		branches: []worktree.Branch{
			{Name: "main"},
			{Name: "feature/test"},
		},
		filtered: []worktree.Branch{
			{Name: "main"},
		},
		filter:       "feat",
		cursor:       1,
		scrollOffset: 1,
	}

	got, _ := m.handleSelectBranchKey(tea.KeyMsg{Type: tea.KeyCtrlN})

	if got.mode != wtModeSelectBase {
		t.Fatalf("mode = %v, want %v", got.mode, wtModeSelectBase)
	}
	if got.filter != "" {
		t.Fatalf("filter = %q, want empty", got.filter)
	}
	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", got.cursor)
	}
	if got.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0", got.scrollOffset)
	}
}

func TestWorktreeDialogF10StartsNewBranchFlow(t *testing.T) {
	m := WorktreeDialogModel{
		visible: true,
		mode:    wtModeSelectBranch,
		branches: []worktree.Branch{
			{Name: "main"},
			{Name: "feature/test"},
		},
		filtered: []worktree.Branch{
			{Name: "main"},
		},
	}

	got, _ := m.handleSelectBranchKey(tea.KeyMsg{Type: tea.KeyF10})
	if got.mode != wtModeSelectBase {
		t.Fatalf("mode = %v, want %v", got.mode, wtModeSelectBase)
	}
}

func TestWorktreeDialogTabSwitchesRepo(t *testing.T) {
	m := WorktreeDialogModel{
		visible:       true,
		mode:          wtModeSelectBranch,
		rootPath:      "/tmp/accounts/a",
		dirPath:       "/tmp/accounts/a",
		repoDir:       "/tmp/accounts/a",
		repoName:      "accounts",
		filter:        "abc",
		cursor:        2,
		scrollOffset:  1,
		newBranchName: "feature/x",
		baseBranch:    "main",
		taskInfos: map[string]tracker.TaskInfo{
			"main": {Name: "Task"},
		},
		repoChoices: []worktreeRepoChoice{
			{Path: "/tmp/accounts/a", Root: "/tmp/accounts", Name: "accounts"},
			{Path: "/tmp/billing/a", Root: "/tmp/billing", Name: "billing"},
		},
	}

	got, _ := m.handleSelectBranchKey(tea.KeyMsg{Type: tea.KeyTab})
	if got.repoIndex != 1 {
		t.Fatalf("repoIndex = %d, want 1", got.repoIndex)
	}
	if got.rootPath != "/tmp/billing/a" || got.dirPath != "/tmp/billing/a" {
		t.Fatalf("repo paths = %q / %q, want /tmp/billing/a", got.rootPath, got.dirPath)
	}
	if got.mode != wtModeSelectBranch {
		t.Fatalf("mode = %v, want %v", got.mode, wtModeSelectBranch)
	}
	if got.filter != "" || got.cursor != 0 || got.scrollOffset != 0 {
		t.Fatalf("expected reset state after repo switch, got filter=%q cursor=%d scroll=%d", got.filter, got.cursor, got.scrollOffset)
	}
	if len(got.taskInfos) != 0 {
		t.Fatalf("taskInfos should reset on repo switch, got %#v", got.taskInfos)
	}
}
