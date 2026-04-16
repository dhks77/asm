package favorites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nhn/asm/config"
)

type Kind string

const (
	KindDir  Kind = "dir"
	KindRepo Kind = "repo"
)

type Entry struct {
	Kind      Kind   `json:"kind"`
	Path      string `json:"path"`
	UpdatedAt int64  `json:"updated_at"`
}

func filePath() string {
	return filepath.Join(config.UserConfigDir(), "favorites.json")
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

func Toggle(kind Kind, path string) (bool, error) {
	cleanPath := filepath.Clean(path)
	entries, err := Load()
	if err != nil {
		return false, err
	}

	for i := range entries {
		if entries[i].Kind == kind && entries[i].Path == cleanPath {
			entries = append(entries[:i], entries[i+1:]...)
			return false, save(entries)
		}
	}

	entries = append(entries, Entry{
		Kind:      kind,
		Path:      cleanPath,
		UpdatedAt: time.Now().Unix(),
	})
	sortEntries(entries)
	return true, save(entries)
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].UpdatedAt == entries[j].UpdatedAt {
			if entries[i].Kind == entries[j].Kind {
				return entries[i].Path < entries[j].Path
			}
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].UpdatedAt > entries[j].UpdatedAt
	})
}

func save(entries []Entry) error {
	if len(entries) == 0 {
		err := os.Remove(filePath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
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
