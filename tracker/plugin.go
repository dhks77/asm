package tracker

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type pluginInfo struct {
	Name string `json:"name"`
}

type resolveResponse struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// PluginTracker implements Tracker by calling an external executable.
//
// Protocol:
//
//	<tracker> resolve <branch>  → {"name": "Task subject"}
type PluginTracker struct {
	path string
	name string
}

// LoadTracker loads a tracker plugin from the given executable path.
func LoadTracker(path string) (*PluginTracker, error) {
	name := filepath.Base(path)
	return &PluginTracker{path: path, name: name}, nil
}

func (t *PluginTracker) Name() string       { return t.name }
func (t *PluginTracker) PluginPath() string { return t.path }

func (t *PluginTracker) Resolve(branch string) TaskInfo {
	const maxRetries = 2
	for attempt := 0; attempt < maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, t.path, "resolve", branch).Output()
		cancel()
		if err != nil {
			continue
		}

		var resp resolveResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			continue
		}
		if resp.Name != "" {
			return TaskInfo{Name: resp.Name, URL: resp.URL}
		}
	}
	return TaskInfo{}
}

// ListNames returns the names of all tracker plugins in the directory.
func ListNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip config files (e.g., dooray.conf)
		if filepath.Ext(name) != "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// LoadFromDir loads a tracker plugin from the directory.
// If preferred is non-empty, loads that specific tracker. Otherwise loads the first one found.
// Returns nil if no trackers are found.
func LoadFromDir(dir string, preferred string) Tracker {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// Try preferred tracker first
	if preferred != "" {
		pluginPath := filepath.Join(dir, preferred)
		if t, err := LoadTracker(pluginPath); err == nil {
			return NewCachedTracker(t, time.Hour)
		}
	}

	// Fall back to first available
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != "" {
			continue
		}
		pluginPath := filepath.Join(dir, entry.Name())
		t, err := LoadTracker(pluginPath)
		if err != nil {
			continue
		}
		return NewCachedTracker(t, time.Hour)
	}
	return nil
}
