package ui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nhn/asm/config"
)

func repoMetadataForPaths(paths []string) (map[string]string, map[string]string, map[string]string) {
	repoRoots := make(map[string]string, len(paths))
	repoLabels := make(map[string]string, len(paths))
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
		root, label := config.ProjectIdentity(path)
		if root == "" || root == "." {
			root = filepath.Clean(path)
		}
		if label == "" || label == "." {
			label = filepath.Base(path)
		}
		repoRoots[path] = root
		repoLabels[path] = label

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

	return repoRoots, repoLabels, repoColors
}

func mergeRepoMetadataForPaths(paths []string, repoRoots, repoLabels, repoColors map[string]string) {
	if len(paths) == 0 {
		return
	}
	roots, labels, colors := repoMetadataForPaths(paths)
	for path, root := range roots {
		repoRoots[path] = root
	}
	for path, label := range labels {
		repoLabels[path] = label
	}
	for label, color := range colors {
		repoColors[label] = color
	}
}

func (m *PickerModel) repoRootForPath(path string) string {
	if root := m.repoRoots[path]; root != "" {
		return root
	}
	return filepath.Clean(path)
}

func (m *PickerModel) repoLabelForPath(path string) string {
	if label := m.repoLabels[path]; label != "" {
		return label
	}
	return filepath.Base(path)
}

func (m *PickerModel) repoAccentForPath(path string) lipgloss.Color {
	label := m.repoLabelForPath(path)
	return resolveRepoAccentColor(label, m.repoColors[label])
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
