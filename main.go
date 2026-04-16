package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/ide"
	"github.com/nhn/asm/plugincfg"
	"github.com/nhn/asm/provider"
	"github.com/nhn/asm/sessionstate"
	"github.com/nhn/asm/shelljoin"
	asmtmux "github.com/nhn/asm/tmux"
	"github.com/nhn/asm/tracker"
	"github.com/nhn/asm/ui"
	"github.com/nhn/asm/worktree"
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
	ideSelect := flag.Bool("ide-select", false, "Run IDE selection dialog")
	launcherMode := flag.Bool("launcher", false, "Run session launcher dialog")
	batchConfirmMode := flag.Bool("batch-confirm", false, "Run batch confirmation dialog")
	restoreLast := flag.Bool("restore-last", false, "Restore previously open sessions in picker mode")
	flag.Parse()

	initLog()
	defer closeLog()

	// Load user config first to get DefaultPath
	userCfg, err := config.LoadScope(config.ScopeUser, "")
	if err != nil {
		logErr("Error loading config: %v\n", err)
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
			logErr("Error getting current directory: %v\n", err)
			os.Exit(1)
		}
	}

	// Resolve to absolute path
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		logErr("Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Verify path exists
	info, err := os.Stat(rootPath)
	if err != nil || !info.IsDir() {
		logErr("Error: %s is not a valid directory\n", rootPath)
		os.Exit(1)
	}

	// Now that we have rootPath, load merged config (user + project overlay)
	if mergedCfg, err := config.LoadMerged(rootPath); err == nil {
		cfg = mergedCfg
	}

	// First-entry auto-seed of project worktree_base_path. Runs before any
	// subprocess fork so both orchestrator and picker (which re-reads
	// config in its own main()) observe the seeded value.
	autoSeedWorktreeBasePath(rootPath, cfg)

	// Derive the tmux session name for this process. Top-level asm launches
	// still hash from --path, but picker/dialog subprocesses running inside an
	// existing asm tmux session must target that CURRENT session even when they
	// receive a different --path (launcher/settings local-scope context, etc.).
	sessionBoundMode := *pickerMode || *settingsMode || *deleteMode != "" || *worktreeCreate || *providerSelect || *ideSelect || *launcherMode || *batchConfirmMode
	sessionSource := "derived-from-root"
	if sessionBoundMode {
		if inheritedSession := strings.TrimSpace(os.Getenv("ASM_SESSION_NAME")); strings.HasPrefix(inheritedSession, "asm-") {
			asmtmux.SessionName = inheritedSession
			sessionSource = "env:ASM_SESSION_NAME"
		} else if asmtmux.IsInsideTmux() {
			if currentSession, err := asmtmux.CurrentSessionName(); err == nil && strings.HasPrefix(currentSession, "asm-") {
				asmtmux.SessionName = currentSession
				sessionSource = "tmux:current-session"
			} else {
				asmtmux.SetSessionName(rootPath)
				if err != nil {
					logDebug("main: failed to resolve current tmux session for session-bound mode root=%q err=%v", rootPath, err)
				}
			}
		} else {
			asmtmux.SetSessionName(rootPath)
		}
	} else {
		asmtmux.SetSessionName(rootPath)
	}
	logDebug("main: root=%q picker=%t settings=%t delete=%q worktree_create=%t provider_select=%t ide_select=%t launcher=%t batch_confirm=%t session=%q source=%s inside_tmux=%t",
		rootPath, *pickerMode, *settingsMode, *deleteMode, *worktreeCreate, *providerSelect, *ideSelect, *launcherMode, *batchConfirmMode, asmtmux.SessionName, sessionSource, asmtmux.IsInsideTmux())

	if sessionBoundMode && asmtmux.IsInsideTmux() {
		asmtmux.InstallRootBindings()
	}

	registry := buildRegistry(cfg)
	taskCache := tracker.NewTaskCache(rootPath, 7*24*time.Hour)
	t := buildTracker(cfg, rootPath, taskCache)

	if *deleteMode != "" {
		runDelete(*deleteMode, *deleteTaskName, *deleteDirty, *deleteWorktree)
	} else if *worktreeCreate {
		runWorktreeCreate(rootPath, *worktreeDir, t)
	} else if *providerSelect {
		runProviderSelect(registry)
	} else if *ideSelect {
		runIDESelect(buildIDEs(cfg))
	} else if *launcherMode {
		runLauncher(rootPath, t, taskCache)
	} else if *batchConfirmMode {
		runBatchConfirm()
	} else if *settingsMode {
		runSettings(cfg, rootPath, registry, t)
	} else if *pickerMode {
		runPicker(cfg, rootPath, registry, t, taskCache, buildIDEs(cfg), *restoreLast)
	} else {
		runOrchestrator(cfg, rootPath, registry, t, taskCache, buildIDEs(cfg))
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
				logErr("Warning: failed to load plugin %q: %v\n", entry.Name(), err)
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
func buildTracker(cfg *config.Config, rootPath string, taskCache *tracker.TaskCache) tracker.Tracker {
	name := cfg.DefaultTracker

	// Try plugin first if name is not a built-in
	if name != "" && !tracker.IsBuiltin(name) {
		if t := tracker.LoadFromDir(config.TrackerDir(), name); t != nil {
			return tracker.NewCachedTracker(t, taskCache)
		}
	}

	// Built-in Dooray
	if name == "dooray" || name == "" {
		dc := loadDoorayConfig(cfg)
		saveFn := func(dc *tracker.DoorayConfig) error {
			return saveDoorayConfig(dc, config.ScopeUser, rootPath)
		}
		dt := tracker.NewDoorayTracker(dc, saveFn)
		return tracker.NewCachedTracker(dt, taskCache)
	}

	// Fall back to any available plugin
	if t := tracker.LoadFromDir(config.TrackerDir(), ""); t != nil {
		return tracker.NewCachedTracker(t, taskCache)
	}
	return nil
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

// autoSeedWorktreeBasePath writes a project-scope worktree_base_path into
// .asm/config.toml the FIRST time asm is run against a git repo that already
// has linked worktrees. Rationale: the user clearly has an existing layout
// convention — we should lock it in so the worktree-create dialog and
// settings UI reflect it, instead of letting the `~/worktrees/{repo}`
// default win by accident.
//
// Guardrails:
//   - Not in repo mode → nothing to infer from.
//   - Project config file already exists → user has been here before, do
//     not touch. Covers both the "we already seeded" and "user has their
//     own project settings" cases uniformly.
//   - No linked worktrees → nothing to infer.
//   - Detected parent matches whatever GetWorktreeBasePath would already
//     return → writing would be a no-op, so skip to avoid creating an
//     otherwise-empty .asm/ directory.
//
// Best-effort: a write failure is logged and ignored — the normal
// fallback chain in resolveWorktreeBase still covers the UX.
func autoSeedWorktreeBasePath(rootPath string, cfg *config.Config) {
	if !worktree.IsRepoMode(rootPath) {
		return
	}
	if _, err := os.Stat(config.ProjectConfigPath(rootPath)); err == nil {
		return
	}
	parent := worktree.MostRecentLinkedWorktreeParent(rootPath)
	if parent == "" {
		return
	}
	repoName := worktree.RepoName(rootPath)
	if parent == cfg.GetWorktreeBasePath(repoName) {
		return
	}
	projectCfg, err := config.LoadScope(config.ScopeProject, rootPath)
	if err != nil {
		logErr("auto-seed worktree_base_path: load project config failed: %v\n", err)
		return
	}
	projectCfg.WorktreeBasePath = parent
	if err := config.SaveScope(projectCfg, config.ScopeProject, rootPath); err != nil {
		logErr("auto-seed worktree_base_path: save failed: %v\n", err)
		return
	}
	// Propagate to the in-memory cfg so the rest of THIS process sees the
	// seeded value without a re-read. Subprocesses re-run LoadMerged in
	// their own main() and pick it up from disk.
	cfg.WorktreeBasePath = parent
}

// confirmRestartExistingSession prompts the user before we destroy an
// existing asm tmux session for this rootPath. Returns true to proceed
// with kill+restart, false to abort. If stdin isn't a TTY (piped, cron,
// etc.) we can't prompt, so fall back to the old silent-kill behavior —
// refusing would break non-interactive launchers.
func confirmRestartExistingSession(rootPath string) bool {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return true
	}
	fmt.Printf("asm is already running for %s (tmux session: %s).\n", rootPath, asmtmux.SessionName)
	fmt.Println("Closing it will kill every AI/terminal session inside.")
	fmt.Print("Close existing session and start fresh? [y/N]: ")
	var answer string
	// Fscanln returns an error on empty input / EOF / ^C; treat all of
	// those as "no" so the safe default (preserve the running session)
	// wins whenever the user hesitates.
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func confirmRestorePreviousSession(rootPath string, snap *sessionstate.Snapshot) bool {
	if snap == nil || !snap.HasTargets() {
		return false
	}
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return false
	}
	aiCount, termCount := 0, 0
	for _, target := range snap.Targets {
		if target.HasAI {
			aiCount++
		}
		if target.HasTerm {
			termCount++
		}
	}
	fmt.Printf("Restore previous asm sessions for %s?\n", rootPath)
	fmt.Printf("Found %d AI and %d terminal session(s) from the last run.\n", aiCount, termCount)
	fmt.Print("Restore them now? [Y/n]: ")
	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

func runOrchestrator(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker, taskCache *tracker.TaskCache, ides []ide.IDE) {
	if !asmtmux.IsAvailable() {
		logErr("Error: tmux is required. Install it with: brew install tmux\n")
		os.Exit(1)
	}

	// If already inside the asm tmux session, run picker directly
	if asmtmux.IsInsideTmux() && asmtmux.SessionExists() {
		runPicker(cfg, rootPath, registry, t, taskCache, ides, false)
		return
	}

	// Kill existing session if any — but warn first so a stray invocation
	// doesn't wipe out AI sessions the user still cares about. The "inside
	// tmux" branch above already handled the case where this instance IS
	// that session, so reaching here means the session is attached (or
	// detached) elsewhere and a silent kill is genuinely destructive.
	//
	// ASM_RESTART_CONFIRMED=1 is set by the picker's ←/→ re-exec path to
	// skip re-asking — the picker already prompted and the user said yes.
	// Unset after reading so it can't leak into child processes or a
	// later in-place re-exec.
	if asmtmux.SessionExists() {
		if os.Getenv("ASM_RESTART_CONFIRMED") == "1" {
			os.Unsetenv("ASM_RESTART_CONFIRMED")
		} else if !confirmRestartExistingSession(rootPath) {
			fmt.Println("Cancelled.")
			return
		}
		_ = sessionstate.Delete(rootPath)
		asmtmux.KillSession()
	}

	restoreLast := false
	if snap, err := sessionstate.Load(rootPath); err == nil && snap != nil && snap.HasTargets() {
		if confirmRestorePreviousSession(rootPath, snap) {
			restoreLast = true
		} else {
			_ = sessionstate.Delete(rootPath)
		}
	}

	// Get current executable path for picker command
	exe, err := os.Executable()
	if err != nil {
		logErr("Error finding executable: %v\n", err)
		os.Exit(1)
	}

	// Create tmux session (starts with default shell)
	pickerArgs := []string{exe, "--picker", "--path", rootPath}
	if restoreLast {
		pickerArgs = append(pickerArgs, "--restore-last")
	}
	pickerCmd := shelljoin.Join(pickerArgs...)
	if err := asmtmux.CreateSession(pickerCmd); err != nil {
		logErr("Error creating tmux session: %v\n", err)
		os.Exit(1)
	}

	// Send the picker command to the main pane
	if err := asmtmux.SendPickerCommand(pickerCmd); err != nil {
		logErr("Error sending picker command: %v\n", err)
		os.Exit(1)
	}

	// Create the working panel (placeholder - 'cat' keeps it alive).
	// Working panel width = 100% - picker width.
	workingWidth := 100 - cfg.GetPickerWidth()
	if err := asmtmux.SplitWorkingPanel(workingWidth); err != nil {
		logErr("Error splitting pane: %v\n", err)
		os.Exit(1)
	}

	// Focus the picking panel
	asmtmux.FocusPickingPanel()

	// Attach to the session (blocks until session ends)
	attachErr := asmtmux.Attach()

	// The picker's ←/→ navigate writes a handoff file then kills the tmux
	// session, which pops Attach. If the handoff is present, re-exec asm
	// with the new --path so we end up in a fresh tmux session named after
	// the new root. syscall.Exec replaces the current process image, so
	// repeated navigations don't accumulate parent asm processes.
	if data, err := os.ReadFile(asmtmux.HandoffFilePath()); err == nil {
		os.Remove(asmtmux.HandoffFilePath())
		next := strings.TrimSpace(string(data))
		if next != "" {
			self, err := os.Executable()
			if err == nil {
				// Mark the re-exec as user-confirmed so the new process
				// doesn't re-ask "close existing session?" — the picker
				// already surfaced that prompt before writing the handoff.
				// Preserve the original env otherwise so ~/.asm config,
				// tmux, $SHELL etc. carry through.
				env := append(os.Environ(), "ASM_RESTART_CONFIRMED=1")
				_ = syscall.Exec(self, []string{self, "--path", next}, env)
			}
		}
	}

	if attachErr != nil {
		if exitErr, ok := attachErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
	}
}

func runDelete(dirName, taskName string, dirty, isWorktree bool) {
	model := ui.NewDeleteModel(dirName, taskName, dirty, isWorktree)
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		logErr("Error: %v\n", err)
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
		logErr("Error: %v\n", err)
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
		logErr("Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.ProviderSelectModel); ok && m.Selected != "" {
		// Store selection in tmux session variable for picker to read
		asmtmux.SetSessionOption("asm-selected-provider", m.Selected)
		os.Exit(0)
	}
	os.Exit(1)
}

// buildIDEs merges the built-in IDE list with any config overrides.
func buildIDEs(cfg *config.Config) []ide.IDE {
	overrides := make(map[string]ide.Override, len(cfg.IDEs))
	for name, c := range cfg.IDEs {
		overrides[name] = ide.Override{Command: c.Command, Args: c.Args}
	}
	return ide.Builtins(overrides)
}

func runIDESelect(ides []ide.IDE) {
	model := ui.NewIDESelectModel(ide.Names(ides))
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.IDESelectModel); ok && m.Selected != "" {
		asmtmux.SetSessionOption("asm-selected-ide", m.Selected)
		os.Exit(0)
	}
	os.Exit(1)
}

func runLauncher(initialPath string, t tracker.Tracker, taskCache *tracker.TaskCache) {
	logDebug("launcher: start initial_path=%q session=%q", initialPath, asmtmux.SessionName)
	model := ui.NewLauncherModel(initialPath, t, taskCache)
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.LauncherModel); ok && m.SelectedPath != "" {
		logDebug("launcher: selected path=%q session=%q", m.SelectedPath, asmtmux.SessionName)
		if err := asmtmux.SetSessionOption("asm-selected-target-path", m.SelectedPath); err != nil {
			logErr("Error storing launcher selection: %v\n", err)
			os.Exit(1)
		}
		logDebug("launcher: stored selection path=%q session=%q", m.SelectedPath, asmtmux.SessionName)
		os.Exit(0)
	}
	logDebug("launcher: exited without selection session=%q result_type=%T", asmtmux.SessionName, result)
	os.Exit(1)
}

func runBatchConfirm() {
	req, err := ui.LoadBatchConfirmRequest()
	if err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}

	model := ui.NewBatchConfirmRunnerModel(req)
	p := tea.NewProgram(model, tea.WithAltScreen())

	result, err := p.Run()
	if err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := result.(ui.BatchConfirmRunnerModel); ok && m.Confirmed {
		os.Exit(0)
	}
	os.Exit(1)
}

func runSettings(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker) {
	plugins := collectConfigurablePlugins(registry, t)
	trackerNames := append(tracker.BuiltinNames(), tracker.ListNames(config.TrackerDir())...)
	ideNames := ide.Names(buildIDEs(cfg))
	model := ui.NewSettingsModel(cfg, rootPath, registry.Names(), trackerNames, ideNames, plugins)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		logErr("Error: %v\n", err)
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

func runPicker(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker, taskCache *tracker.TaskCache, ides []ide.IDE, restoreLast bool) {
	model := ui.NewPickerModel(cfg, rootPath, registry, t, taskCache, ides, restoreLast)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithReportFocus())

	if _, err := p.Run(); err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}
}
