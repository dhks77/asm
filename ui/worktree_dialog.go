package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/csm/worktree"
)

type worktreeDialogMode int

const (
	wtModeSelectBranch worktreeDialogMode = iota
	wtModeSelectBase
	wtModeNewBranch
)

// Messages

type BranchesLoadedMsg struct {
	Branches []worktree.Branch
	RepoDir  string
	Err      error
}

type WorktreeCreatedMsg struct {
	Name string
	Path string
}

type WorktreeCancelledMsg struct{}

type WorktreeErrorMsg struct {
	Err string
}

// WorktreeDialogModel handles branch selection and worktree creation.
type WorktreeDialogModel struct {
	visible      bool
	mode         worktreeDialogMode
	branches     []worktree.Branch
	filtered     []worktree.Branch
	filter       string
	cursor       int
	scrollOffset int
	maxVisible   int

	newBranchName string
	baseBranch    string

	rootPath string
	repoDir  string
	width    int
	height   int
	err      string
}

func NewWorktreeDialogModel() WorktreeDialogModel {
	return WorktreeDialogModel{
		maxVisible: 10,
	}
}

func (m *WorktreeDialogModel) Show(rootPath string) tea.Cmd {
	m.visible = true
	m.mode = wtModeSelectBranch
	m.filter = ""
	m.cursor = 0
	m.scrollOffset = 0
	m.newBranchName = ""
	m.baseBranch = ""
	m.rootPath = rootPath
	m.err = ""
	m.branches = nil
	m.filtered = nil

	return func() tea.Msg {
		repoDir, err := worktree.FindGitRepo(rootPath)
		if err != nil {
			return BranchesLoadedMsg{Err: err}
		}
		branches, err := worktree.ListBranches(repoDir)
		if err != nil {
			return BranchesLoadedMsg{Err: err}
		}
		return BranchesLoadedMsg{Branches: branches, RepoDir: repoDir}
	}
}

func (m *WorktreeDialogModel) Hide() {
	m.visible = false
	m.branches = nil
	m.filtered = nil
}

func (m WorktreeDialogModel) IsVisible() bool {
	return m.visible
}

func (m *WorktreeDialogModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.maxVisible = min(10, h-10)
	if m.maxVisible < 3 {
		m.maxVisible = 3
	}
}

func (m *WorktreeDialogModel) applyFilter() {
	hideWorktree := m.mode == wtModeSelectBranch
	lower := strings.ToLower(m.filter)
	m.filtered = nil
	for _, b := range m.branches {
		if hideWorktree && b.HasWorktree {
			continue
		}
		if m.filter != "" && !strings.Contains(strings.ToLower(b.Name), lower) {
			continue
		}
		m.filtered = append(m.filtered, b)
	}
	if len(m.filtered) == 0 {
		m.cursor = 0
		m.scrollOffset = 0
	} else if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+m.maxVisible {
		m.scrollOffset = m.cursor - m.maxVisible + 1
	}
}

func (m WorktreeDialogModel) Update(msg tea.Msg) (WorktreeDialogModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case BranchesLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err.Error()
			return m, nil
		}
		m.branches = msg.Branches
		m.repoDir = msg.RepoDir
		m.applyFilter()
		return m, nil

	case tea.KeyMsg:
		if m.mode == wtModeNewBranch {
			return m.handleNewBranchKey(msg)
		}
		if m.mode == wtModeSelectBase {
			return m.handleSelectBaseKey(msg)
		}
		return m.handleSelectBranchKey(msg)
	}

	return m, nil
}

func (m WorktreeDialogModel) handleSelectBranchKey(msg tea.KeyMsg) (WorktreeDialogModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
			return m, nil
		}
		m.Hide()
		return m, func() tea.Msg { return WorktreeCancelledMsg{} }

	case "up":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
		}

	case "down":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			if m.cursor >= m.scrollOffset+m.maxVisible {
				m.scrollOffset = m.cursor - m.maxVisible + 1
			}
		}

	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		selected := m.filtered[m.cursor]
		repoDir := m.repoDir
		rootPath := m.rootPath
		m.Hide()
		return m, createWorktreeFromBranchCmd(repoDir, rootPath, selected.Name)

	case "ctrl+n":
		m.mode = wtModeSelectBase
		m.filter = ""
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilter()

	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
		}

	case "ctrl+u":
		m.filter = ""
		m.applyFilter()

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.filter += key
			m.applyFilter()
		}
	}

	return m, nil
}

func (m WorktreeDialogModel) handleSelectBaseKey(msg tea.KeyMsg) (WorktreeDialogModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.mode = wtModeSelectBranch
		m.filter = ""
		m.cursor = 0
		m.scrollOffset = 0
		m.applyFilter()

	case "up":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.scrollOffset {
				m.scrollOffset = m.cursor
			}
		}

	case "down":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			if m.cursor >= m.scrollOffset+m.maxVisible {
				m.scrollOffset = m.cursor - m.maxVisible + 1
			}
		}

	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		m.baseBranch = m.filtered[m.cursor].Name
		m.newBranchName = ""
		m.mode = wtModeNewBranch

	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
		}

	case "ctrl+u":
		m.filter = ""
		m.applyFilter()

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.filter += key
			m.applyFilter()
		}
	}

	return m, nil
}

func (m WorktreeDialogModel) handleNewBranchKey(msg tea.KeyMsg) (WorktreeDialogModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.mode = wtModeSelectBranch
		m.newBranchName = ""

	case "enter":
		name := strings.TrimSpace(m.newBranchName)
		if name == "" {
			return m, nil
		}
		repoDir := m.repoDir
		rootPath := m.rootPath
		baseBranch := m.baseBranch
		m.Hide()
		return m, createWorktreeNewBranchCmd(repoDir, rootPath, name, baseBranch)

	case "backspace":
		if len(m.newBranchName) > 0 {
			m.newBranchName = m.newBranchName[:len(m.newBranchName)-1]
		}

	case "ctrl+u":
		m.newBranchName = ""

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.newBranchName += key
		}
	}

	return m, nil
}

func createWorktreeFromBranchCmd(repoDir, rootPath, branch string) tea.Cmd {
	return func() tea.Msg {
		folderName := worktree.BranchToFolderName(branch)
		targetPath := filepath.Join(rootPath, folderName)

		err := worktree.CreateWorktreeFromBranch(repoDir, targetPath, branch)
		if err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("worktree add failed: %v", err)}
		}
		return WorktreeCreatedMsg{Name: folderName, Path: targetPath}
	}
}

func createWorktreeNewBranchCmd(repoDir, rootPath, newBranch, baseBranch string) tea.Cmd {
	return func() tea.Msg {
		folderName := worktree.BranchToFolderName(newBranch)
		targetPath := filepath.Join(rootPath, folderName)

		err := worktree.CreateWorktreeNewBranch(repoDir, targetPath, newBranch, baseBranch)
		if err != nil {
			return WorktreeErrorMsg{Err: fmt.Sprintf("worktree add failed: %v", err)}
		}
		return WorktreeCreatedMsg{Name: folderName, Path: targetPath}
	}
}

// View

func (m WorktreeDialogModel) View() string {
	if !m.visible {
		return ""
	}
	if m.mode == wtModeNewBranch {
		return m.viewNewBranch()
	}
	if m.mode == wtModeSelectBase {
		return m.viewSelectBase()
	}
	return m.viewSelectBranch()
}

func (m WorktreeDialogModel) viewSelectBranch() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("Create Worktree")

	filterLine := lipgloss.NewStyle().
		Foreground(dimColor).
		Render("> ") + m.filter + lipgloss.NewStyle().
		Foreground(primaryColor).
		Render("▎")

	var rows []string
	if m.err != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.err))
	} else if len(m.branches) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("Loading branches..."))
	} else if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("No matching branches"))
	} else {
		if m.scrollOffset > 0 {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("  ↑ more"))
		}

		end := min(m.scrollOffset+m.maxVisible, len(m.filtered))
		for i := m.scrollOffset; i < end; i++ {
			b := m.filtered[i]
			cursor := "  "
			style := normalItemStyle
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
				style = selectedItemStyle
			}

			name := style.Render(b.Name)
			if b.HasWorktree {
				name += " " + lipgloss.NewStyle().Foreground(activeColor).Render("●")
			}
			if b.IsLocal {
				name += " " + lipgloss.NewStyle().Foreground(dimColor).Render("(local)")
			}

			rows = append(rows, cursor+name)
		}

		if end < len(m.filtered) {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("  ↓ more"))
		}
	}

	hint := statusBarStyle.Render("↑↓: navigate  Enter: checkout  Ctrl+n: new branch  Esc: cancel")

	content := title + "\n\n" + filterLine + "\n\n" + strings.Join(rows, "\n") + "\n\n" + hint

	dialogWidth := min(60, m.width-4)
	if dialogWidth < 30 {
		dialogWidth = 30
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(dialogWidth)

	return dialogStyle.Render(content)
}

func (m WorktreeDialogModel) viewSelectBase() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("Select Base Branch")

	filterLine := lipgloss.NewStyle().
		Foreground(dimColor).
		Render("> ") + m.filter + lipgloss.NewStyle().
		Foreground(primaryColor).
		Render("▎")

	var rows []string
	if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("No matching branches"))
	} else {
		if m.scrollOffset > 0 {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("  ↑ more"))
		}

		end := min(m.scrollOffset+m.maxVisible, len(m.filtered))
		for i := m.scrollOffset; i < end; i++ {
			b := m.filtered[i]
			cursor := "  "
			style := normalItemStyle
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(primaryColor).Render("▸ ")
				style = selectedItemStyle
			}
			name := style.Render(b.Name)
			if b.IsLocal {
				name += " " + lipgloss.NewStyle().Foreground(dimColor).Render("(local)")
			}
			rows = append(rows, cursor+name)
		}

		if end < len(m.filtered) {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("  ↓ more"))
		}
	}

	hint := statusBarStyle.Render("↑↓: navigate  Enter: select base  Esc: back")

	content := title + "\n\n" + filterLine + "\n\n" + strings.Join(rows, "\n") + "\n\n" + hint

	dialogWidth := min(60, m.width-4)
	if dialogWidth < 30 {
		dialogWidth = 30
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(dialogWidth)

	return dialogStyle.Render(content)
}

func (m WorktreeDialogModel) viewNewBranch() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Render("New Branch")

	baseLine := lipgloss.NewStyle().
		Foreground(dimColor).
		Render("Base: ") + lipgloss.NewStyle().
		Foreground(whiteColor).
		Render(m.baseBranch)

	nameLabel := lipgloss.NewStyle().
		Foreground(dimColor).
		Render("Name: ")
	nameInput := m.newBranchName + lipgloss.NewStyle().
		Foreground(primaryColor).
		Render("▎")

	hint := statusBarStyle.Render("Enter: create  Esc: back")

	content := title + "\n\n" + baseLine + "\n\n" + nameLabel + nameInput + "\n\n" + hint

	dialogWidth := min(60, m.width-4)
	if dialogWidth < 30 {
		dialogWidth = 30
	}

	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(dialogWidth)

	return dialogStyle.Render(content)
}
