package ui

import (
	"testing"
	"time"

	"github.com/nhn/asm/tracker"
)

type fakeLauncherTracker struct {
	values map[string]tracker.TaskInfo
}

func (f fakeLauncherTracker) Name() string { return "fake" }

func (f fakeLauncherTracker) Resolve(branch string) tracker.TaskInfo {
	return f.values[branch]
}

func TestLauncherTaskResolverResolveRepoTaskNameCachesByPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := tracker.NewPathCache("/tmp/root", time.Hour)
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
