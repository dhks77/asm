package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/plugincfg"
	"github.com/nhn/asm/provider"
	"github.com/nhn/asm/tracker"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/ui"
)

func main() {
	pathFlag := flag.String("path", "", "Root directory containing directories")
	pickerMode := flag.Bool("picker", false, "Run in picker mode (inside tmux picking panel)")
	settingsMode := flag.Bool("settings", false, "Run plugin settings editor")
	deleteMode := flag.String("delete", "", "Run delete confirmation (directory name)")
	deleteTaskName := flag.String("delete-task", "", "Task name to display in delete confirmation")
	deleteDirty := flag.Bool("delete-dirty", false, "Directory has uncommitted changes")
	deleteWorktree := flag.Bool("delete-worktree", false, "Directory is a git worktree")
	worktreeCreate := flag.Bool("worktree-create", false, "Run worktree creation dialog")
	worktreeDir := flag.String("worktree-dir", "", "Directory path for worktree operations")
	providerSelect := flag.Bool("provider-select", false, "Run provider selection dialog")
	flag.Parse()

	// Load user config first to get DefaultPath
	userCfg, err := config.LoadScope(config.ScopeUser, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	cfg := userCfg

	rootPath := *pathFlag
	if rootPath == "" {
		rootPath = cfg.DefaultPath
	}
	if rootPath == "" {
		rootPath, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
			os.Exit(1)
		}
	}

	// Resolve to absolute path
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Verify path exists
	info, err := os.Stat(rootPath)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a valid directory\n", rootPath)
		os.Exit(1)
	}

	// Now that we have rootPath, load merged config (user + project overlay)
	if mergedCfg, err := config.LoadMerged(rootPath); err == nil {
		cfg = mergedCfg
	}

	// Derive per-path tmux session name so multiple asm instances (one per
	// root path) can run concurrently without stomping on each other.
	asmtmux.SetSessionName(rootPath)

	registry := buildRegistry(cfg)
	t := buildTracker(cfg, rootPath)

	if *deleteMode != "" {
		runDelete(*deleteMode, *deleteTaskName, *deleteDirty, *deleteWorktree)
	} else if *worktreeCreate {
		runWorktreeCreate(rootPath, *worktreeDir, t)
	} else if *providerSelect {
		runProviderSelect(registry)
	} else if *settingsMode {
		runSettings(cfg, rootPath, registry, t)
	} else if *pickerMode {
		runPicker(cfg, rootPath, registry, t)
	} else {
		runOrchestrator(cfg, rootPath, registry, t)
	}
}

func buildRegistry(cfg *config.Config) *provider.Registry {
	reg := provider.NewRegistry()

	// Built-in providers with optional config overrides
	overrides := make(map[string]provider.BuiltinOverride)
	for name, pc := range cfg.Providers {
		overrides[name] = provider.BuiltinOverride{Command: pc.Command, Args: pc.Args}
	}
	for _, p := range provider.Builtins(overrides) {
		reg.Register(p)
	}

	// Load plugins from ~/.asm/plugins/
	pluginDir := config.PluginDir()
	if entries, err := os.ReadDir(pluginDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			pluginPath := filepath.Join(pluginDir, entry.Name())
			p, err := provider.LoadPlugin(pluginPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to load plugin %q: %v\n", entry.Name(), err)
				continue
			}
			reg.Register(p)
		}
	}

	defaultName := cfg.DefaultProvider
	if defaultName == "" {
		defaultName = provider.DefaultProviderName
	}
	reg.SetDefault(defaultName)

	return reg
}

// buildTracker constructs the active tracker (built-in or plugin).
func buildTracker(cfg *config.Config, rootPath string) tracker.Tracker {
	name := cfg.DefaultTracker

	// Try plugin first if name is not a built-in
	if name != "" && !tracker.IsBuiltin(name) {
		if t := tracker.LoadFromDir(config.TrackerDir(), name); t != nil {
			return t
		}
	}

	// Built-in Dooray
	if name == "dooray" || name == "" {
		dc := loadDoorayConfig(cfg)
		saveFn := func(dc *tracker.DoorayConfig) error {
			return saveDoorayConfig(dc, config.ScopeUser, rootPath)
		}
		dt := tracker.NewDoorayTracker(dc, saveFn)
		return tracker.NewCachedTracker(dt, time.Hour)
	}

	// Fall back to any available plugin
	return tracker.LoadFromDir(config.TrackerDir(), "")
}

func loadDoorayConfig(cfg *config.Config) *tracker.DoorayConfig {
	dc := &tracker.DoorayConfig{}
	if m, ok := cfg.Trackers["dooray"]; ok {
		dc.Token = m["token"]
		dc.ProjectID = m["project_id"]
		dc.APIBaseURL = m["api_base_url"]
		dc.WebURL = m["web_url"]
		dc.TaskPattern = m["task_pattern"]
	}
	return dc
}

func saveDoorayConfig(dc *tracker.DoorayConfig, scope config.Scope, rootPath string) error {
	cfg, err := config.LoadScope(scope, rootPath)
	if err != nil {
		return err
	}
	if cfg.Trackers == nil {
		cfg.Trackers = make(map[string]map[string]string)
	}
	cfg.Trackers["dooray"] = map[string]string{
		"token":        dc.Token,
		"project_id":   dc.ProjectID,
		"api_base_url": dc.APIBaseURL,
		"web_url":      dc.WebURL,
		"task_pattern": dc.TaskPattern,
	}
	return config.SaveScope(cfg, scope, rootPath)
}

func runOrchestrator(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker) {
	if !asmtmux.IsAvailable() {
		fmt.Fprintln(os.Stderr, "Error: tmux is required. Install it with: brew install tmux")
		os.Exit(1)
	}

	// If already inside the asm tmux session, run picker directly
	if asmtmux.IsInsideTmux() && asmtmux.SessionExists() {
		runPicker(cfg, rootPath, registry, t)
		return
	}

	// Kill existing session if any
	if asmtmux.SessionExists() {
		asmtmux.KillSession()
	}

	// Get current executable path for picker command
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding executable: %v\n", err)
		os.Exit(1)
	}

	// Create tmux session (starts with default shell)
	pickerCmd := fmt.Sprintf("%s --picker --path %s", exe, rootPath)
	if err := asmtmux.CreateSession(pickerCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating tmux session: %v\n", err)
		os.Exit(1)
	}

	// Send the picker command to the main pane
	if err := asmtmux.SendPickerCommand(pickerCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending picker command: %v\n", err)
		os.Exit(1)
	}

	// Create the working panel (placeholder - 'cat' keeps it alive).
	// Working panel width = 100% - picker width.
	workingWidth := 100 - cfg.GetPickerWidth()
	if err := asmtmux.SplitWorkingPanel(workingWidth); err != nil {
		fmt.Fprintf(os.Stderr, "Error splitting pane: %v\n", err)
		os.Exit(1)
	}

	// Focus the picking panel
	asmtmux.FocusPickingPanel()

	// Attach to the session (blocks until session ends)
	if err := asmtmux.Attach(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
	}
}

func runDelete(dirName, taskName string, dirty, isWorktree bool) {
	model := ui.NewDeleteModel(dirName, taskName, dirty, isWorktree)
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.DeleteModel); ok && m.Confirmed {
		os.Exit(0)
	}
	os.Exit(1)
}

func runWorktreeCreate(rootPath, dirPath string, t tracker.Tracker) {
	model := ui.NewWorktreeRunnerModel(rootPath, dirPath, t)
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.WorktreeRunnerModel); ok && m.Created {
		os.Exit(0)
	}
	os.Exit(1)
}


func runProviderSelect(registry *provider.Registry) {
	model := ui.NewProviderSelectModel(registry.Names())
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.ProviderSelectModel); ok && m.Selected != "" {
		// Store selection in tmux session variable for picker to read
		asmtmux.SetSessionOption("asm-selected-provider", m.Selected)
		os.Exit(0)
	}
	os.Exit(1)
}

func runSettings(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker) {
	plugins := collectConfigurablePlugins(registry, t)
	trackerNames := append(tracker.BuiltinNames(), tracker.ListNames(config.TrackerDir())...)
	model := ui.NewSettingsModel(cfg, rootPath, registry.Names(), trackerNames, plugins)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func collectConfigurablePlugins(registry *provider.Registry, t tracker.Tracker) []plugincfg.Entry {
	var entries []plugincfg.Entry

	// AI provider plugins
	for _, name := range registry.Names() {
		p := registry.Get(name)
		if pp, ok := p.(*provider.PluginProvider); ok {
			entries = append(entries, plugincfg.Entry{
				Name:     pp.DisplayName(),
				Category: "provider",
				Path:     pp.PluginPath(),
			})
		}
	}

	// Tracker (built-in or plugin)
	if t != nil {
		inner := t
		if ct, ok := t.(*tracker.CachedTracker); ok {
			inner = ct.Inner()
		}
		switch tr := inner.(type) {
		case *tracker.PluginTracker:
			entries = append(entries, plugincfg.Entry{
				Name:     tr.Name(),
				Category: "tracker",
				Path:     tr.PluginPath(),
			})
		case plugincfg.Configurable:
			entries = append(entries, plugincfg.Entry{
				Name:     inner.(tracker.Tracker).Name(),
				Category: "tracker",
				Source:   tr,
			})
		}
	}

	return entries
}

func runPicker(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker) {
	model := ui.NewPickerModel(cfg, rootPath, registry, t)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithReportFocus())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
