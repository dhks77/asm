package ui

import (
	"path/filepath"
	"testing"
)

func TestAsyncStringQueueDedupesAndAdvancesSequentially(t *testing.T) {
	q := newAsyncStringQueue(filepath.Clean)
	q.Enqueue("/tmp/repo", "/tmp/repo/", "/tmp/other")

	first, ok := q.StartNext(nil)
	if !ok {
		t.Fatal("expected first item")
	}
	if first != "/tmp/repo" {
		t.Fatalf("first = %q, want %q", first, "/tmp/repo")
	}
	if !q.Contains("/tmp/repo") {
		t.Fatal("expected in-flight item to be tracked")
	}

	q.Finish(first)

	second, ok := q.StartNext(nil)
	if !ok {
		t.Fatal("expected second item")
	}
	if second != "/tmp/other" {
		t.Fatalf("second = %q, want %q", second, "/tmp/other")
	}
}

func TestAsyncStringQueueSkipDropsAlreadyResolvedItems(t *testing.T) {
	q := newAsyncStringQueue(worktreeBranchTaskKey)
	q.Enqueue("origin/feature/123", "feature/123", "main")

	next, ok := q.StartNext(func(branch string) bool {
		return worktreeBranchTaskKey(branch) == "feature/123"
	})
	if !ok {
		t.Fatal("expected next item")
	}
	if next != "main" {
		t.Fatalf("next = %q, want %q", next, "main")
	}
}

func TestAsyncStringQueueStartsUpToTenInFlight(t *testing.T) {
	q := newAsyncStringQueue(filepath.Clean)
	for i := 0; i < 12; i++ {
		q.Enqueue(filepath.Join("/tmp", "repo", string(rune('a'+i))))
	}

	started := q.StartAvailable(nil)

	if len(started) != trackerFetchConcurrency {
		t.Fatalf("started len = %d, want %d", len(started), trackerFetchConcurrency)
	}
	if q.InFlight() != trackerFetchConcurrency {
		t.Fatalf("in flight = %d, want %d", q.InFlight(), trackerFetchConcurrency)
	}
	if q.Queued() != 2 {
		t.Fatalf("queued = %d, want 2", q.Queued())
	}
}
