package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentBranchReadsMainRepoHeadDirectly(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "HEAD"), []byte("ref: refs/heads/feature/123\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	if got := CurrentBranch(repoPath); got != "feature/123" {
		t.Fatalf("CurrentBranch() = %q, want %q", got, "feature/123")
	}
}

func TestCurrentBranchReadsLinkedWorktreeHeadDirectly(t *testing.T) {
	worktreePath := t.TempDir()
	gitDir := filepath.Join(t.TempDir(), "admin")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir admin gitdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/bugfix/42\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	if got := CurrentBranch(worktreePath); got != "bugfix/42" {
		t.Fatalf("CurrentBranch() = %q, want %q", got, "bugfix/42")
	}
}
