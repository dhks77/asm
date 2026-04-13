package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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

func configPath() string {
	asmPath := filepath.Join(homeDir(), ".config", "asm", "config.toml")
	if _, err := os.Stat(asmPath); err == nil {
		return asmPath
	}
	csmPath := filepath.Join(homeDir(), ".config", "csm", "config.toml")
	if _, err := os.Stat(csmPath); err == nil {
		return csmPath
	}
	return asmPath
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	path := configPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// PluginDir returns the directory for provider plugins.
func PluginDir() string {
	return filepath.Join(homeDir(), ".config", "asm", "plugins")
}

// TrackerDir returns the directory for tracker plugins.
func TrackerDir() string {
	return filepath.Join(homeDir(), ".config", "asm", "trackers")
}

// Save writes the config to the asm config path.
func Save(cfg *Config) error {
	path := filepath.Join(homeDir(), ".config", "asm", "config.toml")
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

