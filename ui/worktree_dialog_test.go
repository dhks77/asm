package ui

import (
	"strings"
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

func TestWorktreeDialogCtrlNTogglesBackToExistingBranchFlow(t *testing.T) {
	m := WorktreeDialogModel{
		visible:       true,
		mode:          wtModeSelectBase,
		branches:      []worktree.Branch{{Name: "main"}},
		filtered:      []worktree.Branch{{Name: "main"}},
		filter:        "feat",
		cursor:        1,
		scrollOffset:  1,
		newBranchName: "feature/x",
		baseBranch:    "main",
		taskInfos: map[string]tracker.TaskInfo{
			"main": {Name: "Task"},
		},
	}

	got, _ := m.handleSelectBaseKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	if got.mode != wtModeSelectBranch {
		t.Fatalf("mode = %v, want %v", got.mode, wtModeSelectBranch)
	}
	if got.filter != "" || got.cursor != 0 || got.scrollOffset != 0 {
		t.Fatalf("expected reset state, got filter=%q cursor=%d scroll=%d", got.filter, got.cursor, got.scrollOffset)
	}
	if got.newBranchName != "" || got.baseBranch != "" {
		t.Fatalf("expected new-branch state cleared, got name=%q base=%q", got.newBranchName, got.baseBranch)
	}
}

func TestWorktreeDialogCtrlNInNameInputTogglesBackToExistingBranchFlow(t *testing.T) {
	m := WorktreeDialogModel{
		visible:       true,
		mode:          wtModeNewBranch,
		branches:      []worktree.Branch{{Name: "main"}},
		filtered:      []worktree.Branch{{Name: "main"}},
		newBranchName: "feature/x",
		baseBranch:    "main",
	}

	got, _ := m.handleNewBranchKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	if got.mode != wtModeSelectBranch {
		t.Fatalf("mode = %v, want %v", got.mode, wtModeSelectBranch)
	}
	if got.newBranchName != "" || got.baseBranch != "" {
		t.Fatalf("expected new-branch state cleared, got name=%q base=%q", got.newBranchName, got.baseBranch)
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

func TestWorktreeDialogNewBranchViewsShowWorktreeTitle(t *testing.T) {
	baseView := WorktreeDialogModel{
		visible:  true,
		mode:     wtModeSelectBase,
		repoName: "accounts",
		width:    80,
		height:   12,
	}.View()
	if !containsAll(baseView, "Create Worktree from New Branch", "^n: existing branch") {
		t.Fatalf("unexpected base view:\n%s", baseView)
	}

	newBranchView := WorktreeDialogModel{
		visible:    true,
		mode:       wtModeNewBranch,
		repoName:   "accounts",
		baseBranch: "main",
		width:      80,
		height:     12,
	}.View()
	if !containsAll(newBranchView, "Create Worktree from New Branch", "^n: existing branch") {
		t.Fatalf("unexpected new-branch view:\n%s", newBranchView)
	}
}

func TestWorktreeDialogEnterNewBranchModePrefersCurrentBranch(t *testing.T) {
	m := WorktreeDialogModel{
		branches: []worktree.Branch{
			{Name: "main", IsLocal: true},
			{Name: "feature/current", IsLocal: true},
			{Name: "origin/release/next"},
		},
		currentBranch: "feature/current",
		taskInfos:     make(map[string]tracker.TaskInfo),
		maxVisible:    10,
	}

	m.enterNewBranchMode()

	if m.mode != wtModeSelectBase {
		t.Fatalf("mode = %v, want %v", m.mode, wtModeSelectBase)
	}
	if len(m.filtered) != 3 {
		t.Fatalf("filtered len = %d, want 3", len(m.filtered))
	}
	if got := m.filtered[m.cursor].Name; got != "feature/current" {
		t.Fatalf("selected base = %q, want %q", got, "feature/current")
	}
}

func TestPreferredBaseBranchFallsBackToMainWhenCurrentDetached(t *testing.T) {
	branches := []worktree.Branch{
		{Name: "origin/main"},
		{Name: "release/next", IsLocal: true},
	}

	if got := preferredBaseBranch("HEAD", branches); got != "origin/main" {
		t.Fatalf("preferredBaseBranch(HEAD) = %q, want %q", got, "origin/main")
	}
}

func TestWorktreeDialogResolvedTaskReappliesFilter(t *testing.T) {
	m := WorktreeDialogModel{
		visible:     true,
		mode:        wtModeSelectBranch,
		repoDir:     "/tmp/repo",
		loadVersion: 3,
		branches: []worktree.Branch{
			{Name: "main", IsLocal: true},
			{Name: "feature/123", IsLocal: true},
		},
		taskInfos:  make(map[string]tracker.TaskInfo),
		filter:     "billing",
		maxVisible: 10,
	}
	m.applyFilter()
	if len(m.filtered) != 0 {
		t.Fatalf("initial filtered len = %d, want 0", len(m.filtered))
	}

	m.taskFetch = newAsyncStringQueue(worktreeBranchTaskKey)
	m.taskFetch.Enqueue("feature/123")
	m.taskFetch.StartAvailable(nil)
	m.taskResults = newAsyncResultBuffer[wtTaskResult]()
	m.taskResults.Push(wtTaskResult{
		Version: 3,
		RepoDir: "/tmp/repo",
		Branch:  "feature/123",
		Info:    tracker.TaskInfo{Name: "Billing fix"},
	})
	got, _ := m.Update(wtTaskPollMsg{version: 3})

	if len(got.filtered) != 1 {
		t.Fatalf("filtered len after resolve = %d, want 1", len(got.filtered))
	}
	if got.filtered[0].Name != "feature/123" {
		t.Fatalf("filtered[0] = %q, want %q", got.filtered[0].Name, "feature/123")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
