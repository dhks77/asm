package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CacheEntry struct {
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Cache struct {
	entries map[string]CacheEntry
	mu      sync.RWMutex
	path    string
}

func NewCache(name string) *Cache {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	path := filepath.Join(home, ".asm", "cache", name+".json")

	c := &Cache{
		entries: make(map[string]CacheEntry),
		path:    path,
	}
	c.load()
	return c
}

func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		return "", false
	}
	return entry.Value, true
}

func (c *Cache) Set(key, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
	c.save()
}

func (c *Cache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &c.entries)
}

func (c *Cache) save() {
	_ = os.MkdirAll(filepath.Dir(c.path), 0755)
	data, err := json.Marshal(c.entries)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.path, data, 0644)
}
