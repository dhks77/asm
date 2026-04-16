package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRootFallsBackToTargetPathWhenNoConfigExists(t *testing.T) {
	dir := t.TempDir()
	if got := ProjectRoot(dir); got != dir {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", dir, got, dir)
	}
}

func TestProjectRootFindsNearestAsmConfig(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "child", "nested")
	if err := os.MkdirAll(filepath.Join(root, ".asm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".asm", "config.toml"), []byte("default_provider = \"codex\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := ProjectRoot(target); got != root {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", target, got, root)
	}
}

func TestProjectRootReturnsMainRepoForGitDir(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := ProjectRoot(repo); got != repo {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", repo, got, repo)
	}
}

func TestProjectRootResolvesLinkedWorktreeWithoutGitCommand(t *testing.T) {
	root := t.TempDir()
	mainRepo := filepath.Join(root, "repo")
	worktreePath := filepath.Join(root, "wt-feature")
	gitDir := filepath.Join(mainRepo, ".git", "worktrees", "wt-feature")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "gitdir: " + gitDir + "\n"
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../.."), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := ProjectRoot(worktreePath); got != mainRepo {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", worktreePath, got, mainRepo)
	}
}

func TestProjectRootResolvesBareCommonDirWorktree(t *testing.T) {
	root := t.TempDir()
	repoHome := filepath.Join(root, "tc-dcm-new")
	worktreePath := filepath.Join(root, "hotfix-3912")
	gitDir := filepath.Join(repoHome, ".bare", "worktrees", "hotfix-3912")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../.."), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := ProjectRoot(worktreePath); got != repoHome {
		t.Fatalf("ProjectRoot(%q) = %q, want %q", worktreePath, got, repoHome)
	}
}

func TestProjectIdentityUsesOriginRepoNameForGrouping(t *testing.T) {
	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configBody := `
[remote "origin"]
    url = git@github.com:nhn/tc-dcm.git
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	root, name := ProjectIdentity(repo)
	if root != repo {
		t.Fatalf("root = %q, want %q", root, repo)
	}
	if name != "tc-dcm" {
		t.Fatalf("name = %q, want %q", name, "tc-dcm")
	}
}

func TestProjectIdentityUsesOriginRepoNameForLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	repoHome := filepath.Join(root, "tc-dcm-new")
	worktreePath := filepath.Join(root, "hotfix-3912")
	gitDir := filepath.Join(repoHome, ".bare", "worktrees", "hotfix-3912")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../.."), 0o644); err != nil {
		t.Fatal(err)
	}
	configBody := `
[remote "origin"]
    url = ssh://git@github.com/nhn/tc-dcm.git
`
	if err := os.WriteFile(filepath.Join(repoHome, ".bare", "config"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	projectRoot, name := ProjectIdentity(worktreePath)
	if projectRoot != repoHome {
		t.Fatalf("root = %q, want %q", projectRoot, repoHome)
	}
	if name != "tc-dcm" {
		t.Fatalf("name = %q, want %q", name, "tc-dcm")
	}
}
