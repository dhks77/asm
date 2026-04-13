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

// pathCacheMaxEntries caps the on-disk cache to prevent unbounded growth on
// long-lived projects. When exceeded, entries with the earliest ExpiresAt
// are evicted (equivalent to oldest-first since TTL is constant).
const pathCacheMaxEntries = 500

// PathCache is a persistent task-info cache keyed by worktree path. It is
// scoped per root path (one file per rootPath) so the picker can hydrate
// taskInfos synchronously on startup and render every task name on the
// first frame, instead of letting them trickle in one tea.Msg at a time.
//
// Entries record the branch observed when the info was cached so a branch
// change in a worktree invalidates the stale name without user action.
type PathCache struct {
	path    string
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[string]PathEntry
}

// PathEntry is a single cached task-info record.
type PathEntry struct {
	Branch    string    `json:"branch"`
	Info      TaskInfo  `json:"info"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewPathCache returns a cache persisted under ~/.asm/cache/<hash>.json for
// the given rootPath. Read failures are silently tolerated — a missing or
// corrupt file just starts with an empty cache.
func NewPathCache(rootPath string, ttl time.Duration) *PathCache {
	c := &PathCache{
		path:    cacheFilePath(rootPath),
		ttl:     ttl,
		entries: make(map[string]PathEntry),
	}
	c.load()
	return c
}

// Get returns the cached TaskInfo for a worktree path if present and not
// expired. The caller is expected to cross-check the branch via GetEntry
// when git status becomes available.
func (c *PathCache) Get(path string) (TaskInfo, bool) {
	e, ok := c.GetEntry(path)
	if !ok {
		return TaskInfo{}, false
	}
	return e.Info, true
}

// GetEntry returns the raw cache entry (including branch) for validation.
func (c *PathCache) GetEntry(path string) (PathEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[path]
	if !ok {
		return PathEntry{}, false
	}
	if time.Now().After(e.ExpiresAt) {
		return PathEntry{}, false
	}
	return e, true
}

// Set stores task info for a worktree path under a specific branch and
// persists the cache file asynchronously.
func (c *PathCache) Set(path, branch string, info TaskInfo) {
	if info.Name == "" {
		return
	}
	c.mu.Lock()
	c.entries[path] = PathEntry{
		Branch:    branch,
		Info:      info,
		ExpiresAt: time.Now().Add(c.ttl),
	}
	c.evictLocked(pathCacheMaxEntries)
	c.mu.Unlock()
	go c.save()
}

// evictLocked drops expired entries, then trims to `max` by removing the
// entries with the earliest ExpiresAt. Caller must hold c.mu.
func (c *PathCache) evictLocked(max int) {
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

// Delete removes the cache entry for a path.
func (c *PathCache) Delete(path string) {
	c.mu.Lock()
	_, existed := c.entries[path]
	if existed {
		delete(c.entries, path)
	}
	c.mu.Unlock()
	if existed {
		go c.save()
	}
}

// All returns a snapshot of non-expired entries as path → TaskInfo.
func (c *PathCache) All() map[string]TaskInfo {
	out := make(map[string]TaskInfo)
	now := time.Now()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for p, e := range c.entries {
		if now.After(e.ExpiresAt) {
			continue
		}
		out[p] = e.Info
	}
	return out
}

func (c *PathCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var decoded map[string]PathEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		return
	}
	c.mu.Lock()
	// Drop expired entries up front so they don't get re-persisted later.
	now := time.Now()
	for p, e := range decoded {
		if now.After(e.ExpiresAt) {
			continue
		}
		c.entries[p] = e
	}
	c.evictLocked(pathCacheMaxEntries)
	c.mu.Unlock()
}

func (c *PathCache) save() {
	c.mu.RLock()
	snapshot := make(map[string]PathEntry, len(c.entries))
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

// cacheFilePath returns ~/.asm/cache/<sha1(rootPath)>.json. Hashing keeps
// the filename filesystem-safe and avoids collisions between similarly
// named roots.
func cacheFilePath(rootPath string) string {
	abs := rootPath
	if a, err := filepath.Abs(rootPath); err == nil {
		abs = a
	}
	sum := sha1.Sum([]byte(abs))
	name := "tasks-" + hex.EncodeToString(sum[:]) + ".json"
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".asm", "cache", name)
}
