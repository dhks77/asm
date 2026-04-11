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
	RepoName string
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
	dirPath  string
	repoDir  string
	repoName string
	width    int
	height   int
	err      string
}

func NewWorktreeDialogModel() WorktreeDialogModel {
	return WorktreeDialogModel{
		maxVisible: 10,
	}
}

func (m *WorktreeDialogModel) Show(rootPath, dirPath string) tea.Cmd {
	m.visible = true
	m.mode = wtModeSelectBranch
	m.filter = ""
	m.cursor = 0
	m.scrollOffset = 0
	m.newBranchName = ""
	m.baseBranch = ""
	m.rootPath = rootPath
	m.dirPath = dirPath
	m.err = ""
	m.branches = nil
	m.filtered = nil

	return func() tea.Msg {
		branches, err := worktree.ListBranches(dirPath)
		if err != nil {
			return BranchesLoadedMsg{Err: err}
		}
		repoName := worktree.RepoName(dirPath)
		return BranchesLoadedMsg{Branches: branches, RepoDir: dirPath, RepoName: repoName}
	}
}

func (m *WorktreeDialogModel) Hide() {
	m.visible = false
	m.branches = nil
	m.filtered = nil
}

func (m *WorktreeDialogModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	// title(1) + repo(1) + blank(1) + filter(1) + blank(1) + status_bar(1) + margin(2) = 8
	m.maxVisible = h - 8
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
		m.repoName = msg.RepoName
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

	case "tab":
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

// WorktreeRunnerModel wraps WorktreeDialogModel for standalone use in the working panel.
type WorktreeRunnerModel struct {
	dialog  WorktreeDialogModel
	initCmd tea.Cmd
	Created bool
	err     string
	width   int
	height  int
}

func NewWorktreeRunnerModel(rootPath, dirPath string) WorktreeRunnerModel {
	d := NewWorktreeDialogModel()
	cmd := d.Show(rootPath, dirPath)
	return WorktreeRunnerModel{dialog: d, initCmd: cmd}
}

func (m WorktreeRunnerModel) Init() tea.Cmd {
	return m.initCmd
}

func (m WorktreeRunnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dialog.SetSize(msg.Width, msg.Height)
		return m, nil
	case WorktreeCreatedMsg:
		m.Created = true
		return m, tea.Quit
	case WorktreeCancelledMsg:
		return m, tea.Quit
	case WorktreeErrorMsg:
		m.err = msg.Err
		return m, nil
	case tea.KeyMsg:
		if m.err != "" {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.dialog, cmd = m.dialog.Update(msg)
	return m, cmd
}

func (m WorktreeRunnerModel) View() string {
	if m.err != "" {
		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")).
			Padding(1, 2).
			Render("Error")

		body := lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("255")).
			Render(m.err)

		hint := statusBarStyle.Render("Press any key to dismiss")

		content := title + "\n\n" + body + "\n\n" + hint
		lines := lipgloss.Height(content)
		for lines < m.height-3 {
			content += "\n"
			lines++
		}

		statusBar := statusBarStyle.
			Width(m.width).
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Render(" Press any key to close")

		return content + "\n" + statusBar
	}
	return m.dialog.View()
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

func (m WorktreeDialogModel) repoLine() string {
	name := m.repoName
	if name == "" {
		name = filepath.Base(m.dirPath)
	}
	return lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render("repo: ") +
		lipgloss.NewStyle().Foreground(secondaryColor).Render(name)
}

func (m WorktreeDialogModel) renderFullScreen(title, subtitle, filterLine string, rows []string, hint string) string {
	titleStr := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(1, 2).
		Render(title)

	repo := m.repoLine()

	content := titleStr + "\n" + repo
	if subtitle != "" {
		content += "\n" + lipgloss.NewStyle().Padding(0, 2).Foreground(dimColor).Render(subtitle)
	}

	filterStr := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.NewStyle().Foreground(dimColor).Render("/ ") + filterLine +
			lipgloss.NewStyle().Foreground(primaryColor).Render("▎"),
	)

	content += "\n\n" + filterStr + "\n\n"
	for _, row := range rows {
		content += "  " + row + "\n"
	}

	lines := lipgloss.Height(content)
	contentHeight := m.height - 3
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	statusBar := statusBarStyle.
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(" " + hint)

	return content + "\n" + statusBar
}

func (m WorktreeDialogModel) viewSelectBranch() string {
	var rows []string
	if m.err != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.err))
	} else if len(m.branches) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("Loading branches..."))
	} else if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("No matching branches"))
	} else {
		if m.scrollOffset > 0 {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↑ more"))
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
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↓ more"))
		}
	}

	return m.renderFullScreen("Create Worktree", "", m.filter, rows,
		"↑↓: navigate  Enter: checkout  Tab: new branch  Esc: cancel")
}

func (m WorktreeDialogModel) viewSelectBase() string {
	var rows []string
	if len(m.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("No matching branches"))
	} else {
		if m.scrollOffset > 0 {
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↑ more"))
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
			rows = append(rows, lipgloss.NewStyle().Foreground(dimColor).Render("↓ more"))
		}
	}

	return m.renderFullScreen("New Branch",
		"Select a base branch to create a new branch from",
		m.filter, rows,
		"↑↓: navigate  Enter: select base  Esc: back")
}

func (m WorktreeDialogModel) viewNewBranch() string {
	titleStr := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(1, 2).
		Render("New Branch")

	repo := m.repoLine()

	baseLine := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.NewStyle().Foreground(dimColor).Render("Base: ") +
			lipgloss.NewStyle().Foreground(whiteColor).Render(m.baseBranch),
	)

	nameInput := lipgloss.NewStyle().Padding(0, 2).Render(
		lipgloss.NewStyle().Foreground(dimColor).Render("Name: ") +
			m.newBranchName +
			lipgloss.NewStyle().Foreground(primaryColor).Render("▎"),
	)

	content := titleStr + "\n" + repo + "\n\n" + baseLine + "\n\n" + nameInput

	lines := lipgloss.Height(content)
	contentHeight := m.height - 3
	for lines < contentHeight {
		content += "\n"
		lines++
	}

	statusBar := statusBarStyle.
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Render(" Enter: create  Esc: back")

	return content + "\n" + statusBar
}
