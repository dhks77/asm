package sessionstate

import (
	"encoding/json"
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
	SessionID     string           `json:"session_id"`
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

var lastSessionIDPath = func() string {
	return filepath.Join(config.UserConfigDir(), "state", "last-session-id")
}

func Save(sessionID string, snap Snapshot) error {
	if len(snap.Targets) == 0 {
		return Delete(sessionID)
	}

	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return os.ErrInvalid
	}
	snap.SessionID = cleanSessionID
	snap.UpdatedAt = time.Now()

	dir := snapshotDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	path := snapshotPath(cleanSessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load(sessionID string) (*Snapshot, error) {
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return nil, nil
	}

	data, err := os.ReadFile(snapshotPath(cleanSessionID))
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
	if snap.SessionID != "" && snap.SessionID != cleanSessionID {
		return nil, nil
	}
	if len(snap.Targets) == 0 {
		return nil, nil
	}
	return &snap, nil
}

func Delete(sessionID string) error {
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return nil
	}
	err := os.Remove(snapshotPath(cleanSessionID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func SaveLastSessionID(sessionID string) error {
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return os.ErrInvalid
	}
	path := lastSessionIDPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(cleanSessionID+"\n"), 0o644)
}

func LoadLastSessionID() (string, error) {
	data, err := os.ReadFile(lastSessionIDPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func DeleteLastSessionID() error {
	err := os.Remove(lastSessionIDPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func snapshotPath(sessionID string) string {
	return filepath.Join(snapshotDir(), strings.TrimSpace(sessionID)+".json")
}
