package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ProjectRoot resolves the directory that owns the target-local config for a
// given target path.
func ProjectRoot(targetPath string) string {
	root, _ := ProjectIdentity(targetPath)
	return root
}

// ProjectIdentity resolves the directory that owns target-local config plus
// the human-facing repository/group name used in UI grouping.
func ProjectIdentity(targetPath string) (string, string) {
	clean := filepath.Clean(targetPath)
	if clean == "" || clean == "." {
		return clean, clean
	}
	if gitInfo, ok := gitProjectMeta(clean); ok {
		return gitInfo.Root, gitInfo.Name
	}
	if root, ok := nearestProjectRoot(clean); ok {
		return root, filepath.Base(root)
	}
	return clean, filepath.Base(clean)
}

type gitProjectInfo struct {
	Root string
	Name string
}

func gitProjectMeta(path string) (gitProjectInfo, bool) {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return gitProjectInfo{}, false
	}

	commonDir := ""
	if info.IsDir() {
		commonDir = filepath.Clean(gitPath)
		root := filepath.Clean(path)
		return gitProjectInfo{
			Root: root,
			Name: repoNameFromGitConfig(commonDir, root),
		}, true
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return gitProjectInfo{}, false
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir:") {
		return gitProjectInfo{}, false
	}

	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if gitDir == "" {
		return gitProjectInfo{}, false
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(path, gitDir)
	}
	gitDir = filepath.Clean(gitDir)

	commonDir = gitDir
	if resolved, ok := resolveGitCommonDir(gitDir); ok {
		commonDir = resolved
	} else if resolved, ok := trimLinkedWorktreeGitDir(gitDir); ok {
		commonDir = resolved
	}

	root, ok := projectRootFromCommonGitDir(commonDir)
	if !ok {
		return gitProjectInfo{}, false
	}
	return gitProjectInfo{
		Root: root,
		Name: repoNameFromGitConfig(commonDir, root),
	}, true
}

func resolveGitCommonDir(gitDir string) (string, bool) {
	commondirPath := filepath.Join(gitDir, "commondir")
	data, err := os.ReadFile(commondirPath)
	if err != nil {
		return "", false
	}
	commonDir := strings.TrimSpace(string(data))
	if commonDir == "" {
		return "", false
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitDir, commonDir)
	}
	return filepath.Clean(commonDir), true
}

func trimLinkedWorktreeGitDir(gitDir string) (string, bool) {
	worktreesSegment := string(filepath.Separator) + ".git" + string(filepath.Separator) + "worktrees" + string(filepath.Separator)
	if idx := strings.Index(gitDir, worktreesSegment); idx >= 0 {
		return filepath.Clean(gitDir[:idx] + string(filepath.Separator) + ".git"), true
	}
	genericWorktreesSegment := string(filepath.Separator) + "worktrees" + string(filepath.Separator)
	if idx := strings.Index(gitDir, genericWorktreesSegment); idx >= 0 {
		return filepath.Clean(gitDir[:idx]), true
	}
	return gitDir, true
}

func projectRootFromCommonGitDir(commonDir string) (string, bool) {
	base := filepath.Base(commonDir)
	switch {
	case base == ".git":
		return filepath.Dir(commonDir), true
	case strings.HasPrefix(base, "."):
		return filepath.Dir(commonDir), true
	case filepath.Base(filepath.Dir(commonDir)) == ".git":
		return filepath.Dir(filepath.Dir(commonDir)), true
	default:
		return filepath.Clean(commonDir), true
	}
}

func repoNameFromGitConfig(commonDir, fallbackRoot string) string {
	configPath := filepath.Join(commonDir, "config")
	data, err := os.ReadFile(configPath)
	if err == nil {
		if url := parseOriginURL(string(data)); url != "" {
			if name := repoNameFromRemoteURL(url); name != "" {
				return name
			}
		}
	}
	return filepath.Base(fallbackRoot)
}

func parseOriginURL(data string) string {
	scanner := bufio.NewScanner(strings.NewReader(data))
	currentSection := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if currentSection != `remote "origin"` {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "url" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func repoNameFromRemoteURL(url string) string {
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}
	if idx := strings.LastIndex(trimmed, ":"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return ""
}

func nearestProjectRoot(path string) (string, bool) {
	cur := filepath.Clean(path)
	for {
		candidate := filepath.Join(cur, ".asm", "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			return cur, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}
