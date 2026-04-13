package tracker

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// branchCacheMaxEntries caps the on-disk branch cache. Matches the
// PathCache cap — both grow at similar rates (one entry per branch
// resolved via the worktree dialog or picker).
const branchCacheMaxEntries = 500

// TaskInfo holds resolved task information.
type TaskInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Tracker resolves branch names to task info.
type Tracker interface {
	Name() string
	Resolve(branch string) TaskInfo
}

// Peeker exposes a non-blocking cache lookup. Callers use this to batch UI
// renders: ask the cache what it already knows before dispatching async
// fetches. Implemented by CachedTracker.
type Peeker interface {
	Peek(branch string) (TaskInfo, bool)
}

// CachedTracker wraps a Tracker with a branch-keyed cache. When a store
// path is configured, non-empty entries are persisted to disk so results
// survive across process starts (important because dialogs like
// "Create Worktree" are run as separate `asm` child processes).
type CachedTracker struct {
	inner   Tracker
	entries map[string]cacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	path    string // persistence file; "" = in-memory only
}

type cacheEntry struct {
	Value     TaskInfo  `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewCachedTracker returns an in-memory-only cached tracker. Preserved for
// callers that don't want disk persistence.
func NewCachedTracker(inner Tracker, ttl time.Duration) *CachedTracker {
	return &CachedTracker{
		inner:   inner,
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

// NewCachedTrackerWithStore returns a cached tracker persisted to a file
// derived from rootPath. Hydrates from disk on construction; any load
// failure is silently ignored and results in an empty cache.
func NewCachedTrackerWithStore(inner Tracker, ttl time.Duration, rootPath string) *CachedTracker {
	c := &CachedTracker{
		inner:   inner,
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		path:    branchCachePath(rootPath),
	}
	c.load()
	return c
}

func (c *CachedTracker) Name() string   { return c.inner.Name() }
func (c *CachedTracker) Inner() Tracker { return c.inner }

func (c *CachedTracker) Resolve(branch string) TaskInfo {
	if info, ok := c.Peek(branch); ok {
		return info
	}

	value := c.inner.Resolve(branch)

	// Only cache non-empty results; empty results will be retried
	if value.Name != "" {
		c.mu.Lock()
		c.entries[branch] = cacheEntry{Value: value, ExpiresAt: time.Now().Add(c.ttl)}
		c.evictLocked(branchCacheMaxEntries)
		c.mu.Unlock()
		if c.path != "" {
			go c.save()
		}
	}

	return value
}

// evictLocked drops expired entries, then trims to `max` by removing the
// entries with the earliest ExpiresAt. Caller must hold c.mu.
func (c *CachedTracker) evictLocked(max int) {
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.ExpiresAt) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) <= max {
		return
	}
	type kv struct {
		key string
		exp time.Time
	}
	all := make([]kv, 0, len(c.entries))
	for k, e := range c.entries {
		all = append(all, kv{k, e.ExpiresAt})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].exp.Before(all[j].exp) })
	for i := 0; i < len(all)-max; i++ {
		delete(c.entries, all[i].key)
	}
}

// Peek returns a cached TaskInfo without consulting the underlying tracker.
// Used by UIs to batch the initial "already-known" list into a single
// render before any async fetches fire.
func (c *CachedTracker) Peek(branch string) (TaskInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[branch]
	if !ok || time.Now().After(e.ExpiresAt) {
		return TaskInfo{}, false
	}
	return e.Value, true
}

func (c *CachedTracker) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var decoded map[string]cacheEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	for k, v := range decoded {
		if now.After(v.ExpiresAt) {
			continue
		}
		c.entries[k] = v
	}
	c.evictLocked(branchCacheMaxEntries)
	c.mu.Unlock()
}

func (c *CachedTracker) save() {
	c.mu.RLock()
	snapshot := make(map[string]cacheEntry, len(c.entries))
	for k, v := range c.entries {
		snapshot[k] = v
	}
	c.mu.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, c.path)
}

// branchCachePath returns ~/.asm/cache/branches-<sha1(rootPath)>.json.
// Kept separate from the picker's path-keyed cache (tasks-<hash>.json).
func branchCachePath(rootPath string) string {
	abs := rootPath
	if a, err := filepath.Abs(rootPath); err == nil {
		abs = a
	}
	sum := sha1.Sum([]byte(abs))
	name := "branches-" + hex.EncodeToString(sum[:]) + ".json"
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".asm", "cache", name)
}
