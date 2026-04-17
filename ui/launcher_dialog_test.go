package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nhn/asm/tracker"
)

type fakeLauncherTracker struct {
	values map[string]tracker.TaskInfo
	calls  int
}

func (f fakeLauncherTracker) Name() string { return "fake" }

func (f fakeLauncherTracker) Resolve(branch string) tracker.TaskInfo {
	return f.values[branch]
}

type countingLauncherTracker struct {
	values map[string]tracker.TaskInfo
	calls  *int
}

func (f countingLauncherTracker) Name() string { return "fake" }

func (f countingLauncherTracker) Resolve(branch string) tracker.TaskInfo {
	if f.calls != nil {
		(*f.calls)++
	}
	return f.values[branch]
}

func TestLauncherTaskResolverResolveRepoTaskNameCachesByPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := tracker.NewTaskCache("/tmp/root", time.Hour)
	resolver := newLauncherTaskResolver(fakeLauncherTracker{
		values: map[string]tracker.TaskInfo{
			"feature/123": {Name: "Task 123"},
		},
	}, cache)

	got := resolver.resolveRepoTaskName("/tmp/repo", "feature/123")
	if got != "Task 123" {
		t.Fatalf("resolveRepoTaskName() = %q, want %q", got, "Task 123")
	}

	entry, ok := cache.GetEntry("/tmp/repo")
	if !ok {
		t.Fatalf("expected task cache entry for repo path")
	}
	if entry.Branch != "feature/123" {
		t.Fatalf("cached branch = %q, want %q", entry.Branch, "feature/123")
	}
	if entry.Info.Name != "Task 123" {
		t.Fatalf("cached task name = %q, want %q", entry.Info.Name, "Task 123")
	}
}

func TestLoadDirectoryEntriesDefersUncachedRepoTaskLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "HEAD"), []byte("ref: refs/heads/feature/123\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	var calls int
	resolver := newLauncherTaskResolver(countingLauncherTracker{
		values: map[string]tracker.TaskInfo{
			"feature/123": {Name: "Task 123"},
		},
		calls: &calls,
	}, tracker.NewTaskCache(root, time.Hour))

	entries, pendingPaths, err := loadDirectoryEntries(root, "", launcherActiveTargets{}, map[string]bool{}, resolver, true)
	if err != nil {
		t.Fatalf("loadDirectoryEntries() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("tracker Resolve() calls = %d, want 0", calls)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if got := entries[1].taskName; got != "" {
		t.Fatalf("repo taskName = %q, want empty cached-first render", got)
	}
	wantPending := []string{repoPath}
	if !reflect.DeepEqual(pendingPaths, wantPending) {
		t.Fatalf("pendingPaths = %#v, want %#v", pendingPaths, wantPending)
	}
}

func TestLoadDirectoryEntriesSkipsTaskLookupInDirectoriesTab(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "HEAD"), []byte("ref: refs/heads/feature/123\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	var calls int
	resolver := newLauncherTaskResolver(countingLauncherTracker{
		values: map[string]tracker.TaskInfo{
			"feature/123": {Name: "Task 123"},
		},
		calls: &calls,
	}, tracker.NewTaskCache(root, time.Hour))

	entries, pendingPaths, err := loadDirectoryEntries(root, "", launcherActiveTargets{}, map[string]bool{}, resolver, false)
	if err != nil {
		t.Fatalf("loadDirectoryEntries() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("tracker Resolve() calls = %d, want 0", calls)
	}
	if len(pendingPaths) != 0 {
		t.Fatalf("pendingPaths = %#v, want none", pendingPaths)
	}
	if got := entries[1].kind; got != "repo" {
		t.Fatalf("entry kind = %q, want %q", got, "repo")
	}
	if got := entries[1].taskName; got != "" {
		t.Fatalf("repo taskName = %q, want empty", got)
	}
}

func TestLauncherFavoriteKindForPathUsesRepoMode(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	if got := launcherFavoriteKindForPath(repoPath); got != "repo" {
		t.Fatalf("launcherFavoriteKindForPath(repo) = %q, want %q", got, "repo")
	}

	dirPath := t.TempDir()
	if got := launcherFavoriteKindForPath(dirPath); got != "dir" {
		t.Fatalf("launcherFavoriteKindForPath(dir) = %q, want %q", got, "dir")
	}
}

func TestLauncherEntriesLoadedRestoresPendingSelectionPath(t *testing.T) {
	m := LauncherModel{
		cursor:               0,
		pendingSelectionPath: "/tmp/root/child",
	}

	updated, _ := m.Update(launcherEntriesLoadedMsg{
		version: 0,
		entries: []launcherEntry{
			{label: "other", path: "/tmp/root/other"},
			{label: "child", path: "/tmp/root/child"},
		},
	})

	got := updated.(LauncherModel)
	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.cursor)
	}
	if got.pendingSelectionPath != "" {
		t.Fatalf("pendingSelectionPath = %q, want empty", got.pendingSelectionPath)
	}
}

func TestHandleBackDirectoriesStoresSelectionPath(t *testing.T) {
	m := LauncherModel{
		tab:         launcherTabDirectories,
		currentPath: "/tmp/root/child",
		cursor:      3,
	}

	updated, _ := m.handleBack()
	if updated.currentPath != "/tmp/root" {
		t.Fatalf("currentPath = %q, want %q", updated.currentPath, "/tmp/root")
	}
	if updated.pendingSelectionPath != "/tmp/root/child" {
		t.Fatalf("pendingSelectionPath = %q, want %q", updated.pendingSelectionPath, "/tmp/root/child")
	}
}

func TestHandleBackRepoStoresSelectionPath(t *testing.T) {
	m := LauncherModel{
		tab:         launcherTabDirectories,
		currentPath: "/tmp/root",
		repoPath:    "/tmp/root/repo",
	}

	updated, _ := m.handleBack()
	if updated.repoPath != "" {
		t.Fatalf("repoPath = %q, want empty", updated.repoPath)
	}
	if updated.pendingSelectionPath != "/tmp/root/repo" {
		t.Fatalf("pendingSelectionPath = %q, want %q", updated.pendingSelectionPath, "/tmp/root/repo")
	}
}

func TestLauncherTaskNameForEntryResolvesTaskFilterOnDemand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "HEAD"), []byte("ref: refs/heads/feature/123\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	var calls int
	resolver := newLauncherTaskResolver(countingLauncherTracker{
		values: map[string]tracker.TaskInfo{
			"feature/123": {Name: "Task 123"},
		},
		calls: &calls,
	}, tracker.NewTaskCache(repoPath, time.Hour))

	taskName, needsResolve, include := launcherTaskNameForEntry("task 123", filepath.Base(repoPath), repoPath, resolver)
	if !include {
		t.Fatalf("include = false, want true")
	}
	if needsResolve {
		t.Fatalf("needsResolve = true, want false after on-demand resolve")
	}
	if taskName != "Task 123" {
		t.Fatalf("taskName = %q, want %q", taskName, "Task 123")
	}
	if calls != 1 {
		t.Fatalf("tracker Resolve() calls = %d, want 1", calls)
	}
}
