package tmux

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SessionName is the active tmux session name. Defaults to "asm" but is
// expected to be reassigned via SetSessionName so multiple asm instances on
// different root paths can coexist.
var SessionName = "asm"

const MainWindow = "main"

// SetSessionName derives a per-rootPath tmux session name. Same rootPath
// always yields the same name (hash-stable), different rootPaths get
// distinct names. Safe to call repeatedly.
func SetSessionName(rootPath string) {
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
	SessionName = fmt.Sprintf("asm-%s-%06x", sanitized, h.Sum32()&0xffffff)
}

func IsAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

func SessionExists() bool {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, "tmux", "has-session", "-t", SessionName).Run()
	return err == nil
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

func WindowName(dirName string) string {
	return "wt-" + dirName
}

// CreateSession creates a new tmux session and sets up pane-switching key bindings.
func CreateSession(pickerCmd string) error {
	err := exec.Command("tmux", "new-session", "-d",
		"-s", SessionName,
		"-n", MainWindow,
		"-x", "200", "-y", "50",
	).Run()
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s:%s.0", SessionName, MainWindow)

	// Ctrl+t: toggle terminal/AI (sends F12 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-t",
		"send-keys", "-t", target, "F12",
	).Run()

	// Ctrl+g: toggle pane focus
	//   working panel → focus picking panel
	//   picking panel → focus working panel or start AI session (picker handles via F11)
	exec.Command("tmux", "bind-key", "-T", "root", "C-g",
		"if-shell", "-F", "#{==:#{pane_index},1}",
		fmt.Sprintf("select-pane -t %s", target),
		fmt.Sprintf("send-keys -t %s F11", target),
	).Run()

	// Ctrl+n: new AI session (sends F10 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-n",
		"send-keys", "-t", target, "F10",
	).Run()

	// Ctrl+s: settings (sends F9 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-s",
		"send-keys", "-t", target, "F9",
	).Run()

	// Ctrl+q: quit (sends F8 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-q",
		"send-keys", "-t", target, "F8",
	).Run()

	// Ctrl+w: create worktree (sends F7 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-w",
		"send-keys", "-t", target, "F7",
	).Run()

	// Ctrl+d: delete directory (sends F6 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-d",
		"send-keys", "-t", target, "F6",
	).Run()

	// Ctrl+x: toggle batch selection (picker-only). Pane-aware — when
	// the working pane is focused (settings dialog, worktree dialog,
	// etc.) we pass the key through so those UIs can use Ctrl+X for
	// their own actions (e.g. deleting an IDE entry in settings).
	exec.Command("tmux", "bind-key", "-T", "root", "C-x",
		"if-shell", "-F", "#{==:#{pane_index},0}",
		fmt.Sprintf("send-keys -t %s F5", target),
		"send-keys C-x",
	).Run()

	// Ctrl+p: provider selection (sends F4 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-p",
		"send-keys", "-t", target, "F4",
	).Run()

	// Ctrl+k: open task URL (sends F3 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-k",
		"send-keys", "-t", target, "F3",
	).Run()

	// Ctrl+] : rotate to next active session (F1). Single direction — cycles
	// back to the first session after the last. ASCII control code 0x1D is
	// forwarded by every terminal. tmux only recognises F1–F12 as named keys
	// (F13+ is sent as literal text), so we reuse a free F-key slot.
	exec.Command("tmux", "bind-key", "-T", "root", "C-]",
		"send-keys", "-t", target, "F1",
	).Run()

	// Ctrl+e: open cursor worktree in an IDE (sends F2 to picker).
	exec.Command("tmux", "bind-key", "-T", "root", "C-e",
		"send-keys", "-t", target, "F2",
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

	return nil
}

// SendPickerCommand sends the picker command to the main window.
func SendPickerCommand(pickerCmd string) error {
	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		pickerCmd, "Enter",
	).Run()
}

// SplitWorkingPanel creates the working panel with a placeholder that stays alive.
func SplitWorkingPanel(percentage int) error {
	return exec.Command("tmux", "split-window", "-h", "-d",
		"-l", fmt.Sprintf("%d%%", percentage),
		"-t", fmt.Sprintf("%s:%s.0", SessionName, MainWindow),
		"cat",
	).Run()
}

// ResizePickerPanel sets the picker pane (pane 0) width to the given percent
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
	return exec.Command("tmux", "resize-pane",
		"-t", fmt.Sprintf("%s:%s.0", SessionName, MainWindow),
		"-x", fmt.Sprintf("%d", cells),
	).Run()
}

// WorkingPanelExists checks if the working panel (pane 1) exists.
func WorkingPanelExists() bool {
	out, err := exec.Command("tmux", "list-panes",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		"-F", "#{pane_index}",
	).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "1" {
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
// Unzooms the main window first since zoom hides pane 0.
func FocusPickingPanel() error {
	UnzoomWorkingPanel()
	return exec.Command("tmux", "select-pane",
		"-t", fmt.Sprintf("%s:%s.0", SessionName, MainWindow),
	).Run()
}

// ActivePaneIndex returns the index of the currently active pane in the main
// window (0 = picker, 1 = working). Returns -1 on error.
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
	switch s {
	case "0":
		return 0
	case "1":
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
// tmux's zoom invariant: when a window is zoomed, the active pane IS the
// zoomed pane. So we can detect "already zoomed on target" via
// ActivePaneIndex() and skip the flash of unzoom + re-zoom in that case.
func zoomPane(idx int) error {
	target := fmt.Sprintf("%s:%s.%d", SessionName, MainWindow, idx)
	if IsWorkingPanelZoomed() && ActivePaneIndex() == idx {
		return nil
	}
	if IsWorkingPanelZoomed() {
		UnzoomWorkingPanel()
	}
	exec.Command("tmux", "select-pane", "-t", target).Run()
	return exec.Command("tmux", "resize-pane", "-Z", "-t", target).Run()
}

// UnzoomWorkingPanel removes zoom if currently zoomed. Safe to call always.
func UnzoomWorkingPanel() error {
	if !IsWorkingPanelZoomed() {
		return nil
	}
	return exec.Command("tmux", "resize-pane",
		"-Z", "-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
	).Run()
}

// EnableStatusBar turns on a three-line status bar at the bottom of the
// terminal, spanning full width.
//   line 0: current session detail
//   line 1: other active sessions
//   line 2: keyboard shortcuts hint
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
	return exec.Command("tmux", "select-pane",
		"-t", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
	).Run()
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
func CreateDirectoryWindow(dirName, dirPath, command string, args []string) error {
	winName := WindowName(dirName)

	err := exec.Command("tmux", "new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", dirPath,
	).Run()
	if err != nil {
		return err
	}

	aiCmd := command
	for _, a := range args {
		aiCmd += " " + a
	}

	// After AI exits: signal via wait-for (instant event detection)
	exitSignal := fmt.Sprintf("tmux wait-for -S %s", ExitSignalName(dirName))
	fullCmd := aiCmd + " ; " + exitSignal

	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	).Run()
}

// ExitSignalName returns the tmux wait-for signal name for a directory.
func ExitSignalName(dirName string) string {
	return "asm-exit-" + dirName
}

// waitForExitPollInterval bounds each `tmux wait-for` blocking call so a
// lost/racy signal can't park the caller goroutine forever. On timeout the
// wrapper re-checks whether the window still exists; if it's gone we treat
// the session as exited. If it's still alive we loop and wait again.
const waitForExitPollInterval = 30 * time.Second

// WaitForExit blocks until the AI process exits in the given directory window.
// Uses a bounded `tmux wait-for` + window-existence probe so a missed signal
// or a tmux server restart can't leak the caller goroutine indefinitely.
func WaitForExit(dirName string) error {
	return waitForSignalOrWindowGone(ExitSignalName(dirName), WindowName(dirName))
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

// CleanupExitedWindow handles cleanup when the AI process exits in a directory.
func CleanupExitedWindow(dirName string, isCurrentlyDisplayed bool) {
	if isCurrentlyDisplayed {
		SwapBackFromWorkingPanel(dirName)
	}
	KillDirectoryWindow(dirName)
}

func swapPaneToWorking(winName string) error {
	return exec.Command("tmux", "swap-pane",
		"-s", fmt.Sprintf("%s:%s.0", SessionName, winName),
		"-t", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
	).Run()
}

func swapPaneFromWorking(winName string) error {
	return exec.Command("tmux", "swap-pane",
		"-s", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
		"-t", fmt.Sprintf("%s:%s.0", SessionName, winName),
	).Run()
}

// SwapToWorkingPanel swaps a directory's window pane into the main window's working panel.
func SwapToWorkingPanel(dirName string) error {
	return swapPaneToWorking(WindowName(dirName))
}

// SwapBackFromWorkingPanel swaps the current working panel back to its directory window.
func SwapBackFromWorkingPanel(dirName string) error {
	return swapPaneFromWorking(WindowName(dirName))
}

// KillDirectoryWindow kills a directory's tmux window.
func KillDirectoryWindow(dirName string) error {
	winName := WindowName(dirName)
	return exec.Command("tmux", "kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
	).Run()
}

// RunInWorkingPanel creates a hidden tmux window running cmd, swaps it into the
// working panel, and returns the window name used for waiting/cleanup.
// The exit code is stored in tmux variable @{windowName}-exit.
func RunInWorkingPanel(windowName, cmd string) error {
	EnsureWorkingPanel()
	err := exec.Command("tmux", "new-window", "-d",
		"-t", SessionName,
		"-n", windowName,
	).Run()
	if err != nil {
		return err
	}

	exitVar := fmt.Sprintf("@%s-exit", windowName)
	exitSignal := fmt.Sprintf("tmux set -t %s %s $? ; tmux wait-for -S %s", SessionName, exitVar, windowName)
	fullCmd := cmd + " ; " + exitSignal

	if err := exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, windowName),
		fullCmd, "Enter",
	).Run(); err != nil {
		return err
	}

	// Swap into working panel
	return exec.Command("tmux", "swap-pane",
		"-s", fmt.Sprintf("%s:%s.0", SessionName, windowName),
		"-t", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
	).Run()
}

// WaitAndCleanupWorkingPanel blocks until the window's process exits,
// swaps back, kills the window, and focuses picking panel.
// Returns the exit code of the process (0 = success).
func WaitAndCleanupWorkingPanel(windowName string) int {
	exec.Command("tmux", "wait-for", windowName).Run()

	exitCode := 1
	exitVar := fmt.Sprintf("@%s-exit", windowName)
	out, err := exec.Command("tmux", "show-option", "-t", SessionName, "-v", exitVar).Output()
	if err == nil {
		s := strings.TrimSpace(string(out))
		if s == "0" {
			exitCode = 0
		}
	}

	exec.Command("tmux", "swap-pane",
		"-s", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
		"-t", fmt.Sprintf("%s:%s.0", SessionName, windowName),
	).Run()
	exec.Command("tmux", "kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, windowName),
	).Run()
	FocusPickingPanel()
	return exitCode
}

// tmuxCallTimeout bounds individual tmux CLI calls used on hot paths (pane
// title/content poll) so a stuck tmux server can't park goroutines and
// snowball the per-second detect-state loop.
const tmuxCallTimeout = 3 * time.Second

// CapturePaneContent captures the visible content of a directory's pane.
func CapturePaneContent(dirName string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(dirName))
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
func CapturePaneHistory(dirName string, isDisplayed bool, lines int) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(dirName))
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

// GetPaneTitle reads the tmux pane title for a directory's pane.
// The AI provider sets the pane title to indicate its current state.
func GetPaneTitle(dirName string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(dirName))
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p", "#{pane_title}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// utilityWindows are non-session windows that should be closed on Ctrl+G.
var utilityWindows = []string{"asm-settings", "asm-worktree", "asm-delete", "asm-provider-select"}

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
	target := fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	exec.Command("tmux", "send-keys", "-t", target, "Escape").Run()
	exec.Command("tmux", "send-keys", "-t", target, "Escape").Run()
}

// KillSession kills the entire asm tmux session.
func KillSession() error {
	return exec.Command("tmux", "kill-session", "-t", SessionName).Run()
}

// TerminalWindowName returns the tmux window name for a directory's terminal.
func TerminalWindowName(dirName string) string {
	return "term-" + dirName
}

// TermExitSignalName returns the tmux wait-for signal name for a terminal.
func TermExitSignalName(dirName string) string {
	return "asm-term-exit-" + dirName
}

// WaitForTermExit is the terminal-window counterpart of WaitForExit — it uses
// the same bounded wait + window-existence probe so a missed signal can't
// park the caller goroutine forever.
func WaitForTermExit(dirName string) error {
	return waitForSignalOrWindowGone(TermExitSignalName(dirName), TerminalWindowName(dirName))
}

// CreateTerminalWindow creates a hidden tmux window with a shell at the directory path.
// When the shell exits, sends a wait-for signal for cleanup.
func CreateTerminalWindow(dirName, dirPath string) error {
	winName := TerminalWindowName(dirName)

	err := exec.Command("tmux", "new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", dirPath,
	).Run()
	if err != nil {
		return err
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "zsh"
	}

	exitSignal := fmt.Sprintf("tmux wait-for -S %s", TermExitSignalName(dirName))
	fullCmd := shell + " ; " + exitSignal

	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	).Run()
}

// SwapTermToWorkingPanel swaps a terminal window's pane into the main window's working panel.
func SwapTermToWorkingPanel(dirName string) error {
	return swapPaneToWorking(TerminalWindowName(dirName))
}

// SwapTermBackFromWorkingPanel swaps the working panel back to the terminal's hidden window.
func SwapTermBackFromWorkingPanel(dirName string) error {
	return swapPaneFromWorking(TerminalWindowName(dirName))
}

// KillTerminalWindow kills a terminal's tmux window.
func KillTerminalWindow(dirName string) error {
	winName := TerminalWindowName(dirName)
	return exec.Command("tmux", "kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
	).Run()
}

// SetSessionOption sets a tmux session-level option.
func SetSessionOption(key, value string) error {
	return exec.Command("tmux", "set-option", "-t", SessionName, "@"+key, value).Run()
}

// GetSessionOption reads a tmux session-level option.
func GetSessionOption(key string) string {
	out, err := exec.Command("tmux", "show-option", "-t", SessionName, "-v", "@"+key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SetWindowOption sets a tmux window option on a directory's window.
func SetWindowOption(dirName, key, value string) error {
	winName := WindowName(dirName)
	return exec.Command("tmux", "set-option", "-w",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		"@"+key, value,
	).Run()
}

// GetWindowOption reads a tmux window option from a directory's window.
func GetWindowOption(dirName, key string) string {
	winName := WindowName(dirName)
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

// ListActiveSessions returns per-directory kind flags by inspecting live tmux windows.
// Keys are directory names (without the wt-/term- prefix); values are bitmasks of
// kinds that currently have a window. Directories with no active session are
// omitted. One tmux call, shared by ListDirectoryWindows.
func ListActiveSessions() map[string]SessionKind {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-windows", "-t", SessionName, "-F", "#{window_name}").Output()
	if err != nil {
		return nil
	}
	result := make(map[string]SessionKind)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case strings.HasPrefix(line, "wt-"):
			name := strings.TrimPrefix(line, "wt-")
			result[name] |= SessionAI
		case strings.HasPrefix(line, "term-"):
			name := strings.TrimPrefix(line, "term-")
			result[name] |= SessionTerm
		}
	}
	return result
}

// ListDirectoryWindows returns directory names (without "wt-" prefix) of all active AI
// directory windows. Kept for callers that only care about AI sessions.
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
