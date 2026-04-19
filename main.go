package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/asm/config"
	"github.com/nhn/asm/ide"
	"github.com/nhn/asm/notification"
	"github.com/nhn/asm/platform"
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
	sessionShort := flag.String("s", "", "Resume or create the given session ID")
	sessionFlag := flag.String("session", "", "Resume or create the given session ID")
	listMode := flag.Bool("list", false, "List currently running asm sessions")
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
	notifyHelperPayload := flag.String("notify-helper", "", "Internal notification helper payload")
	flag.Parse()

	initLog()
	defer closeLog()

	if strings.TrimSpace(*notifyHelperPayload) != "" {
		if err := notification.RunHelper(*notifyHelperPayload); err != nil {
			logErr("Error running notification helper: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *listMode {
		runListSessions()
		return
	}

	// Load user config first so provider/tracker defaults are available
	userCfg, err := config.LoadScope(config.ScopeUser, "")
	if err != nil {
		logErr("Error loading config: %v\n", err)
		os.Exit(1)
	}
	cfg := userCfg

	rootPath, err := resolveContextPath()
	if err != nil {
		logErr("Error resolving context path: %v\n", err)
		os.Exit(1)
	}
	info, err := os.Stat(rootPath)
	if err != nil || !info.IsDir() {
		logErr("Error: %s is not a valid directory\n", rootPath)
		os.Exit(1)
	}

	// Now that we have rootPath, load merged config (user + project overlay)
	if mergedCfg, err := config.LoadMerged(rootPath); err == nil {
		cfg = mergedCfg
	}
	if err := validateRuntimeDependencies(cfg); err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}

	// First-entry auto-seed of project worktree_base_path. Runs before any
	// subprocess fork so both orchestrator and picker (which re-reads
	// config in its own main()) observe the seeded value.
	autoSeedWorktreeBasePath(rootPath, cfg)

	requestedSessionID, err := parseRequestedSessionID(*sessionShort, *sessionFlag)
	if err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}

	sessionBoundMode := *pickerMode || *settingsMode || *deleteMode != "" || *worktreeCreate || *providerSelect || *ideSelect || *launcherMode || *batchConfirmMode
	restoreSelection := *restoreLast
	sessionSource := "generated:fallback"
	if sessionBoundMode {
		if inheritedSession := strings.TrimSpace(os.Getenv("ASM_SESSION_NAME")); strings.HasPrefix(inheritedSession, "asm-") {
			asmtmux.UseSessionName(inheritedSession)
			sessionSource = "env:ASM_SESSION_NAME"
		} else if asmtmux.IsInsideTmux() {
			if currentSession, err := asmtmux.CurrentSessionName(); err == nil && strings.HasPrefix(currentSession, "asm-") {
				asmtmux.UseSessionName(currentSession)
				sessionSource = "tmux:current-session"
			} else if requestedSessionID != "" {
				asmtmux.SetSessionID(requestedSessionID)
				sessionSource = "flag:session"
			} else {
				asmtmux.SetSessionID(generateSessionID())
				logDebug("main: failed to resolve current tmux session for session-bound mode context=%q err=%v", rootPath, err)
			}
		} else if requestedSessionID != "" {
			asmtmux.SetSessionID(requestedSessionID)
			sessionSource = "flag:session"
		} else {
			asmtmux.SetSessionID(generateSessionID())
		}
	} else {
		selection, err := resolveTopLevelSessionSelection(requestedSessionID)
		if err != nil {
			logErr("Error selecting session: %v\n", err)
			os.Exit(1)
		}
		asmtmux.SetSessionID(selection.ID)
		restoreSelection = selection.RestoreLast
		sessionSource = selection.Source
		if err := sessionstate.SaveLastSessionID(selection.ID); err != nil {
			logDebug("main: failed to persist last session id=%q err=%v", selection.ID, err)
		}
	}
	logDebug("main: context=%q picker=%t settings=%t delete=%q worktree_create=%t provider_select=%t ide_select=%t launcher=%t batch_confirm=%t session_id=%q session=%q source=%s inside_tmux=%t",
		rootPath, *pickerMode, *settingsMode, *deleteMode, *worktreeCreate, *providerSelect, *ideSelect, *launcherMode, *batchConfirmMode, asmtmux.SessionID, asmtmux.SessionName, sessionSource, asmtmux.IsInsideTmux())

	if sessionBoundMode && asmtmux.IsInsideTmux() {
		asmtmux.EnablePassthrough()
		asmtmux.InstallRootBindings()
	}

	registry := buildRegistry(cfg)
	var taskCache *tracker.TaskCache
	var t tracker.Tracker

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
		runPicker(cfg, rootPath, registry, t, taskCache, buildIDEs(cfg), restoreSelection)
	} else {
		runOrchestrator(cfg, rootPath, registry, t, taskCache, buildIDEs(cfg), restoreSelection)
	}
}

func validateRuntimeDependencies(cfg *config.Config) error {
	return nil
}

func buildRegistry(cfg *config.Config) *provider.Registry {
	reg := provider.NewRegistry()

	// Built-in providers with optional config overrides
	overrides := make(map[string]provider.BuiltinOverride)
	for name, pc := range cfg.Providers {
		overrides[name] = provider.BuiltinOverride{Command: pc.Command, Args: pc.Args}
	}
	for _, p := range provider.Builtins(overrides) {
		if err := reg.Register(p); err != nil {
			logErr("Warning: failed to register built-in provider %q: %v\n", p.Name(), err)
		}
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
			if err := reg.Register(p); err != nil {
				logErr("Warning: failed to register provider plugin %q: %v\n", entry.Name(), err)
			}
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

type sessionSelection struct {
	ID          string
	RestoreLast bool
	Source      string
}

func parseRequestedSessionID(shortValue, longValue string) (string, error) {
	shortValue = strings.TrimSpace(shortValue)
	longValue = strings.TrimSpace(longValue)
	switch {
	case shortValue != "" && longValue != "" && shortValue != longValue:
		return "", fmt.Errorf("-s and -session specify different session ids")
	case shortValue != "":
		if err := asmtmux.ValidateSessionID(shortValue); err != nil {
			return "", err
		}
		return shortValue, nil
	case longValue != "":
		if err := asmtmux.ValidateSessionID(longValue); err != nil {
			return "", err
		}
		return longValue, nil
	default:
		return "", nil
	}
}

func resolveContextPath() (string, error) {
	contextPath := strings.TrimSpace(os.Getenv("ASM_CONTEXT_PATH"))
	if contextPath == "" {
		home, err := platform.Current().HomeDir()
		if err != nil {
			return "", err
		}
		contextPath = home
	}
	return filepath.Abs(contextPath)
}

func runListSessions() {
	if !asmtmux.IsAvailable() {
		logErr("Error: tmux is required. Install it with: brew install tmux\n")
		os.Exit(1)
	}
	ids, err := asmtmux.ListASMSessionIDs()
	if err != nil {
		logErr("Error listing asm sessions: %v\n", err)
		os.Exit(1)
	}
	if len(ids) == 0 {
		fmt.Println("No running asm sessions.")
		return
	}
	for _, id := range ids {
		fmt.Println(id)
	}
}

func generateSessionID() string {
	return time.Now().Format("20060102-150405-000")
}

func resolveTopLevelSessionSelection(explicitSessionID string) (sessionSelection, error) {
	if explicitSessionID != "" {
		snap, err := sessionstate.Load(explicitSessionID)
		if err != nil {
			return sessionSelection{}, err
		}
		return sessionSelection{
			ID:          explicitSessionID,
			RestoreLast: snap != nil && snap.HasTargets(),
			Source:      "flag:session",
		}, nil
	}

	lastSessionID, err := sessionstate.LoadLastSessionID()
	if err != nil {
		return sessionSelection{}, err
	}
	lastSessionID = strings.TrimSpace(lastSessionID)
	if lastSessionID != "" && asmtmux.ValidateSessionID(lastSessionID) == nil {
		snap, err := sessionstate.Load(lastSessionID)
		if err != nil {
			return sessionSelection{}, err
		}
		running := asmtmux.SessionExistsNamed(asmtmux.DeriveSessionName(lastSessionID))
		if confirmContinueLastSession(lastSessionID, running, snap) {
			return sessionSelection{
				ID:          lastSessionID,
				RestoreLast: snap != nil && snap.HasTargets(),
				Source:      "state:last-session",
			}, nil
		}
	}

	return sessionSelection{
		ID:     generateSessionID(),
		Source: "generated:new-session",
	}, nil
}

func confirmContinueLastSession(sessionID string, running bool, snap *sessionstate.Snapshot) bool {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return true
	}

	fmt.Printf("Continue last asm session '%s'?\n", sessionID)
	if running {
		fmt.Printf("tmux session '%s' is already running.\n", asmtmux.DeriveSessionName(sessionID))
	}
	if snap != nil && snap.HasTargets() {
		aiCount, termCount := snapshotCounts(snap)
		fmt.Printf("Found %d AI and %d terminal session(s) from the last run.\n", aiCount, termCount)
	} else if !running {
		fmt.Println("No saved targets were found. Continuing will open the same session ID as an empty session.")
	}
	fmt.Print("Continue it now? [Y/n]: ")
	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

func snapshotCounts(snap *sessionstate.Snapshot) (int, int) {
	aiCount, termCount := 0, 0
	if snap == nil {
		return 0, 0
	}
	for _, target := range snap.Targets {
		if target.HasAI {
			aiCount++
		}
		if target.HasTerm {
			termCount++
		}
	}
	return aiCount, termCount
}

func runOrchestrator(cfg *config.Config, rootPath string, registry *provider.Registry, t tracker.Tracker, taskCache *tracker.TaskCache, ides []ide.IDE, restoreLast bool) {
	if !asmtmux.IsAvailable() {
		logErr("Error: tmux is required. Install it with: brew install tmux\n")
		os.Exit(1)
	}
	asmtmux.EnablePassthrough()

	if asmtmux.SessionExists() {
		if asmtmux.IsInsideTmux() {
			if currentSession, err := asmtmux.CurrentSessionName(); err == nil && currentSession == asmtmux.SessionName {
				runPicker(cfg, rootPath, registry, t, taskCache, ides, false)
				return
			}
		}
		if err := asmtmux.Attach(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			logErr("Error attaching to tmux session: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Get current executable path for picker command
	exe, err := platform.Current().ExecutablePath()
	if err != nil {
		logErr("Error finding executable: %v\n", err)
		os.Exit(1)
	}

	// Create tmux session (starts with default shell)
	pickerArgs := []string{exe, "--picker"}
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

	// Attach or switch to the session.
	if err := asmtmux.Attach(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		logErr("Error attaching to tmux session: %v\n", err)
		os.Exit(1)
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
	logDebug("launcher: start requested_path=%q session=%q", initialPath, asmtmux.SessionName)
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
	plugins := collectConfigurablePlugins(rootPath, registry)
	trackerNames := tracker.DefaultService().Names()
	ideNames := ide.Names(buildIDEs(cfg))
	model := ui.NewSettingsModel(cfg, rootPath, registry.Names(), trackerNames, ideNames, plugins)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		logErr("Error: %v\n", err)
		os.Exit(1)
	}
}

func collectConfigurablePlugins(rootPath string, registry *provider.Registry) []plugincfg.Entry {
	var entries []plugincfg.Entry

	// AI provider entries
	for _, name := range registry.Names() {
		p := registry.Get(name)
		if p == nil {
			continue
		}
		entry := plugincfg.Entry{
			Name:     p.DisplayName(),
			Category: "provider",
		}
		if pp, ok := p.(provider.PluginBacked); ok {
			entry.Path = pp.PluginPath()
			entries = append(entries, entry)
			continue
		}
		if source, ok := p.(plugincfg.Configurable); ok {
			entry.Source = source
			entries = append(entries, entry)
		}
	}

	entries = append(entries, tracker.DefaultService().Entries(rootPath)...)
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
