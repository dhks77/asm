package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhn/csm/config"
	csmtmux "github.com/nhn/csm/tmux"
	"github.com/nhn/csm/ui"
)

func main() {
	pathFlag := flag.String("path", "", "Root directory containing directories")
	pickerMode := flag.Bool("picker", false, "Run in picker mode (inside tmux picking panel)")
	settingsMode := flag.Bool("settings", false, "Run in settings mode (inside tmux working panel)")
	deleteMode := flag.String("delete", "", "Run delete confirmation (directory name)")
	deleteTaskName := flag.String("delete-task", "", "Task name to display in delete confirmation")
	deleteDirty := flag.Bool("delete-dirty", false, "Directory has uncommitted changes")
	deleteWorktree := flag.Bool("delete-worktree", false, "Directory is a git worktree")
	worktreeCreate := flag.Bool("worktree-create", false, "Run worktree creation dialog")
	worktreeDir := flag.String("worktree-dir", "", "Directory path for worktree operations")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

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

	if *deleteMode != "" {
		runDelete(*deleteMode, *deleteTaskName, *deleteDirty, *deleteWorktree)
	} else if *worktreeCreate {
		runWorktreeCreate(rootPath, *worktreeDir)
	} else if *settingsMode {
		runSettings(rootPath)
	} else if *pickerMode {
		runPicker(cfg, rootPath)
	} else {
		runOrchestrator(cfg, rootPath)
	}
}

func runOrchestrator(cfg *config.Config, rootPath string) {
	if !csmtmux.IsAvailable() {
		fmt.Fprintln(os.Stderr, "Error: tmux is required. Install it with: brew install tmux")
		os.Exit(1)
	}

	// If already inside the csm tmux session, run picker directly
	if csmtmux.IsInsideTmux() && csmtmux.SessionExists() {
		runPicker(cfg, rootPath)
		return
	}

	// Kill existing session if any
	if csmtmux.SessionExists() {
		csmtmux.KillSession()
	}

	// Get current executable path for picker command
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding executable: %v\n", err)
		os.Exit(1)
	}

	// Create tmux session (starts with default shell)
	pickerCmd := fmt.Sprintf("%s --picker --path %s", exe, rootPath)
	if err := csmtmux.CreateSession(pickerCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating tmux session: %v\n", err)
		os.Exit(1)
	}

	// Send the picker command to the main pane
	if err := csmtmux.SendPickerCommand(pickerCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending picker command: %v\n", err)
		os.Exit(1)
	}

	// Create the working panel (placeholder - 'cat' keeps it alive)
	if err := csmtmux.SplitWorkingPanel(70); err != nil {
		fmt.Fprintf(os.Stderr, "Error splitting pane: %v\n", err)
		os.Exit(1)
	}

	// Focus the picking panel
	csmtmux.FocusPickingPanel()

	// Attach to the session (blocks until session ends)
	if err := csmtmux.Attach(); err != nil {
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

func runWorktreeCreate(rootPath, dirPath string) {
	model := ui.NewWorktreeRunnerModel(rootPath, dirPath)
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

func runSettings(rootPath string) {
	model := ui.NewSettingsModel(rootPath)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runPicker(cfg *config.Config, rootPath string) {
	model := ui.NewPickerModel(cfg, rootPath)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithReportFocus())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
