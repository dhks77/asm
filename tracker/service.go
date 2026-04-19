package tracker

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nhn/asm/config"
	"github.com/nhn/asm/plugincfg"
)

// DefaultCacheTTL is the persisted task-cache lifetime used across the UI.
const DefaultCacheTTL = 7 * 24 * time.Hour

var (
	defaultServiceMu sync.Mutex
	defaultService   *Service
)

// DefaultService returns the shared tracker service used by the app.
func DefaultService() *Service {
	defaultServiceMu.Lock()
	defer defaultServiceMu.Unlock()
	if defaultService == nil {
		defaultService = NewService(DefaultCacheTTL)
	}
	return defaultService
}

// Service resolves trackers per target path and partitions task caches by the
// active tracker identity/configuration.
type Service struct {
	ttl time.Duration

	mu     sync.Mutex
	caches map[string]*TaskCache
}

// NewService creates a tracker service with the given cache TTL.
func NewService(ttl time.Duration) *Service {
	return &Service{
		ttl:    ttl,
		caches: make(map[string]*TaskCache),
	}
}

// Resolve uses the tracker configured for path to resolve a task title for branch.
func (s *Service) Resolve(path, branch string) TaskInfo {
	if strings.TrimSpace(branch) == "" {
		return TaskInfo{}
	}
	tr, cache := s.trackerForPath(path)
	if tr == nil || cache == nil {
		return TaskInfo{}
	}
	if info, ok := cache.Peek(branch); ok {
		return info
	}
	info := tr.Resolve(branch)
	if info.Name != "" {
		cache.StoreBranch(branch, info)
	}
	return info
}

// Peek returns cached task info for path+branch without hitting the tracker.
func (s *Service) Peek(path, branch string) (TaskInfo, bool) {
	if strings.TrimSpace(branch) == "" {
		return TaskInfo{}, false
	}
	_, cache := s.trackerForPath(path)
	if cache == nil {
		return TaskInfo{}, false
	}
	return cache.Peek(branch)
}

// GetEntry returns the cached path entry for the tracker configured at path.
func (s *Service) GetEntry(path string) (PathEntry, bool) {
	cleanPath := filepath.Clean(path)
	_, cache := s.trackerForPath(cleanPath)
	if cache == nil {
		return PathEntry{}, false
	}
	return cache.GetEntry(cleanPath)
}

// Set stores path+branch task info in the cache partition active for path.
func (s *Service) Set(path, branch string, info TaskInfo) {
	cleanPath := filepath.Clean(path)
	_, cache := s.trackerForPath(cleanPath)
	if cache == nil {
		return
	}
	cache.Set(cleanPath, branch, info)
}

// Delete removes the cached path binding for the tracker partition active at path.
func (s *Service) Delete(path string) {
	cleanPath := filepath.Clean(path)
	_, cache := s.trackerForPath(cleanPath)
	if cache == nil {
		return
	}
	cache.Delete(cleanPath)
}

// Names returns the available tracker names in UI order.
func (s *Service) Names() []string {
	names := append([]string(nil), BuiltinNames()...)
	names = append(names, ListNames(config.TrackerDir())...)
	return names
}

// Entries returns configurable tracker entries (built-ins and plugins).
func (s *Service) Entries(_ string) []plugincfg.Entry {
	entries := append([]plugincfg.Entry(nil), BuiltinEntries()...)
	for _, name := range ListNames(config.TrackerDir()) {
		entries = append(entries, plugincfg.Entry{
			Name:     name,
			Category: "tracker",
			Path:     filepath.Join(config.TrackerDir(), name),
		})
	}
	return entries
}

func (s *Service) trackerForPath(path string) (Tracker, *TaskCache) {
	cleanPath := filepath.Clean(path)
	cfg, err := config.LoadMerged(cleanPath)
	if err != nil || cfg == nil {
		return nil, nil
	}
	spec := buildTrackerSpec(cfg)
	if spec.tracker == nil {
		return nil, nil
	}
	projectRoot := config.ProjectRoot(cleanPath)
	if strings.TrimSpace(projectRoot) == "" {
		projectRoot = cleanPath
	}
	cache := s.cache(projectRoot, spec.cacheKey)
	return spec.tracker, cache
}

func (s *Service) cache(rootPath, scopeKey string) *TaskCache {
	key := filepath.Clean(rootPath) + "\x00" + scopeKey
	s.mu.Lock()
	defer s.mu.Unlock()
	if cache, ok := s.caches[key]; ok {
		return cache
	}
	cache := NewScopedTaskCache(rootPath, scopeKey, s.ttl)
	s.caches[key] = cache
	return cache
}

type trackerSpec struct {
	tracker  Tracker
	cacheKey string
}

func buildTrackerSpec(cfg *config.Config) trackerSpec {
	if cfg == nil {
		return trackerSpec{}
	}
	name := strings.TrimSpace(cfg.DefaultTracker)
	if name == "" {
		return trackerSpec{}
	}
	if IsBuiltin(name) {
		tr := buildBuiltin(name, cfg.Trackers[name])
		if tr == nil {
			return trackerSpec{}
		}
		return trackerSpec{
			tracker:  tr,
			cacheKey: builtinCacheKey(name, cfg.Trackers[name]),
		}
	}
	t, err := LoadTracker(filepath.Join(config.TrackerDir(), name))
	if err != nil {
		return trackerSpec{}
	}
	return trackerSpec{
		tracker:  t,
		cacheKey: pluginCacheKey(t),
	}
}

func buildBuiltin(name string, fields map[string]string) Tracker {
	switch name {
	case "dooray":
		return NewDoorayTracker(doorayConfigFromFields(fields), nil)
	default:
		return nil
	}
}

func BuiltinEntries() []plugincfg.Entry {
	return []plugincfg.Entry{
		{
			Name:     "dooray",
			Category: "tracker",
			Source:   NewDoorayTracker(&DoorayConfig{}, nil),
		},
	}
}

func doorayConfigFromFields(fields map[string]string) *DoorayConfig {
	cfg := &DoorayConfig{}
	if fields == nil {
		return cfg
	}
	cfg.Token = fields["token"]
	cfg.ProjectID = fields["project_id"]
	cfg.APIBaseURL = fields["api_base_url"]
	cfg.WebURL = fields["web_url"]
	cfg.TaskPattern = fields["task_pattern"]
	return cfg
}

func builtinCacheKey(name string, fields map[string]string) string {
	return name + "\x00" + stableMapKey(fields)
}

func pluginCacheKey(t *PluginTracker) string {
	values, _ := t.ConfigValues()
	return t.Name() + "\x00" + filepath.Clean(t.PluginPath()) + "\x00" + stableMapKey(values)
}

func stableMapKey(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(keys))
	for _, key := range keys {
		ordered[key] = values[key]
	}
	data, err := json.Marshal(ordered)
	if err != nil {
		return ""
	}
	return string(data)
}
