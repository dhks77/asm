package worktree

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Worktree struct {
	Name string
	Path string
}

// IsWorktree returns true if the directory is a git worktree
// (has a .git file pointing to the main repo, rather than a .git directory).
func IsWorktree(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// IsRepoMode reports whether path looks like a git working tree (main repo or
// linked worktree). Used by asm to pick the listing strategy: when true, the
// picker sources entries from `git worktree list`; when false, it falls back
// to scanning subdirectories of path as a flat collection of unrelated
// repos/worktrees.
func IsRepoMode(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
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

// ScanRepo returns worktrees registered with the repo that owns repoPath
// (which may be the main repo or any linked worktree). Sources entries from
// `git worktree list --porcelain`, so it finds worktrees anywhere on disk —
// not just under repoPath. The main working tree is included as the first
// entry; bare repo entries are skipped. Results are sorted by Name for stable
// rendering.
//
// Stale entries (git metadata exists but the worktree directory has been
// deleted on disk — e.g., removed with rm -rf or by an older asm build
// that bypassed `git worktree remove`) are filtered out. Git itself only
// cleans these up on `git worktree prune`; asm stays read-only and just
// hides them from the picker.
func ScanRepo(repoPath string) ([]Worktree, error) {
	entries, err := ListRepoWorktrees(repoPath)
	if err != nil {
		return nil, err
	}
	var worktrees []Worktree
	for _, e := range entries {
		if e.Bare {
			continue
		}
		if _, err := os.Stat(e.Path); err != nil {
			continue // stale: git knows about it but the directory is gone
		}
		worktrees = append(worktrees, Worktree{
			Name: filepath.Base(e.Path),
			Path: e.Path,
		})
	}
	sort.Slice(worktrees, func(i, j int) bool {
		return worktrees[i].Name < worktrees[j].Name
	})
	return worktrees, nil
}

// MostRecentLinkedWorktreeParent returns the parent directory of the
// most-recently-modified LINKED worktree for the repo that owns rootPath,
// comparing by on-disk mtime. The main worktree is excluded — we want the
// location where the user has been dropping NEW worktrees, not where the
// repo was originally cloned. Bare entries and stale paths are filtered by
// ScanRepo upstream. Returns "" when no linked worktree is found (fresh
// repo, or only a main tree exists) so callers can fall through to their
// own defaults.
//
// Used by:
//   - resolveWorktreeBase in the worktree dialog, to pick a base for new
//     worktrees that matches the user's current layout.
//   - the auto-seed step on first repo-mode entry, to persist that same
//     layout as the project's worktree_base_path.
func MostRecentLinkedWorktreeParent(rootPath string) string {
	entries, err := ScanRepo(rootPath)
	if err != nil {
		return ""
	}
	mainRepo, _ := FindMainRepo(rootPath)
	mainClean := ""
	if mainRepo != "" {
		mainClean = filepath.Clean(mainRepo)
	}

	var bestPath string
	var bestMtime time.Time
	for _, wt := range entries {
		if mainClean != "" && filepath.Clean(wt.Path) == mainClean {
			continue
		}
		info, err := os.Stat(wt.Path)
		if err != nil {
			continue
		}
		if bestPath == "" || info.ModTime().After(bestMtime) {
			bestPath = wt.Path
			bestMtime = info.ModTime()
		}
	}
	if bestPath == "" {
		return ""
	}
	return filepath.Dir(bestPath)
}
