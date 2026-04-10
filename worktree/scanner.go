package worktree

import (
	"os"
	"path/filepath"
	"sort"
)

type Worktree struct {
	Name string
	Path string
}

// Scan returns all subdirectories under root that contain a .git file or directory
// (git worktrees have a .git file pointing to the main repo).
func Scan(root string) ([]Worktree, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name()[0] == '.' {
			continue
		}
		dirPath := filepath.Join(root, entry.Name())
		gitPath := filepath.Join(dirPath, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			worktrees = append(worktrees, Worktree{
				Name: entry.Name(),
				Path: dirPath,
			})
		}
	}

	sort.Slice(worktrees, func(i, j int) bool {
		return worktrees[i].Name < worktrees[j].Name
	})

	return worktrees, nil
}
