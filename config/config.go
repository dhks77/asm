package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DefaultPath        string       `toml:"default_path"`
	TaskIDPattern      string       `toml:"task_id_pattern"`
	GitRefreshInterval int          `toml:"git_refresh_interval"`
	ClaudePath         string       `toml:"claude_path"`
	ClaudeArgs         []string     `toml:"claude_args"`
	Dooray             DoorayConfig `toml:"dooray"`
}

type DoorayConfig struct {
	Enabled bool   `toml:"enabled"`
	Token   string `toml:"token"`
	APIURL  string `toml:"api_url"`
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
		TaskIDPattern:      `(\d{4,})`,
		GitRefreshInterval: 5,
	}
}

func configPath() string {
	return filepath.Join(homeDir(), ".config", "csm", "config.toml")
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

func (c *Config) ResolveClaudePath() string {
	if c.ClaudePath != "" {
		return c.ClaudePath
	}
	return "claude"
}
