package recent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nhn/asm/config"
)

type Entry struct {
	Path       string `json:"path"`
	LastUsedAt int64  `json:"last_used_at"`
}

const maxEntries = 100

func filePath() string {
	return filepath.Join(config.UserConfigDir(), "recent_targets.json")
}

func Load() ([]Entry, error) {
	data, err := os.ReadFile(filePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	sortEntries(entries)
	return entries, nil
}

func Record(path string) error {
	cleanPath := filepath.Clean(path)
	entries, err := Load()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	found := false
	for i := range entries {
		if entries[i].Path == cleanPath {
			entries[i].LastUsedAt = now
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, Entry{Path: cleanPath, LastUsedAt: now})
	}
	sortEntries(entries)
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	if err := os.MkdirAll(config.UserConfigDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath(), data, 0o644)
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].LastUsedAt == entries[j].LastUsedAt {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].LastUsedAt > entries[j].LastUsedAt
	})
}
