package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const SessionName = "csm"
const MainWindow = "main"

func IsAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

func SessionExists() bool {
	err := exec.Command("tmux", "has-session", "-t", SessionName).Run()
	return err == nil
}

func WindowExists(windowName string) bool {
	out, err := exec.Command("tmux", "list-windows", "-t", SessionName, "-F", "#{window_name}").Output()
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

func WindowName(worktreeName string) string {
	return "wt-" + worktreeName
}

// CreateSession creates a new tmux session and sets up double-Tab pane switching.
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

	// Double-Tab to switch to picker:
	//
	// In the RIGHT pane (claude): Tab sends Tab normally + enters csm-tab table
	// In the LEFT pane (picker): Tab sends Tab normally (picker handles it via Bubble Tea)
	//
	// If second Tab comes within 400ms → focus picker
	// If not → timeout, back to root (first Tab was already sent, no loss)

	exec.Command("tmux", "bind-key", "-T", "root", "Tab",
		"if-shell", "-F", "#{==:#{pane_index},1}",
		"send-keys Tab ; switch-client -T csm-tab",
		"send-keys Tab",
	).Run()

	// Second Tab in csm-tab table → focus picker pane
	exec.Command("tmux", "bind-key", "-T", "csm-tab", "Tab",
		"select-pane", "-t", target,
	).Run()

	// Timeout for key table (400ms) — if no second Tab, just return to root
	exec.Command("tmux", "set-option", "-t", SessionName, "repeat-time", "400").Run()

	return nil
}

// SendPickerCommand sends the picker command to the main window.
func SendPickerCommand(pickerCmd string) error {
	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, MainWindow),
		pickerCmd, "Enter",
	).Run()
}

// SplitRight creates the right pane with a placeholder that stays alive.
func SplitRight(percentage int) error {
	return exec.Command("tmux", "split-window", "-h", "-d",
		"-l", fmt.Sprintf("%d%%", percentage),
		"-t", fmt.Sprintf("%s:%s.0", SessionName, MainWindow),
		"cat",
	).Run()
}

// FocusLeft focuses the left pane (picker).
func FocusLeft() error {
	return exec.Command("tmux", "select-pane",
		"-t", fmt.Sprintf("%s:%s.0", SessionName, MainWindow),
	).Run()
}

// FocusRight focuses the right pane (session).
func FocusRight() error {
	return exec.Command("tmux", "select-pane",
		"-t", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
	).Run()
}

// Attach attaches to the csm tmux session (blocking).
func Attach() error {
	cmd := exec.Command("tmux", "attach-session", "-t", SessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CreateWorktreeWindow creates a hidden tmux window with a shell,
// then sends the claude command via send-keys so aliases are available.
// When claude exits, marks the window with @claude-exited for detection.
func CreateWorktreeWindow(worktreeName, worktreePath, claudePath string, claudeArgs []string) error {
	winName := WindowName(worktreeName)

	err := exec.Command("tmux", "new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", worktreePath,
	).Run()
	if err != nil {
		return err
	}

	claudeCmd := claudePath
	for _, a := range claudeArgs {
		claudeCmd += " " + a
	}

	// After claude exits: signal via wait-for (instant event detection)
	exitSignal := fmt.Sprintf("tmux wait-for -S %s", ExitSignalName(worktreeName))
	fullCmd := claudeCmd + " ; " + exitSignal

	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	).Run()
}

// ResumeInWindow creates a hidden tmux window resuming a claude session.
func ResumeInWindow(worktreeName, worktreePath, claudePath, sessionID string, claudeArgs []string) error {
	winName := WindowName(worktreeName)

	err := exec.Command("tmux", "new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", worktreePath,
	).Run()
	if err != nil {
		return err
	}

	claudeCmd := claudePath + " --resume " + sessionID
	for _, a := range claudeArgs {
		claudeCmd += " " + a
	}

	exitSignal := fmt.Sprintf("tmux wait-for -S %s", ExitSignalName(worktreeName))
	fullCmd := claudeCmd + " ; " + exitSignal

	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	).Run()
}

// ExitSignalName returns the tmux wait-for signal name for a worktree.
func ExitSignalName(worktreeName string) string {
	return "csm-exit-" + worktreeName
}

// WaitForExit blocks until claude exits in the given worktree window.
// Returns the worktree name when the signal fires.
func WaitForExit(worktreeName string) error {
	return exec.Command("tmux", "wait-for", ExitSignalName(worktreeName)).Run()
}

// CleanupExitedWindow handles cleanup when claude exits in a worktree.
func CleanupExitedWindow(worktreeName string, isCurrentlyDisplayed bool) {
	if isCurrentlyDisplayed {
		SwapBackFromRight(worktreeName)
	}
	KillWorktreeWindow(worktreeName)
}

// SwapToRight swaps a worktree's window pane into the main window's right pane.
func SwapToRight(worktreeName string) error {
	winName := WindowName(worktreeName)
	return exec.Command("tmux", "swap-pane",
		"-s", fmt.Sprintf("%s:%s.0", SessionName, winName),
		"-t", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
	).Run()
}

// SwapBackFromRight swaps the current right pane back to its worktree window.
func SwapBackFromRight(worktreeName string) error {
	winName := WindowName(worktreeName)
	return exec.Command("tmux", "swap-pane",
		"-s", fmt.Sprintf("%s:%s.1", SessionName, MainWindow),
		"-t", fmt.Sprintf("%s:%s.0", SessionName, winName),
	).Run()
}

// KillWorktreeWindow kills a worktree's tmux window.
func KillWorktreeWindow(worktreeName string) error {
	winName := WindowName(worktreeName)
	return exec.Command("tmux", "kill-window",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
	).Run()
}

// CapturePaneContent captures the visible content of a worktree's pane.
func CapturePaneContent(worktreeName string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(worktreeName))
	}
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetPaneTitle reads the tmux pane title for a worktree's pane.
// Claude Code sets the pane title to indicate its current state.
func GetPaneTitle(worktreeName string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(worktreeName))
	}
	out, err := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_title}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// KillSession kills the entire csm tmux session.
func KillSession() error {
	return exec.Command("tmux", "kill-session", "-t", SessionName).Run()
}

// ListWorktreeWindows returns worktree names (without "wt-" prefix) of all active worktree windows.
func ListWorktreeWindows() []string {
	out, err := exec.Command("tmux", "list-windows", "-t", SessionName, "-F", "#{window_name}").Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "wt-") {
			result = append(result, strings.TrimPrefix(line, "wt-"))
		}
	}
	return result
}
