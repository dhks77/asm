package config

import (
	"os"
	"path/filepath"
	"strings"

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

// IDEConfig overrides a built-in IDE launcher or registers a new one.
type IDEConfig struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// WorktreeTemplateConfig controls post-create file templating.
// Files under {projectRoot}/.asm/templates/{repo}/ are copied into a new
// worktree at the same relative paths. OnConflict decides what happens when a
// destination file already exists; valid values are "skip" (default) and
// "overwrite".
type WorktreeTemplateConfig struct {
	OnConflict string `toml:"on_conflict"`
}

type Config struct {
	DefaultPath          string                       `toml:"default_path"`
	DesktopNotifications *bool                        `toml:"desktop_notifications"`
	AutoZoom             *bool                        `toml:"auto_zoom"`
	PickerWidth          int                          `toml:"picker_width"` // picker pane width in %
	DefaultProvider      string                       `toml:"default_provider"`
	DefaultTracker       string                       `toml:"default_tracker"`
	Providers            map[string]ProviderConfig    `toml:"providers"`
	Trackers             map[string]map[string]string `toml:"trackers"`
	IDEs                 map[string]IDEConfig         `toml:"ides"`
	// DefaultIDE, if set, skips the IDE selector and opens directly.
	DefaultIDE       string                 `toml:"default_ide"`
	WorktreeTemplate WorktreeTemplateConfig `toml:"worktree_template"`
	// WorktreeBasePath is the directory where new worktrees are created in
	// repo mode when there are no existing linked worktrees to inherit the
	// layout from. Empty = fall back to the main repo's parent directory
	// (standard git sibling convention). Leading `~` is expanded to the
	// user's home directory when resolved via GetWorktreeBasePath.
	WorktreeBasePath string `toml:"worktree_base_path"`
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return home
}

func DefaultConfig() *Config {
	return &Config{}
}

// UserConfigDir returns the user-level config directory.
func UserConfigDir() string {
	return filepath.Join(homeDir(), ".asm")
}

// UserConfigPath returns the user-level config path.
func UserConfigPath() string {
	return filepath.Join(UserConfigDir(), "config.toml")
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
	if overlay.DesktopNotifications != nil {
		base.DesktopNotifications = overlay.DesktopNotifications
	}
	if overlay.AutoZoom != nil {
		base.AutoZoom = overlay.AutoZoom
	}
	if overlay.PickerWidth != 0 {
		base.PickerWidth = overlay.PickerWidth
	}
	if overlay.DefaultProvider != "" {
		base.DefaultProvider = overlay.DefaultProvider
	}
	if overlay.DefaultTracker != "" {
		base.DefaultTracker = overlay.DefaultTracker
	}
	if overlay.DefaultIDE != "" {
		base.DefaultIDE = overlay.DefaultIDE
	}
	if overlay.WorktreeTemplate.OnConflict != "" {
		base.WorktreeTemplate.OnConflict = overlay.WorktreeTemplate.OnConflict
	}
	if overlay.WorktreeBasePath != "" {
		base.WorktreeBasePath = overlay.WorktreeBasePath
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

	// Merge IDEs (wholesale per key)
	if len(overlay.IDEs) > 0 {
		if base.IDEs == nil {
			base.IDEs = make(map[string]IDEConfig)
		}
		for k, v := range overlay.IDEs {
			base.IDEs[k] = v
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
	return filepath.Join(UserConfigDir(), "plugins")
}

// TrackerDir returns the directory for tracker plugins.
func TrackerDir() string {
	return filepath.Join(UserConfigDir(), "trackers")
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

func (c *Config) IsAutoZoomEnabled() bool {
	if c.AutoZoom == nil {
		return true // default: enabled
	}
	return *c.AutoZoom
}

// TemplateConflictPolicy returns the configured conflict policy for worktree
// template copying. Defaults to "skip".
func (c *Config) TemplateConflictPolicy() string {
	if c.WorktreeTemplate.OnConflict == "overwrite" {
		return "overwrite"
	}
	return "skip"
}

// GetWorktreeBasePath returns WorktreeBasePath with a leading `~` expanded to
// the user's home directory. Returns "" when unset so callers can fall
// through to their default (typically the main repo's parent directory).
func (c *Config) GetWorktreeBasePath() string {
	p := strings.TrimSpace(c.WorktreeBasePath)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// GetPickerWidth returns the picker pane width in percent, clamped to a
// sensible range. Defaults to 22 when unset.
func (c *Config) GetPickerWidth() int {
	w := c.PickerWidth
	if w == 0 {
		w = 22
	}
	if w < 10 {
		w = 10
	}
	if w > 50 {
		w = 50
	}
	return w
}
