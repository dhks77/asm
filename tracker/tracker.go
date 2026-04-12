package tracker

import (
	"sync"
	"time"
)

// TaskInfo holds resolved task information.
type TaskInfo struct {
	Name string
	URL  string
}

// Tracker resolves branch names to task info.
type Tracker interface {
	Name() string
	Resolve(branch string) TaskInfo
}

// CachedTracker wraps a Tracker with an in-memory TTL cache.
type CachedTracker struct {
	inner   Tracker
	entries map[string]cacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
}

type cacheEntry struct {
	value     TaskInfo
	expiresAt time.Time
}

func NewCachedTracker(inner Tracker, ttl time.Duration) *CachedTracker {
	return &CachedTracker{
		inner:   inner,
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

func (c *CachedTracker) Name() string   { return c.inner.Name() }
func (c *CachedTracker) Inner() Tracker { return c.inner }

func (c *CachedTracker) Resolve(branch string) TaskInfo {
	c.mu.RLock()
	if e, ok := c.entries[branch]; ok && time.Now().Before(e.expiresAt) {
		c.mu.RUnlock()
		return e.value
	}
	c.mu.RUnlock()

	value := c.inner.Resolve(branch)

	// Only cache non-empty results; empty results will be retried
	if value.Name != "" {
		c.mu.Lock()
		c.entries[branch] = cacheEntry{value: value, expiresAt: time.Now().Add(c.ttl)}
		c.mu.Unlock()
	}

	return value
}
