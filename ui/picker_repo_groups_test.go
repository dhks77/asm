package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nhn/asm/config"
)

func TestRepoMetadataForPathsUsesMainRepoProjectColorForWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	mainRepo := filepath.Join(root, "tc-dcm")
	worktreePath := filepath.Join(root, "tc-dcm-1234")
	gitDir := filepath.Join(mainRepo, ".git", "worktrees", "tc-dcm-1234")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mainRepo, ".asm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainRepo, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:nhn/tc-dcm.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../.."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveScope(&config.Config{RepoColor: "#123456"}, config.ScopeProject, mainRepo); err != nil {
		t.Fatal(err)
	}

	repoRoots, repoLabels, repoColors := repoMetadataForPaths([]string{worktreePath})
	if got := repoRoots[worktreePath]; got != mainRepo {
		t.Fatalf("repoRoots[%q] = %q, want %q", worktreePath, got, mainRepo)
	}
	if got := repoLabels[worktreePath]; got != "tc-dcm" {
		t.Fatalf("repoLabels[%q] = %q, want %q", worktreePath, got, "tc-dcm")
	}
	if got := repoColors[mainRepo]; got != "#123456" {
		t.Fatalf("repoColors[%q] = %q, want %q", mainRepo, got, "#123456")
	}
}

func TestRepoMetadataForPathsMigratesLegacyGlobalRepoColorToProjectScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	mainRepo := filepath.Join(root, "tc-dcm")
	if err := os.MkdirAll(filepath.Join(mainRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainRepo, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:nhn/tc-dcm.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveScope(&config.Config{RepoColors: map[string]string{"tc-dcm": "117"}}, config.ScopeUser, ""); err != nil {
		t.Fatal(err)
	}

	_, _, repoColors := repoMetadataForPaths([]string{mainRepo})
	if got := repoColors[mainRepo]; got != "117" {
		t.Fatalf("repoColors[%q] = %q, want %q", mainRepo, got, "117")
	}

	projectCfg, err := config.LoadScope(config.ScopeProject, mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	if projectCfg.RepoColor != "117" {
		t.Fatalf("project repo_color = %q, want %q", projectCfg.RepoColor, "117")
	}

	userCfg, err := config.LoadScope(config.ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := userCfg.RepoColors["tc-dcm"]; ok {
		t.Fatalf("expected legacy user repo color to be removed after migration: %#v", userCfg.RepoColors)
	}
}
