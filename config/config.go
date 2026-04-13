package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Scope represents the configuration scope.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// ProviderConfig holds command/args overrides for built-in providers.
type ProviderConfig struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

type Config struct {
	DefaultPath          string                       `toml:"default_path"`
	GitRefreshInterval   int                          `toml:"git_refresh_interval"`
	DesktopNotifications *bool                        `toml:"desktop_notifications"`
	DefaultProvider      string                       `toml:"default_provider"`
	DefaultTracker       string                       `toml:"default_tracker"`
	Providers            map[string]ProviderConfig    `toml:"providers"`
	Trackers             map[string]map[string]string `toml:"trackers"`
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return home
}

func DefaultConfig() *Config {
	return &Config{
		GitRefreshInterval: 5,
	}
}

// UserConfigDir returns the user-level config directory.
func UserConfigDir() string {
	return filepath.Join(homeDir(), ".asm")
}

// UserConfigPath returns the user-level config path.
// Checks for existing config in this order: ~/.asm/, ~/.config/asm/, ~/.config/csm/.
// If none exist, returns the primary ~/.asm/ path.
func UserConfigPath() string {
	primary := filepath.Join(UserConfigDir(), "config.toml")
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	// Fallback to legacy locations
	for _, legacy := range []string{
		filepath.Join(homeDir(), ".config", "asm", "config.toml"),
		filepath.Join(homeDir(), ".config", "csm", "config.toml"),
	} {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return primary
}

// ProjectConfigPath returns the project-level config path.
func ProjectConfigPath(rootPath string) string {
	return filepath.Join(rootPath, ".asm", "config.toml")
}

// ScopePath returns the config file path for the given scope.
func ScopePath(scope Scope, rootPath string) string {
	if scope == ScopeProject {
		return ProjectConfigPath(rootPath)
	}
	return UserConfigPath()
}

// Load returns the merged config (user + project overlay) for runtime use.
// If rootPath is empty, only user config is loaded.
func Load() (*Config, error) {
	return LoadMerged("")
}

// LoadMerged loads user config merged with project config overlay.
func LoadMerged(rootPath string) (*Config, error) {
	base, err := LoadScope(ScopeUser, rootPath)
	if err != nil {
		return nil, err
	}
	if rootPath == "" {
		return base, nil
	}
	overlay, err := LoadScope(ScopeProject, rootPath)
	if err != nil {
		return base, nil
	}
	merge(base, overlay)
	return base, nil
}

// LoadScope loads a single-scope config (raw, unmerged).
func LoadScope(scope Scope, rootPath string) (*Config, error) {
	cfg := DefaultConfig()

	var path string
	if scope == ScopeProject {
		path = ProjectConfigPath(rootPath)
	} else {
		path = UserConfigPath()
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// merge applies overlay on top of base, in-place. Non-zero overlay values override base.
func merge(base, overlay *Config) {
	if overlay.DefaultPath != "" {
		base.DefaultPath = overlay.DefaultPath
	}
	if overlay.GitRefreshInterval != 0 {
		base.GitRefreshInterval = overlay.GitRefreshInterval
	}
	if overlay.DesktopNotifications != nil {
		base.DesktopNotifications = overlay.DesktopNotifications
	}
	if overlay.DefaultProvider != "" {
		base.DefaultProvider = overlay.DefaultProvider
	}
	if overlay.DefaultTracker != "" {
		base.DefaultTracker = overlay.DefaultTracker
	}

	// Merge Providers (wholesale per key)
	if len(overlay.Providers) > 0 {
		if base.Providers == nil {
			base.Providers = make(map[string]ProviderConfig)
		}
		for k, v := range overlay.Providers {
			base.Providers[k] = v
		}
	}

	// Merge Trackers (deep merge per field within each tracker)
	if len(overlay.Trackers) > 0 {
		if base.Trackers == nil {
			base.Trackers = make(map[string]map[string]string)
		}
		for name, fields := range overlay.Trackers {
			if base.Trackers[name] == nil {
				base.Trackers[name] = make(map[string]string)
			}
			for k, v := range fields {
				if v != "" {
					base.Trackers[name][k] = v
				}
			}
		}
	}
}

// PluginDir returns the directory for provider plugins.
func PluginDir() string {
	primary := filepath.Join(UserConfigDir(), "plugins")
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	legacy := filepath.Join(homeDir(), ".config", "asm", "plugins")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return primary
}

// TrackerDir returns the directory for tracker plugins.
func TrackerDir() string {
	primary := filepath.Join(UserConfigDir(), "trackers")
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	legacy := filepath.Join(homeDir(), ".config", "asm", "trackers")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return primary
}

// Save writes the config to the user config path (backward compat).
func Save(cfg *Config) error {
	return SaveScope(cfg, ScopeUser, "")
}

// SaveScope writes the config to the given scope's path.
// User-scope saves always go to ~/.asm/ (primary location) regardless of legacy paths.
func SaveScope(cfg *Config, scope Scope, rootPath string) error {
	var path string
	if scope == ScopeProject {
		if rootPath == "" {
			return os.ErrInvalid
		}
		path = ProjectConfigPath(rootPath)
	} else {
		path = filepath.Join(UserConfigDir(), "config.toml")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

func (c *Config) IsDesktopNotificationsEnabled() bool {
	if c.DesktopNotifications == nil {
		return true // default: enabled
	}
	return *c.DesktopNotifications
}
