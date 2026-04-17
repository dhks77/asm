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
	projectCfgs := make(map[string]*config.Config)
	migratedLegacyLabels := make(map[string]bool)
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

		if _, ok := repoColors[root]; ok {
			continue
		}

		projectCfg := projectCfgs[root]
		if projectCfg == nil {
			projectCfg, _ = config.LoadScope(config.ScopeProject, root)
			if projectCfg == nil {
				projectCfg = config.DefaultConfig()
			}
			projectCfgs[root] = projectCfg
		}

		color := strings.TrimSpace(projectCfg.RepoColor)
		if color == "" {
			color = strings.TrimSpace(userCfg.RepoColors[label])
			if color != "" {
				projectCfg.RepoColor = color
				_ = config.SaveScope(projectCfg, config.ScopeProject, root)
				migratedLegacyLabels[label] = true
			}
		}
		if color == "" {
			color = generatedRepoColorValue(label)
		}
		repoColors[root] = color
	}

	if len(migratedLegacyLabels) > 0 && len(userCfg.RepoColors) > 0 {
		for label := range migratedLegacyLabels {
			delete(userCfg.RepoColors, label)
		}
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
	for root, color := range colors {
		repoColors[root] = color
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
