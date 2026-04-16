package ui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/config"
)

func repoMetadataForPaths(paths []string) (map[string]string, map[string]string) {
	repoRoots := make(map[string]string, len(paths))
	repoColors := make(map[string]string)
	userCfg, _ := config.LoadScope(config.ScopeUser, "")
	if userCfg == nil {
		userCfg = config.DefaultConfig()
	}
	if userCfg.RepoColors == nil {
		userCfg.RepoColors = make(map[string]string)
	}

	dirty := false
	for _, path := range paths {
		_, label := config.ProjectIdentity(path)
		if label == "" || label == "." {
			label = filepath.Base(path)
		}
		repoRoots[path] = label

		color := strings.TrimSpace(userCfg.RepoColors[label])
		if color == "" {
			color = generatedRepoColorValue(label)
			userCfg.RepoColors[label] = color
			dirty = true
		}
		repoColors[label] = color
	}

	if dirty {
		_ = config.SaveScope(userCfg, config.ScopeUser, "")
	}

	return repoRoots, repoColors
}

func mergeRepoMetadataForPaths(paths []string, repoRoots, repoColors map[string]string) {
	if len(paths) == 0 {
		return
	}
	roots, colors := repoMetadataForPaths(paths)
	for path, label := range roots {
		repoRoots[path] = label
	}
	for label, color := range colors {
		repoColors[label] = color
	}
}

func (m *PickerModel) repoRootForPath(path string) string {
	if root := m.repoRoots[path]; root != "" {
		return root
	}
	return filepath.Base(path)
}

func (m *PickerModel) repoLabelForPath(path string) string {
	return m.repoRootForPath(path)
}

func (m *PickerModel) repoAccentForPath(path string) lipgloss.Color {
	root := m.repoRootForPath(path)
	label := m.repoLabelForPath(path)
	return resolveRepoAccentColor(label, m.repoColors[root])
}

func (m *PickerModel) renderRepoHeader(path string) string {
	label := m.repoLabelForPath(path)
	color := m.repoAccentForPath(path)
	return lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(color).
		Bold(true).
		Render(label)
}
