package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ClaudeSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	StartedAt int64  `json:"startedAt"`
	Kind      string `json:"kind"`
}

func (s ClaudeSession) StartedAtTime() time.Time {
	return time.UnixMilli(s.StartedAt)
}

// FindSessions scans ~/.claude/sessions/ and returns sessions matching the given cwd.
func FindSessions(cwd string) ([]ClaudeSession, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	sessDir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil, err
	}

	var sessions []ClaudeSession
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		var sess ClaudeSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		if sess.CWD == cwd && sess.SessionID != "" {
			sessions = append(sessions, sess)
		}
	}

	// Sort by start time, most recent first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt > sessions[j].StartedAt
	})

	return sessions, nil
}
