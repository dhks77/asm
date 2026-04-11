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

	// Ctrl+t: toggle terminal/claude (sends F12 to picker from either pane)
	exec.Command("tmux", "bind-key", "-T", "root", "C-t",
		"send-keys", "-t", target, "F12",
	).Run()

	// Ctrl+g: toggle pane focus
	//   working panel → focus picking panel (direct)
	//   picking panel → focus working panel or start Claude (picker handles via F11)
	exec.Command("tmux", "bind-key", "-T", "root", "C-g",
		"if-shell", "-F", "#{==:#{pane_index},1}",
		fmt.Sprintf("select-pane -t %s", target),
		fmt.Sprintf("send-keys -t %s F11", target),
	).Run()

	// Ctrl+n: new Claude session (sends F10 to picker from either pane)
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

// FocusPickingPanel focuses the picking panel (picker).
func FocusPickingPanel() error {
	return exec.Command("tmux", "select-pane",
		"-t", fmt.Sprintf("%s:%s.0", SessionName, MainWindow),
	).Run()
}

// FocusWorkingPanel focuses the working panel (session).
func FocusWorkingPanel() error {
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

// CreateDirectoryWindow creates a hidden tmux window with a shell,
// then sends the claude command via send-keys so aliases are available.
// When claude exits, marks the window with @claude-exited for detection.
func CreateDirectoryWindow(dirName, dirPath, claudePath string, claudeArgs []string) error {
	winName := WindowName(dirName)

	err := exec.Command("tmux", "new-window", "-d",
		"-t", SessionName,
		"-n", winName,
		"-c", dirPath,
	).Run()
	if err != nil {
		return err
	}

	claudeCmd := claudePath
	for _, a := range claudeArgs {
		claudeCmd += " " + a
	}

	// After claude exits: signal via wait-for (instant event detection)
	exitSignal := fmt.Sprintf("tmux wait-for -S %s", ExitSignalName(dirName))
	fullCmd := claudeCmd + " ; " + exitSignal

	return exec.Command("tmux", "send-keys",
		"-t", fmt.Sprintf("%s:%s", SessionName, winName),
		fullCmd, "Enter",
	).Run()
}

// ExitSignalName returns the tmux wait-for signal name for a directory.
func ExitSignalName(dirName string) string {
	return "csm-exit-" + dirName
}

// WaitForExit blocks until claude exits in the given directory window.
// Returns the directory name when the signal fires.
func WaitForExit(dirName string) error {
	return exec.Command("tmux", "wait-for", ExitSignalName(dirName)).Run()
}

// CleanupExitedWindow handles cleanup when claude exits in a directory.
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

// CapturePaneContent captures the visible content of a directory's pane.
func CapturePaneContent(dirName string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(dirName))
	}
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetPaneTitle reads the tmux pane title for a directory's pane.
// Claude Code sets the pane title to indicate its current state.
func GetPaneTitle(dirName string, isDisplayed bool) (string, error) {
	var target string
	if isDisplayed {
		target = fmt.Sprintf("%s:%s.1", SessionName, MainWindow)
	} else {
		target = fmt.Sprintf("%s:%s.0", SessionName, WindowName(dirName))
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

// TerminalWindowName returns the tmux window name for a directory's terminal.
func TerminalWindowName(dirName string) string {
	return "term-" + dirName
}

// TermExitSignalName returns the tmux wait-for signal name for a terminal.
func TermExitSignalName(dirName string) string {
	return "csm-term-exit-" + dirName
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

// ListDirectoryWindows returns directory names (without "wt-" prefix) of all active directory windows.
func ListDirectoryWindows() []string {
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
