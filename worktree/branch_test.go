package worktree

import (
	"strings"
	"testing"
)

func TestCreateWorktreeNewBranchRequiresBaseBranch(t *testing.T) {
	err := CreateWorktreeNewBranch("", "", "feature/test", "")
	if err == nil {
		t.Fatal("expected error for empty base branch")
	}
	if !strings.Contains(err.Error(), "base branch is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
