package tmux

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nhn/asm/asmlog"
)

// SessionName is the active tmux session name. Defaults to "asm" but is
// expected to be reassigned via SetSessionName so multiple asm instances on
// different root paths can coexist.
var SessionName = "asm"

const MainWindow = "main"

// paneBaseIndex caches the global pane-base-index so we don't query tmux
// repeatedly. Populated lazily by PaneBase().
var paneBaseIndex = -1

// PaneBase returns the tmux pane-base-index (typically 0 or 1).
func PaneBase() int {
	if paneBaseIndex >= 0 {
		return paneBaseIndex
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "show-options", "-gv", "pane-base-index").Output()
	if err != nil {
		paneBaseIndex = 0
		return 0
	}
	s := strings.TrimSpace(string(out))
	if s == "1" {
		paneBaseIndex = 1
	} else {
		paneBaseIndex = 0
	}
	return paneBaseIndex
}

// pickerPane returns the pane index string for the picker (first pane).
func pickerPane() string {
	return fmt.Sprintf("%d", PaneBase())
}

// workingPane returns the pane index string for the working panel (second pane).
func workingPane() string {
	return fmt.Sprintf("%d", PaneBase()+1)
}

// pickerTarget returns the tmux target for the picker pane in the main window.
func pickerTarget() string {
	return fmt.Sprintf("%s:%s.%s", SessionName, MainWindow, pickerPane())
}

// workingTarget returns the tmux target for the working pane in the main window.
func workingTarget() string {
	return fmt.Sprintf("%s:%s.%s", SessionName, MainWindow, workingPane())
}

// windowFirstPane returns the target for the first pane in a named window.
func windowFirstPane(winName string) string {
	return fmt.Sprintf("%s:%s.%s", SessionName, winName, pickerPane())
}

// DeriveSessionName computes the tmux session name for a given rootPath
// without mutating any global state. Pure and idempotent — callers that
// need to check "would there be a session for this other path?" use this
// together with SessionExistsNamed.
func DeriveSessionName(rootPath string) string {
	base := filepath.Base(rootPath)
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, base)
	if sanitized == "" {
		sanitized = "root"
	}
	h := fnv.New32a()
	h.Write([]byte(rootPath))
	return fmt.Sprintf("asm-%s-%06x", sanitized, h.Sum32()&0xffffff)
}

// SetSessionName derives a per-rootPath tmux session name. Same rootPath
// always yields the same name (hash-stable), different rootPaths get
// distinct names. Safe to call repeatedly.
func SetSessionName(rootPath string) {
	SessionName = DeriveSessionName(rootPath)
}

// HandoffFilePath returns the path used by the picker to leave a "next
// rootPath" for its orchestrator before killing the tmux session. The
// picker writes it, the orchestrator reads it right after Attach returns,
// and on non-empty content re-execs asm with the new --path. Keyed by
// SessionName so concurrent asm instances don't clobber each other.
func HandoffFilePath() string {
	return filepath.Join(os.TempDir(), "asm-handoff-"+SessionName+".txt")
}

func IsAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// CurrentSessionName returns the tmux session name of the currently attached
// client. Intended for picker/dialog subprocesses already running inside an
// existing asm tmux session.
func CurrentSessionName() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		asmlog.Debugf("tmux: current-session lookup failed err=%v", err)
		return "", err
	}
	name := strings.TrimSpace(string(out))
	asmlog.Debugf("tmux: current-session=%q", name)
	return name, nil
}

func SessionExists() bool {
	return SessionExistsNamed(SessionName)
}

// SessionExistsNamed is the arbitrary-name variant used by navigation
// preflight: the picker needs to ask "would my target --path already have
// an asm session running?" without mutating the global SessionName.
func SessionExistsNamed(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, "tmux", "has-session", "-t", name).Run()
	return err == nil
}

// TargetID returns a stable short hash for an absolute target path.
func TargetID(targetPath string) string {
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(targetPath)))
	return fmt.Sprintf("%010x", h.Sum64()&0xffffffffff)
}

func WindowExists(windowName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-windows", "-t", SessionName, "-F", "#{window_name}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == windowName {
			return true
		}
	}
	return false
}

func WindowName(targetPath string) string {
	return "ai-" + TargetID(targetPath)
}

func windowTarget(winName string) string {
	return fmt.Sprintf("%s:%s", SessionName, winName)
}

func setWindowOptionByName(winName, key, value string) error {
	return runTmuxCmd("set-option", "-w",
		"-t", windowTarget(winName),
		"@"+key, value,
	)
}

func setManagedWindowMetadata(winName, targetPath, displayName, kind string) error {
	cleanPath := filepath.Clean(targetPath)
	options := [][2]string{
		{"asm-id", TargetID(cleanPath)},
		{"asm-path", cleanPath},
		{"asm-display-name", displayName},
		{"asm-kind", kind},
		{"asm-started-at", strconv.FormatInt(time.Now().Unix(), 10)},
	}
	for _, opt := range options {
		if err := setWindowOptionByName(winName, opt[0], opt[1]); err != nil {
			return err
		}
	}
	return nil
}

// InstallRootBindings refreshes asm's server-wide tmux key bindings so the
// current binary's routing rules apply even to already-running asm sessions.
func InstallRootBindings() {
	// Key bindings are installed in the tmux root table, which is SERVER-WIDE.
	// Multiple asm instances (one per project root) therefore can't each own
	// their own hardcoded target session — whichever started last would
	// clobber earlier bindings and route every keystroke to its own picker.
	//
	// Instead, the bindings below target the CURRENT session's `main.0`
	// (asm's picker pane) and are gated by `#{m:asm-*,#{session_name}}` so
	// they only fire when the user is actually inside an asm-managed
	// session. In any other tmux session (plain shell, unrelated work) the
	// key is passed through unchanged. Result: every asm session handles
	// its own keys without cross-talk, and non-asm sessions aren't affected.
	const inAsm = "#{m:asm-*,#{session_name}}"
	const inUtility = "#{==:#{@asm-utility-open},1}"

	// Simple routed key: inside asm session, deliver an F-key to the
	// picker pane; outside, pass the key through unchanged.
	pp := pickerPane()
	wp := workingPane()
	pickerRef := "main." + pp

	bindRouted := func(key, fkey string) {
		_ = exec.Command("tmux", "bind-key", "-T", "root", key,
			"if-shell", "-F", inAsm,
			"send-keys -t "+pickerRef+" "+fkey,
			"send-keys "+key,
		).Run()
	}

	// Utility dialogs run in the working panel and need some keys to reach the
	// dialog process itself instead of being swallowed by picker-level global
	// shortcuts. When a utility window is focused, pass the original key
	// through unchanged; otherwise route to the picker pane.
	bindRoutedUnlessUtility := func(key, pickerKey, utilityKey string) {
		utilityTarget := pickerRef
		if utilityKey == "Tab" || utilityKey == "BTab" || strings.HasPrefix(utilityKey, "F") {
			utilityTarget = workingTarget()
		}
		_ = exec.Command("tmux", "bind-key", "-T", "root", key,
			"if-shell", "-F", inAsm,
			fmt.Sprintf("if-shell -F '%s' 'send-keys -t %s %s' 'send-keys -t %s %s'", inUtility, utilityTarget, utilityKey, pickerRef, pickerKey),
			"send-keys "+key,
		).Run()
	}

	bindRouted("C-t", "F12")                     // toggle terminal/AI
	bindRoutedUnlessUtility("C-n", "F10", "F10") // launcher in picker, new-branch in worktree dialog
	bindRouted("C-s", "F9")                      // settings
	bindRouted("C-q", "F8")                      // quit
	bindRouted("C-w", "F7")                      // create worktree
	bindRouted("C-d", "F6")                      // delete directory
	bindRouted("C-p", "F4")                      // provider selection
	bindRouted("C-k", "F3")                      // kill selected session
	bindRouted("C-l", "C-l")                     // toggle picker panel visibility
	bindRouted("C-o", "o")                       // open task URL
	bindRouted("C-]", "F1")                      // rotate to next active session
	bindRouted("C-e", "F2")                      // open worktree in IDE
	bindRoutedUnlessUtility("Tab", "Tab", "Tab")

	// Ctrl+g: toggle pane focus — pane-index dependent.
	//   working panel → select picker
	//   picking panel → send F11 so the picker can focus-or-start
	_ = exec.Command("tmux", "bind-key", "-T", "root", "C-g",
		"if-shell", "-F", inAsm,
		fmt.Sprintf("if-shell -F '#{==:#{pane_index},%s}' 'select-pane -t %s' 'send-keys -t %s F11'", wp, pickerRef, pickerRef),
		"send-keys C-g",
	).Run()

	// Ctrl+x: toggle batch selection. Picker-only — when working pane is
	// focused (dialogs use Ctrl+X for their own actions) pass through.
	_ = exec.Command("tmux", "bind-key", "-T", "root", "C-x",
		"if-shell", "-F", inAsm,
		fmt.Sprintf("if-shell -F '#{==:#{pane_index},%s}' 'send-keys -t %s F5' 'send-keys C-x'", pp, pickerRef),
		"send-keys C-x",
	).Run()

	// Enable focus events so Bubble Tea can detect pane focus/blur
	exec.Command("tmux", "set-option", "-t", SessionName, "focus-events", "on").Run()

	// Enable mouse support for scrollback in working panel
	exec.Command("tmux", "set-option", "-t", SessionName, "mouse", "on").Run()

	// Scroll up enters copy mode with -e (auto-exit when scrolled back to bottom)
	exec.Command("tmux", "bind-key", "-T", "root", "WheelUpPane",
		"if-shell", "-F", "-t=", "#{mouse_any_flag}",
		"send-keys -M",
		"if-shell -Ft= '#{pane_in_mode}' 'send-keys -M' 'copy-mode -e'",
	).Run()
}

// CreateSession creates a new tmux session and sets up pane-switching key bindings.
func CreateSession(pickerCmd string) error {
	// Chain set-option atomically with new-session via ";" so that
	// destroy-unattached is disabled before tmux's event loop can destroy
	// the detached session.
	err := runTmuxCmd("new-session", "-d",
		"-s", SessionName,
		"-n", MainWindow,
		"-x", "200", "-y", "50",
		";", "set-option", "-t", SessionName, "destroy-unattached", "off",
	)
	if err != nil {
		return err
	}

	InstallRootBindings()

	return nil
}

// SendPickerCommand sends the picker command to the main window.
func SendPickerCommand(pickerCmd string) error {
	return runTmuxCmd("send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		pickerCmd, "Enter",
	)
}

// SplitWorkingPanel creates the working panel with a placeholder that stays alive.
func SplitWorkingPanel(percentage int) error {
	return runTmuxCmd("split-window", "-h", "-d",
		"-l", fmt.Sprintf("%d%%", percentage),
		"-t", pickerTarget(),
		"cat",
	)
}

// ResizePickerPanel sets the picker pane width to the given percent
// of the terminal width. Applied immediately to a running session.
func ResizePickerPanel(pickerPct int) error {
	if pickerPct <= 0 {
		return nil
	}
	w := TerminalWidth()
	cells := w * pickerPct / 100
	if cells < 10 {
		cells = 10
	}
	return runTmuxCmd("resize-pane",
		"-t", pickerTarget(),
		"-x", fmt.Sprintf("%d", cells),
	)
}

// WorkingPanelExists checks if the working panel exists.
func WorkingPanelExists() bool {
	out, err := exec.Command("tmux", "list-panes",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		"-F", "#{pane_index}",
	).Output()
	if err != nil {
		return false
	}
	wp := workingPane()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == wp {
			return true
		}
	}
	return false
}

// EnsureWorkingPanel recreates the working panel if it was lost.
func EnsureWorkingPanel() {
	if !WorkingPanelExists() {
		SplitWorkingPanel(70)
	}
}

// FocusPickingPanel focuses the picking panel (picker).
// Unzooms the main window first since zoom hides the picker.
func FocusPickingPanel() error {
	UnzoomWorkingPanel()
	return runTmuxCmd("select-pane",
		"-t", pickerTarget(),
	)
}

// ActivePaneIndex returns the logical index of the currently active pane in
// the main window (0 = picker, 1 = working). Returns -1 on error.
func ActivePaneIndex() int {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		"-p", "#{pane_index}",
	).Output()
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(out))
	if s == pickerPane() {
		return 0
	}
	if s == workingPane() {
		return 1
	}
	return -1
}

// IsWorkingPanelZoomed reports whether the main window is currently zoomed.
func IsWorkingPanelZoomed() bool {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		"-p", "#{window_zoomed_flag}",
	).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

// ZoomWorkingPanel zooms the working pane (pane 1) to fullscreen.
// If zoom is currently on a different pane (e.g. the picker), unzoom first
// so the zoom flag moves onto pane 1.
func ZoomWorkingPanel() error {
	return zoomPane(1)
}

// ZoomPickingPanel zooms the picker pane (pane 0) to fullscreen.
// If zoom is currently on a different pane (e.g. the working pane), unzoom
// first so the zoom flag moves onto pane 0.
func ZoomPickingPanel() error {
	return zoomPane(0)
}

// zoomPane is the shared implementation for ZoomWorkingPanel / ZoomPickingPanel.
// idx is a logical index: 0 = picker, 1 = working.
// tmux's zoom invariant: when a window is zoomed, the active pane IS the
// zoomed pane. So we can detect "already zoomed on target" via
// ActivePaneIndex() and skip the flash of unzoom + re-zoom in that case.
func zoomPane(idx int) error {
	var target string
	if idx == 0 {
		target = pickerTarget()
	} else {
		target = workingTarget()
	}
	if IsWorkingPanelZoomed() && ActivePaneIndex() == idx {
		return nil
	}
	if IsWorkingPanelZoomed() {
		UnzoomWorkingPanel()
	}
	runTmuxCmd("select-pane", "-t", target)
	return runTmuxCmd("resize-pane", "-Z", "-t", target)
}

// UnzoomWorkingPanel removes zoom if currently zoomed. Safe to call always.
func UnzoomWorkingPanel() error {
	if !IsWorkingPanelZoomed() {
		return nil
	}
	return runTmuxCmd("resize-pane",
		"-Z", "-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
	)
}

// EnableStatusBar turns on a three-line status bar at the bottom of the
// terminal, spanning full width.
//
//	line 0: current session detail
//	line 1: other active sessions
//	line 2: keyboard shortcuts hint
//
// Content is set via SetStatusLines / SetShortcutsLine.
func EnableStatusBar() {
	runTmux("set-option", "-t", SessionName, "status", "3")
	runTmux("set-option", "-t", SessionName, "status-position", "bottom")
	runTmux("set-option", "-t", SessionName, "status-style", "bg=colour236,fg=colour252")
	runTmux("set-option", "-t", SessionName, "status-left-length", "500")
	runTmux("set-option", "-t", SessionName, "status-right", "")
	runTmux("set-option", "-t", SessionName, "status-format[0]", "")
	runTmux("set-option", "-t", SessionName, "status-format[1]", "")
	runTmux("set-option", "-t", SessionName, "status-format[2]", "")
}

// SetStatusLines writes the two summary lines (current + other sessions).
// Both accept tmux format strings (e.g. "#[fg=cyan,bold]...#[default]").
func SetStatusLines(line1, line2 string) {
	runTmux("set-option", "-t", SessionName,
		"status-format[0]", "#[align=left]"+line1,
	)
	runTmux("set-option", "-t", SessionName,
		"status-format[1]", "#[align=left]"+line2,
	)
}

// SetShortcutsLine writes the keyboard shortcuts line (bottom of the status
// area, full terminal width).
func SetShortcutsLine(line string) {
	runTmux("set-option", "-t", SessionName,
		"status-format[2]", "#[align=left,bg=colour236,fg=colour244]"+line,
	)
}

// runTmux is a best-effort tmux invocation guarded by tmuxCallTimeout so a
// stuck tmux server can't leak the calling goroutine. Errors are ignored —
// all current callers are fire-and-forget UI state writes.
func runTmux(args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, "tmux", args...).Run()
}

// workingSlotSize returns the current width/height of the main working pane.
// Hidden AI/terminal/dialog windows are pinned to this size so swapping panes
// doesn't force a reflow in the provider TUI.
func workingSlotSize() (int, int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message",
		"-t", workingTarget(), "-p", "#{pane_width}x#{pane_height}").Output()
	if err != nil {
		return 0, 0, false
	}
	var w, h int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%dx%d", &w, &h); err != nil {
		return 0, 0, false
	}
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func syncHiddenWindowSize(winName string) {
	if winName == "" || !WindowExists(winName) {
		return
	}
	w, h, ok := workingSlotSize()
	if !ok {
		return
	}
	target := fmt.Sprintf("%s:%s", SessionName, winName)
	runTmux("set-option", "-w", "-t", target, "window-size", "manual")
	runTmux("resize-window", "-t", target, "-x", fmt.Sprintf("%d", w), "-y", fmt.Sprintf("%d", h))
}

func managedHiddenWindowNames() []string {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-windows", "-t", SessionName, "-F", "#{window_name}\t#{@asm-kind}").Output()
	if err != nil {
		return nil
	}
	utilitySet := make(map[string]bool, len(utilityWindows))
	for _, name := range utilityWindows {
		utilitySet[name] = true
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" || name == MainWindow {
			continue
		}
		kind := ""
		if len(parts) > 1 {
			kind = strings.TrimSpace(parts[1])
		}
		if kind == "ai" || kind == "term" || utilitySet[name] {
			names = append(names, name)
		}
	}
	return names
}

// SyncManagedWindowSizes pins every hidden AI/terminal/dialog window to the
// current working-pane size. Best-effort only.
func SyncManagedWindowSizes() {
	for _, winName := range managedHiddenWindowNames() {
		syncHiddenWindowSize(winName)
	}
}

// TerminalWidth returns the attached client's terminal width in columns.
// Falls back to 120 if it can't be queried.
func TerminalWidth() int {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-t", SessionName,
		"-p", "#{client_width}").Output()
	if err != nil {
		return 120
	}
	var w int
	_, err = fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &w)
	if err != nil || w <= 0 {
		return 120
	}
	return w
}

// FocusWorkingPanel focuses the working panel (session).
func FocusWorkingPanel() error {
	return runTmuxCmd("select-pane",
		"-t", workingTarget(),
	)
}

// Attach attaches to the asm tmux session (blocking).
func Attach() error {
	cmd := exec.Command("tmux", "attach-session", "-t", SessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CreateDirectoryWindow creates a hidden tmux window with a shell,
// then sends the AI command via send-keys so aliases are available.
// When the AI process exits, signals via wait-for for detection.
func CreateDirectoryWindow(targetPath, dirPath, command string, args []string) error {
	winName := WindowName(targetPath)
	asmlog.Debugf("tmux: create-directory-window session=%q win=%q target=%q dir=%q command=%q args=%v",
		SessionName, winName, targetPath, dirPath, command, args)

	err := runTmuxCmd("new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", dirPath,
	)
	if err != nil {
		asmlog.Debugf("tmux: create-directory-window new-window failed session=%q win=%q err=%v",
			SessionName, winName, err)
		return err
	}
	if err := setManagedWindowMetadata(winName, targetPath, filepath.Base(dirPath), "ai"); err != nil {
		asmlog.Debugf("tmux: create-directory-window metadata failed session=%q win=%q err=%v",
			SessionName, winName, err)
		return err
	}
	syncHiddenWindowSize(winName)

	aiCmd := command
	for _, a := range args {
		aiCmd += " " + a
	}

	// After AI exits: signal via wait-for (instant event detection)
	exitSignal := fmt.Sprintf("tmux wait-for -S %s", ExitSignalName(targetPath))
	fullCmd := aiCmd + " ; " + exitSignal

	if err := runTmuxCmd("send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	); err != nil {
		asmlog.Debugf("tmux: create-directory-window send-keys failed session=%q win=%q err=%v",
			SessionName, winName, err)
		return err
	}
	asmlog.Debugf("tmux: create-directory-window ready session=%q win=%q target=%q", SessionName, winName, targetPath)
	return nil
}

// ExitSignalName returns the tmux wait-for signal name for a target path.
func ExitSignalName(targetPath string) string {
	return "asm-exit-" + TargetID(targetPath)
}

// waitForExitPollInterval bounds each `tmux wait-for` blocking call so a
// lost/racy signal can't park the caller goroutine forever. On timeout the
// wrapper re-checks whether the window still exists; if it's gone we treat
// the session as exited. If it's still alive we loop and wait again.
const waitForExitPollInterval = 30 * time.Second

// WaitForExit blocks until the AI process exits in the given AI window.
// Uses a bounded `tmux wait-for` + window-existence probe so a missed signal
// or a tmux server restart can't leak the caller goroutine indefinitely.
func WaitForExit(targetPath string) error {
	return waitForSignalOrWindowGone(ExitSignalName(targetPath), WindowName(targetPath))
}

// waitForSignalOrWindowGone is the shared logic behind WaitForExit and the
// terminal-exit waiter. Returns nil once either the tmux signal fires or the
// window has disappeared. Propagates only non-deadline exec errors.
func waitForSignalOrWindowGone(signal, windowName string) error {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), waitForExitPollInterval)
		err := exec.CommandContext(ctx, "tmux", "wait-for", signal).Run()
		cancel()
		if err == nil {
			return nil
		}
		// Deadline hit — the signal may have been missed. If the window
		// is gone, treat the session as exited; otherwise re-arm the
		// wait. Any non-deadline error propagates.
		if ctx.Err() != context.DeadlineExceeded {
			return err
		}
		if !WindowExists(windowName) {
			return nil
		}
	}
}

// CleanupExitedWindow handles cleanup when the AI process exits for a target.
func CleanupExitedWindow(targetPath string, isCurrentlyDisplayed bool) {
	if isCurrentlyDisplayed {
		SwapBackFromWorkingPanel(targetPath)
	}
	KillDirectoryWindow(targetPath)
}

func swapPaneToWorking(winName string) error {
	syncHiddenWindowSize(winName)
	return runTmuxCmd("swap-pane",
		"-s", windowFirstPane(winName),
		"-t", workingTarget(),
	)
}

func swapPaneFromWorking(winName string) error {
	syncHiddenWindowSize(winName)
	return runTmuxCmd("swap-pane",
		"-s", workingTarget(),
		"-t", windowFirstPane(winName),
	)
}

// SwapToWorkingPanel swaps a target's AI window into the main working panel.
func SwapToWorkingPanel(targetPath string) error {
	return swapPaneToWorking(WindowName(targetPath))
}

// SwapBackFromWorkingPanel swaps the current working panel back to its AI window.
func SwapBackFromWorkingPanel(targetPath string) error {
	return swapPaneFromWorking(WindowName(targetPath))
}

// KillDirectoryWindow kills a target's AI tmux window.
func KillDirectoryWindow(targetPath string) error {
	winName := WindowName(targetPath)
	return runTmuxCmd("kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
	)
}

// RunInWorkingPanel creates a hidden tmux window running cmd, swaps it into the
// working panel, and returns the window name used for waiting/cleanup.
// The exit code is stored in tmux variable @{windowName}-exit.
func RunInWorkingPanel(windowName, cmd string) error {
	EnsureWorkingPanel()
	asmlog.Debugf("tmux: run-dialog-window session=%q win=%q cmd=%q", SessionName, windowName, cmd)
	_ = runTmuxCmd("set-option", "-t", SessionName, "@asm-utility-open", "1")
	err := runTmuxCmd("new-window", "-d",
		"-t", SessionName,
		"-n", windowName,
	)
	if err != nil {
		asmlog.Debugf("tmux: run-dialog-window new-window failed session=%q win=%q err=%v",
			SessionName, windowName, err)
		_ = runTmuxCmd("set-option", "-t", SessionName, "@asm-utility-open", "0")
		return err
	}
	syncHiddenWindowSize(windowName)

	exitVar := fmt.Sprintf("@%s-exit", windowName)
	exitSignal := fmt.Sprintf("tmux set -t %s %s $? ; tmux wait-for -S %s", SessionName, exitVar, windowName)
	fullCmd := cmd + " ; " + exitSignal

	if err := runTmuxCmd("send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, windowName),
		fullCmd, "Enter",
	); err != nil {
		asmlog.Debugf("tmux: run-dialog-window send-keys failed session=%q win=%q err=%v",
			SessionName, windowName, err)
		_ = runTmuxCmd("set-option", "-t", SessionName, "@asm-utility-open", "0")
		return err
	}

	// Swap into working panel
	if err := runTmuxCmd("swap-pane",
		"-s", windowFirstPane(windowName),
		"-t", workingTarget(),
	); err != nil {
		asmlog.Debugf("tmux: run-dialog-window swap-pane failed session=%q win=%q err=%v",
			SessionName, windowName, err)
		_ = runTmuxCmd("set-option", "-t", SessionName, "@asm-utility-open", "0")
		return err
	}
	asmlog.Debugf("tmux: run-dialog-window ready session=%q win=%q", SessionName, windowName)
	return nil
}

// WaitAndCleanupWorkingPanel blocks until the window's process exits,
// swaps back, kills the window, and focuses picking panel.
// Returns the exit code of the process (0 = success).
func WaitAndCleanupWorkingPanel(windowName string) int {
	// Bounded wait with window-existence probe — same pattern as
	// WaitForExit, so a lost signal or externally-killed window can't
	// park this goroutine forever.
	_ = waitForSignalOrWindowGone(windowName, windowName)

	exitCode := 1
	exitVar := fmt.Sprintf("@%s-exit", windowName)
	out, err := exec.Command("tmux", "show-option", "-t", SessionName, "-v", exitVar).Output()
	if err == nil {
		s := strings.TrimSpace(string(out))
		if s == "0" {
			exitCode = 0
		}
	}

	runTmux("swap-pane",
		"-s", workingTarget(),
		"-t", windowFirstPane(windowName),
	)
	runTmux("kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, windowName),
	)
	runTmux("set-option",
		"-t", SessionName, "@asm-utility-open", "0",
	)
	FocusPickingPanel()
	return exitCode
}

// tmuxCallTimeout bounds individual tmux CLI calls used on hot paths (pane
// title/content poll) so a stuck tmux server can't park goroutines and
// snowball the per-second detect-state loop.
const tmuxCallTimeout = 3 * time.Second

// runTmuxCmd runs a tmux command and wraps the error with stderr output
// so callers get actionable messages instead of bare "exit status 1".
func runTmuxCmd(args ...string) error {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		asmlog.Debugf("tmux: command failed session=%q args=%v err=%v output=%q", SessionName, args, err, msg)
		if msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}

// CapturePaneContent captures the visible content of an AI pane.
func CapturePaneContent(targetPath string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = workingTarget()
	} else {
		target = windowFirstPane(WindowName(targetPath))
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", target, "-p").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CapturePaneHistory captures the last `lines` of scrollback plus the visible
// pane content. Useful when the visible area is full of UI chrome and the
// actual response has scrolled above.
func CapturePaneHistory(targetPath string, isDisplayed bool, lines int) (string, error) {
	var target string
	if isDisplayed {
		target = workingTarget()
	} else {
		target = windowFirstPane(WindowName(targetPath))
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", target, "-p",
		"-S", fmt.Sprintf("-%d", lines)).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetPaneTitle reads the tmux pane title for an AI pane.
// The AI provider sets the pane title to indicate its current state.
func GetPaneTitle(targetPath string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = workingTarget()
	} else {
		target = windowFirstPane(WindowName(targetPath))
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p", "#{pane_title}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// utilityWindows are non-session windows that should be treated as modal
// dialogs: only one should be open at a time, Ctrl+G should close them, and
// repeated open requests should refocus the existing dialog instead of trying
// to spawn a duplicate window.
var utilityWindows = []string{
	"asm-settings",
	"asm-worktree",
	"asm-delete",
	"asm-batch-confirm",
	"asm-provider-select",
	"asm-ide-select",
	"asm-launcher",
}

// HasUtilityWindow returns true if any utility window is open.
func HasUtilityWindow() bool {
	for _, name := range utilityWindows {
		if WindowExists(name) {
			return true
		}
	}
	return false
}

// CloseUtilityPanel sends Escape twice to the working panel to gracefully close any utility dialog.
// First Escape clears any active filter/input, second Escape closes the dialog.
func CloseUtilityPanel() {
	target := workingTarget()
	exec.Command("tmux", "send-keys", "-t", target, "Escape").Run()
	exec.Command("tmux", "send-keys", "-t", target, "Escape").Run()
}

// KillSession kills the entire asm tmux session.
func KillSession() error {
	return runTmuxCmd("kill-session", "-t", SessionName)
}

// TerminalWindowName returns the tmux window name for a target's terminal.
func TerminalWindowName(targetPath string) string {
	return "term-" + TargetID(targetPath)
}

// TermExitSignalName returns the tmux wait-for signal name for a terminal.
func TermExitSignalName(targetPath string) string {
	return "asm-term-exit-" + TargetID(targetPath)
}

// WaitForTermExit is the terminal-window counterpart of WaitForExit — it uses
// the same bounded wait + window-existence probe so a missed signal can't
// park the caller goroutine forever.
func WaitForTermExit(targetPath string) error {
	return waitForSignalOrWindowGone(TermExitSignalName(targetPath), TerminalWindowName(targetPath))
}

// CreateTerminalWindow creates a hidden tmux window with a shell at the directory path.
// When the shell exits, sends a wait-for signal for cleanup.
func CreateTerminalWindow(targetPath, dirPath string) error {
	winName := TerminalWindowName(targetPath)

	err := runTmuxCmd("new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", dirPath,
	)
	if err != nil {
		return err
	}
	if err := setManagedWindowMetadata(winName, targetPath, filepath.Base(dirPath), "term"); err != nil {
		return err
	}
	syncHiddenWindowSize(winName)

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "zsh"
	}

	exitSignal := fmt.Sprintf("tmux wait-for -S %s", TermExitSignalName(targetPath))
	fullCmd := shell + " ; " + exitSignal

	return runTmuxCmd("send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	)
}

// SwapTermToWorkingPanel swaps a terminal window's pane into the main window's working panel.
func SwapTermToWorkingPanel(targetPath string) error {
	return swapPaneToWorking(TerminalWindowName(targetPath))
}

// SwapTermBackFromWorkingPanel swaps the working panel back to the terminal's hidden window.
func SwapTermBackFromWorkingPanel(targetPath string) error {
	return swapPaneFromWorking(TerminalWindowName(targetPath))
}

// KillTerminalWindow kills a terminal's tmux window.
func KillTerminalWindow(targetPath string) error {
	winName := TerminalWindowName(targetPath)
	return runTmuxCmd("kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
	)
}

// SetSessionOption sets a tmux session-level option.
func SetSessionOption(key, value string) error {
	err := runTmuxCmd("set-option", "-t", SessionName, "@"+key, value)
	if err != nil {
		asmlog.Debugf("tmux: set-session-option failed session=%q key=%q value=%q err=%v", SessionName, key, value, err)
		return err
	}
	asmlog.Debugf("tmux: set-session-option session=%q key=%q value=%q", SessionName, key, value)
	return nil
}

// GetSessionOption reads a tmux session-level option.
func GetSessionOption(key string) string {
	out, err := exec.Command("tmux", "show-option", "-t", SessionName, "-v", "@"+key).Output()
	if err != nil {
		asmlog.Debugf("tmux: get-session-option failed session=%q key=%q err=%v", SessionName, key, err)
		return ""
	}
	value := strings.TrimSpace(string(out))
	asmlog.Debugf("tmux: get-session-option session=%q key=%q value=%q", SessionName, key, value)
	return value
}

// SetWindowOption sets a tmux window option on a target's AI window.
func SetWindowOption(targetPath, key, value string) error {
	return setWindowOptionByName(WindowName(targetPath), key, value)
}

// GetWindowOption reads a tmux window option from a target's AI window.
func GetWindowOption(targetPath, key string) string {
	winName := WindowName(targetPath)
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "show-option", "-w", "-v",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		"@"+key,
	).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SessionKind is a bitmask of active session types for a given worktree.
type SessionKind uint8

const (
	SessionAI   SessionKind = 1 << iota // wt-<name> window
	SessionTerm                         // term-<name> window
)

func (k SessionKind) HasAI() bool   { return k&SessionAI != 0 }
func (k SessionKind) HasTerm() bool { return k&SessionTerm != 0 }

// ListActiveSessions returns per-target kind flags by inspecting live tmux windows.
// Keys are absolute target paths from @asm-path metadata; values are bitmasks of
// kinds that currently have a window. Targets with no active session are omitted.
func ListActiveSessions() map[string]SessionKind {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-windows", "-t", SessionName, "-F", "#{window_name}\t#{@asm-path}\t#{@asm-kind}").Output()
	if err != nil {
		return nil
	}
	result := make(map[string]SessionKind)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		targetPath := strings.TrimSpace(parts[1])
		if targetPath == "" {
			continue
		}
		switch strings.TrimSpace(parts[2]) {
		case "ai":
			result[targetPath] |= SessionAI
		case "term":
			result[targetPath] |= SessionTerm
		}
	}
	return result
}

// ListDirectoryWindows returns absolute target paths of all active AI windows.
func ListDirectoryWindows() []string {
	kinds := ListActiveSessions()
	var result []string
	for name, k := range kinds {
		if k.HasAI() {
			result = append(result, name)
		}
	}
	return result
}
