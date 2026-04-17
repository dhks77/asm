package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhn/asm/config"
)

func TestSettingsModelLoadsRepoColorFromMainRepoForWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	mainRepo := filepath.Join(root, "tc-dcm")
	worktreePath := filepath.Join(root, "tc-dcm-1234")
	gitDir := filepath.Join(mainRepo, ".git", "worktrees", "tc-dcm-1234")

	if err := os.MkdirAll(filepath.Join(mainRepo, ".asm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
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
	if err := config.SaveScope(&config.Config{RepoColor: "sky"}, config.ScopeProject, mainRepo); err != nil {
		t.Fatal(err)
	}

	m := NewSettingsModel(nil, worktreePath, nil, nil, nil, nil)
	if m.repoColorName != "tc-dcm" {
		t.Fatalf("repoColorName = %q, want %q", m.repoColorName, "tc-dcm")
	}
	if m.repoColorStr != "sky" {
		t.Fatalf("repoColorStr = %q, want %q", m.repoColorStr, "sky")
	}
	view := m.View()
	if !strings.Contains(view, "Repository") || !strings.Contains(view, "Repo Color") {
		t.Fatalf("expected local settings view to show repository color field:\n%s", view)
	}
}

func TestSettingsSaveWritesRepoColorToProjectScopeOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := filepath.Join(t.TempDir(), "tc-dcm")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "config"), []byte("[remote \"origin\"]\n\turl = git@github.com:nhn/tc-dcm.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveScope(&config.Config{RepoColors: map[string]string{"tc-dcm": "#ABCDEF"}}, config.ScopeUser, ""); err != nil {
		t.Fatal(err)
	}

	m := NewSettingsModel(nil, repoPath, nil, nil, nil, nil)
	m.repoColorStr = "sky"
	if cmd := m.save(); cmd != nil {
		_ = cmd()
	}

	projectCfg, err := config.LoadScope(config.ScopeProject, repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if projectCfg.RepoColor != "#7BC7FF" {
		t.Fatalf("project repo_color = %q, want %q", projectCfg.RepoColor, "#7BC7FF")
	}

	userCfg, err := config.LoadScope(config.ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := userCfg.RepoColors["tc-dcm"]; ok {
		t.Fatalf("expected global repo_colors entry to be removed, got %#v", userCfg.RepoColors)
	}

	m.scopeIdx = 0
	m.loadGeneralFromScope()
	m.rebuildItems()
	view := m.View()
	if strings.Contains(view, "Repository") || strings.Contains(view, "Repo Color") {
		t.Fatalf("expected global settings view to hide repo color field:\n%s", view)
	}
}

func TestSettingsGlobalThemeSavesToUserScopeOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewSettingsModel(nil, "", []string{"claude"}, nil, nil, nil)
	m.scopeIdx = 0
	m.loadGeneralFromScope()
	m.rebuildItems()
	if !strings.Contains(m.View(), "Theme") {
		t.Fatalf("expected global settings view to show Theme field:\n%s", m.View())
	}

	m.selectedTheme = 1 // light
	if cmd := m.save(); cmd != nil {
		_ = cmd()
	}

	userCfg, err := config.LoadScope(config.ScopeUser, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := userCfg.ThemeMode(); got != "light" {
		t.Fatalf("user theme = %q, want %q", got, "light")
	}
}

func TestSettingsLocalViewHidesThemeField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rootPath := t.TempDir()
	m := NewSettingsModel(nil, rootPath, []string{"claude"}, nil, nil, nil)
	m.scopeIdx = 1
	m.loadGeneralFromScope()
	m.rebuildItems()

	if strings.Contains(m.View(), "Theme") {
		t.Fatalf("expected local settings view to hide Theme field:\n%s", m.View())
	}
}
