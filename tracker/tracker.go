package tracker

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

// Peeker exposes a non-blocking branch-cache lookup. Callers use this to
// batch initial UI renders before any async tracker resolves fire.
type Peeker interface {
	Peek(branch string) (TaskInfo, bool)
}

// CachedTracker wraps a Tracker with the shared TaskCache branch index.
type CachedTracker struct {
	inner Tracker
	cache *TaskCache
}

func NewCachedTracker(inner Tracker, cache *TaskCache) *CachedTracker {
	return &CachedTracker{inner: inner, cache: cache}
}

func (c *CachedTracker) Name() string   { return c.inner.Name() }
func (c *CachedTracker) Inner() Tracker { return c.inner }

func (c *CachedTracker) Resolve(branch string) TaskInfo {
	if info, ok := c.Peek(branch); ok {
		return info
	}
	value := c.inner.Resolve(branch)
	if c.cache != nil && value.Name != "" {
		c.cache.StoreBranch(branch, value)
	}
	return value
}

func (c *CachedTracker) Peek(branch string) (TaskInfo, bool) {
	if c.cache == nil {
		return TaskInfo{}, false
	}
	return c.cache.Peek(branch)
}
