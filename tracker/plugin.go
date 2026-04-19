package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

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
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("%s is not executable", path)
	}
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

func (t *PluginTracker) ConfigValues() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, t.path, "config-get").Output()
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err := json.Unmarshal(out, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// ListNames returns the names of all tracker plugins in the directory.
func ListNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !trackerCandidate(entry) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// LoadFromDir loads a tracker plugin from the directory.
// If preferred is non-empty, loads that specific tracker. Otherwise loads the first one found.
// Returns nil if no trackers are found.
func LoadFromDir(dir string, preferred string) Tracker {
	// Try preferred tracker first
	if preferred != "" {
		pluginPath := filepath.Join(dir, preferred)
		if t, err := LoadTracker(pluginPath); err == nil {
			return t
		}
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// Fall back to first available
	for _, entry := range entries {
		if !trackerCandidate(entry) {
			continue
		}
		pluginPath := filepath.Join(dir, entry.Name())
		t, err := LoadTracker(pluginPath)
		if err != nil {
			continue
		}
		return t
	}
	return nil
}

func trackerCandidate(entry os.DirEntry) bool {
	if entry.IsDir() {
		return false
	}
	if len(entry.Name()) == 0 || entry.Name()[0] == '.' {
		return false
	}
	info, err := entry.Info()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.Mode()&0o111 != 0
}
