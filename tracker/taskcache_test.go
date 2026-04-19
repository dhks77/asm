package tracker

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

type fakeCachedInnerTracker struct {
	resolveCount int
	values       map[string]TaskInfo
}

func (f *fakeCachedInnerTracker) Name() string { return "fake" }

func (f *fakeCachedInnerTracker) Resolve(branch string) TaskInfo {
	f.resolveCount++
	return f.values[branch]
}

func TestTaskCacheSetStoresPathAndBranchIndexes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := NewTaskCache("/tmp/root", time.Hour)
	cache.Set("/tmp/repo", "feature/123", TaskInfo{Name: "Task 123"})

	entry, ok := cache.GetEntry("/tmp/repo")
	if !ok {
		t.Fatal("expected path cache entry")
	}
	if entry.Branch != "feature/123" {
		t.Fatalf("cached branch = %q, want %q", entry.Branch, "feature/123")
	}
	if entry.Info.Name != "Task 123" {
		t.Fatalf("cached task name = %q, want %q", entry.Info.Name, "Task 123")
	}

	info, ok := cache.Peek("feature/123")
	if !ok {
		t.Fatal("expected branch cache entry")
	}
	if info.Name != "Task 123" {
		t.Fatalf("branch cache task name = %q, want %q", info.Name, "Task 123")
	}
}

func TestCachedTrackerUsesSharedTaskCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := NewTaskCache("/tmp/root", time.Hour)
	inner := &fakeCachedInnerTracker{
		values: map[string]TaskInfo{
			"feature/123": {Name: "Task 123"},
		},
	}
	tracker := NewCachedTracker(inner, cache)

	if got := tracker.Resolve("feature/123"); got.Name != "Task 123" {
		t.Fatalf("first Resolve() = %#v, want task name %q", got, "Task 123")
	}
	if got := tracker.Resolve("feature/123"); got.Name != "Task 123" {
		t.Fatalf("second Resolve() = %#v, want task name %q", got, "Task 123")
	}
	if inner.resolveCount != 1 {
		t.Fatalf("inner resolve count = %d, want %d", inner.resolveCount, 1)
	}
}

func TestTaskCacheConcurrentSaveKeepsReadableSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := NewTaskCache("/tmp/root", time.Hour)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache.Set("/tmp/repo", "feature/123", TaskInfo{Name: "Task"})
			cache.StoreBranch("feature/"+time.Unix(int64(i), 0).UTC().Format("150405"), TaskInfo{Name: "Task"})
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(cache.path)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	var snapshot taskCacheSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("cache snapshot is not valid json: %v", err)
	}
	if len(snapshot.Paths) == 0 {
		t.Fatal("expected path entries in snapshot")
	}
}
