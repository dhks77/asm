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

	"github.com/nhn/asm/platform"
)

// Both cache indexes are capped independently to prevent unbounded growth.
const (
	pathCacheMaxEntries   = 500
	branchCacheMaxEntries = 500
)

// TaskCache is the unified persistent task-info cache.
//
// It keeps two indexes in one store:
//   - path -> {branch, info}
//   - branch -> info
//
// The path index lets the picker and launcher hydrate task names
// synchronously on startup. The branch index avoids repeated tracker API
// calls when multiple paths point at the same branch or when a path switches
// to a branch we have already resolved before.
type TaskCache struct {
	path             string
	legacyBranchPath string
	ttl              time.Duration
	mu               sync.RWMutex
	saveMu           sync.Mutex
	pathEntries      map[string]PathEntry
	branchEntries    map[string]BranchEntry
}

// PathEntry is a cached task-info record for a specific target path.
type PathEntry struct {
	Branch    string    `json:"branch"`
	Info      TaskInfo  `json:"info"`
	ExpiresAt time.Time `json:"expires_at"`
}

// BranchEntry is a cached task-info record for a branch name.
type BranchEntry struct {
	Info      TaskInfo  `json:"info"`
	ExpiresAt time.Time `json:"expires_at"`
}

type taskCacheSnapshot struct {
	Paths    map[string]PathEntry   `json:"paths,omitempty"`
	Branches map[string]BranchEntry `json:"branches,omitempty"`
}

type legacyBranchEntry struct {
	Value     TaskInfo  `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewTaskCache returns a cache persisted under ~/.asm/cache/tasks-<hash>.json
// for the given rootPath. Legacy path/branch cache files are read on first
// load so older cached data keeps working after the unified-cache migration.
func NewTaskCache(rootPath string, ttl time.Duration) *TaskCache {
	return NewScopedTaskCache(rootPath, "", ttl)
}

// NewScopedTaskCache returns a cache partitioned by both rootPath and scopeKey.
// scopeKey is used when the same project can resolve tasks through different
// trackers or tracker configurations.
func NewScopedTaskCache(rootPath, scopeKey string, ttl time.Duration) *TaskCache {
	c := &TaskCache{
		path:             cacheFilePath(rootPath, scopeKey),
		legacyBranchPath: legacyBranchCachePath(rootPath),
		ttl:              ttl,
		pathEntries:      make(map[string]PathEntry),
		branchEntries:    make(map[string]BranchEntry),
	}
	c.load()
	return c
}

// Get returns the cached TaskInfo for a target path if present and not
// expired. Callers that need the cached branch should use GetEntry.
func (c *TaskCache) Get(path string) (TaskInfo, bool) {
	e, ok := c.GetEntry(path)
	if !ok {
		return TaskInfo{}, false
	}
	return e.Info, true
}

// GetEntry returns the raw cache entry for a target path.
func (c *TaskCache) GetEntry(path string) (PathEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.pathEntries[path]
	if !ok || time.Now().After(e.ExpiresAt) {
		return PathEntry{}, false
	}
	return e, true
}

// Peek returns cached task info for a branch if present and not expired.
func (c *TaskCache) Peek(branch string) (TaskInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.branchEntries[branch]
	if !ok || time.Now().After(e.ExpiresAt) {
		return TaskInfo{}, false
	}
	return e.Info, true
}

// StoreBranch stores task info under a branch key and persists asynchronously.
func (c *TaskCache) StoreBranch(branch string, info TaskInfo) {
	if branch == "" || info.Name == "" {
		return
	}
	c.mu.Lock()
	c.branchEntries[branch] = BranchEntry{
		Info:      info,
		ExpiresAt: time.Now().Add(c.ttl),
	}
	c.evictLocked()
	c.mu.Unlock()
	c.save()
}

// Set stores task info for a target path and mirrors it into the branch index
// when branch is non-empty.
func (c *TaskCache) Set(path, branch string, info TaskInfo) {
	if path == "" || info.Name == "" {
		return
	}
	exp := time.Now().Add(c.ttl)
	c.mu.Lock()
	c.pathEntries[path] = PathEntry{
		Branch:    branch,
		Info:      info,
		ExpiresAt: exp,
	}
	if branch != "" {
		c.branchEntries[branch] = BranchEntry{
			Info:      info,
			ExpiresAt: exp,
		}
	}
	c.evictLocked()
	c.mu.Unlock()
	c.save()
}

// Delete removes only the path binding. Branch cache entries remain available
// so switching a path away from a branch doesn't throw away the branch's known
// task info.
func (c *TaskCache) Delete(path string) {
	c.mu.Lock()
	_, existed := c.pathEntries[path]
	if existed {
		delete(c.pathEntries, path)
	}
	c.mu.Unlock()
	if existed {
		c.save()
	}
}

// All returns a snapshot of non-expired path entries as path -> TaskInfo.
func (c *TaskCache) All() map[string]TaskInfo {
	out := make(map[string]TaskInfo)
	now := time.Now()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for p, e := range c.pathEntries {
		if now.After(e.ExpiresAt) {
			continue
		}
		out[p] = e.Info
	}
	return out
}

func (c *TaskCache) evictLocked() {
	now := time.Now()
	for k, e := range c.pathEntries {
		if now.After(e.ExpiresAt) {
			delete(c.pathEntries, k)
		}
	}
	for k, e := range c.branchEntries {
		if now.After(e.ExpiresAt) {
			delete(c.branchEntries, k)
		}
	}
	trimOldestPathEntries(c.pathEntries, pathCacheMaxEntries)
	trimOldestBranchEntries(c.branchEntries, branchCacheMaxEntries)
}

func trimOldestPathEntries(entries map[string]PathEntry, max int) {
	if len(entries) <= max {
		return
	}
	type kv struct {
		key string
		exp time.Time
	}
	all := make([]kv, 0, len(entries))
	for k, e := range entries {
		all = append(all, kv{key: k, exp: e.ExpiresAt})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].exp.Before(all[j].exp) })
	for i := 0; i < len(all)-max; i++ {
		delete(entries, all[i].key)
	}
}

func trimOldestBranchEntries(entries map[string]BranchEntry, max int) {
	if len(entries) <= max {
		return
	}
	type kv struct {
		key string
		exp time.Time
	}
	all := make([]kv, 0, len(entries))
	for k, e := range entries {
		all = append(all, kv{key: k, exp: e.ExpiresAt})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].exp.Before(all[j].exp) })
	for i := 0; i < len(all)-max; i++ {
		delete(entries, all[i].key)
	}
}

func (c *TaskCache) load() {
	now := time.Now()

	if data, err := os.ReadFile(c.path); err == nil {
		paths, branches := decodeTaskCacheFile(data)
		c.mu.Lock()
		for path, entry := range paths {
			if now.After(entry.ExpiresAt) {
				continue
			}
			c.pathEntries[path] = entry
			if entry.Branch != "" {
				if existing, ok := c.branchEntries[entry.Branch]; !ok || existing.ExpiresAt.Before(entry.ExpiresAt) {
					c.branchEntries[entry.Branch] = BranchEntry{
						Info:      entry.Info,
						ExpiresAt: entry.ExpiresAt,
					}
				}
			}
		}
		for branch, entry := range branches {
			if now.After(entry.ExpiresAt) {
				continue
			}
			c.branchEntries[branch] = entry
		}
		c.mu.Unlock()
	}

	if data, err := os.ReadFile(c.legacyBranchPath); err == nil {
		branches := decodeLegacyBranchCacheFile(data)
		c.mu.Lock()
		for branch, entry := range branches {
			if now.After(entry.ExpiresAt) {
				continue
			}
			if existing, ok := c.branchEntries[branch]; !ok || existing.ExpiresAt.Before(entry.ExpiresAt) {
				c.branchEntries[branch] = entry
			}
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.evictLocked()
	c.mu.Unlock()
}

func decodeTaskCacheFile(data []byte) (map[string]PathEntry, map[string]BranchEntry) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil
	}
	if _, hasPaths := raw["paths"]; hasPaths || raw["branches"] != nil {
		var snap taskCacheSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, nil
		}
		return snap.Paths, snap.Branches
	}

	var legacyPaths map[string]PathEntry
	if err := json.Unmarshal(data, &legacyPaths); err != nil {
		return nil, nil
	}
	return legacyPaths, nil
}

func decodeLegacyBranchCacheFile(data []byte) map[string]BranchEntry {
	var legacy map[string]legacyBranchEntry
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil
	}
	out := make(map[string]BranchEntry, len(legacy))
	for branch, entry := range legacy {
		out[branch] = BranchEntry{
			Info:      entry.Value,
			ExpiresAt: entry.ExpiresAt,
		}
	}
	return out
}

func (c *TaskCache) save() {
	c.mu.RLock()
	snapshot := taskCacheSnapshot{
		Paths:    make(map[string]PathEntry, len(c.pathEntries)),
		Branches: make(map[string]BranchEntry, len(c.branchEntries)),
	}
	for k, v := range c.pathEntries {
		snapshot.Paths[k] = v
	}
	for k, v := range c.branchEntries {
		snapshot.Branches[k] = v
	}
	c.mu.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return
	}
	c.saveMu.Lock()
	defer c.saveMu.Unlock()
	tmpFile, err := os.CreateTemp(filepath.Dir(c.path), filepath.Base(c.path)+".*.tmp")
	if err != nil {
		return
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
	}
}

// cacheFilePath returns ~/.asm/cache/tasks-<sha1(rootPath+scopeKey)>.json.
func cacheFilePath(rootPath, scopeKey string) string {
	abs := rootPath
	if a, err := filepath.Abs(rootPath); err == nil {
		abs = a
	}
	sum := sha1.Sum([]byte(abs + "\x00" + scopeKey))
	name := "tasks-" + hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(platform.Current().UserConfigDir(), "cache", name)
}

// legacyBranchCachePath matches the pre-unification branch-cache filename so
// old persisted branch data can be imported on first load.
func legacyBranchCachePath(rootPath string) string {
	abs := rootPath
	if a, err := filepath.Abs(rootPath); err == nil {
		abs = a
	}
	sum := sha1.Sum([]byte(abs))
	name := "branches-" + hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(platform.Current().UserConfigDir(), "cache", name)
}
