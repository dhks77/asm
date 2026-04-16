package sessionstate

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nhn/asm/config"
)

type TargetSnapshot struct {
	Path     string `json:"path"`
	HasAI    bool   `json:"has_ai,omitempty"`
	HasTerm  bool   `json:"has_term,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type Snapshot struct {
	RootPath      string           `json:"root_path"`
	UpdatedAt     time.Time        `json:"updated_at"`
	FrontPath     string           `json:"front_path,omitempty"`
	FrontKind     string           `json:"front_kind,omitempty"`
	FocusedPane   string           `json:"focused_pane,omitempty"`
	WorkingZoomed bool             `json:"working_zoomed,omitempty"`
	Targets       []TargetSnapshot `json:"targets,omitempty"`
}

func (s *Snapshot) HasTargets() bool {
	return s != nil && len(s.Targets) > 0
}

var snapshotDir = func() string {
	return filepath.Join(config.UserConfigDir(), "state", "sessions")
}

func Save(rootPath string, snap Snapshot) error {
	if len(snap.Targets) == 0 {
		return Delete(rootPath)
	}

	cleanRoot := filepath.Clean(rootPath)
	snap.RootPath = cleanRoot
	snap.UpdatedAt = time.Now()

	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	path := snapshotPath(cleanRoot)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load(rootPath string) (*Snapshot, error) {
	data, err := os.ReadFile(snapshotPath(rootPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	if filepath.Clean(snap.RootPath) != filepath.Clean(rootPath) {
		return nil, nil
	}
	if len(snap.Targets) == 0 {
		return nil, nil
	}
	return &snap, nil
}

func Delete(rootPath string) error {
	err := os.Remove(snapshotPath(rootPath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func snapshotPath(rootPath string) string {
	cleanRoot := filepath.Clean(rootPath)
	base := filepath.Base(cleanRoot)
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	if sanitized == "" {
		sanitized = "root"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(cleanRoot))
	return filepath.Join(snapshotDir(), fmt.Sprintf("%s-%06x.json", sanitized, h.Sum32()&0xffffff))
}
