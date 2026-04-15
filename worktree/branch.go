package worktree

import (
	"path/filepath"
	"strings"
)

type Branch struct {
	Name        string
	IsLocal     bool
	HasWorktree bool
}

// RepoName returns the repository name from the remote origin URL.
// Falls back to the main repo directory name if no remote is configured.
func RepoName(dir string) string {
	out, err := runGit(dir, "remote", "get-url", "origin")
	if err == nil {
		url := strings.TrimSpace(out)
		url = strings.TrimSuffix(url, ".git")
		if i := strings.LastIndex(url, "/"); i >= 0 {
			return url[i+1:]
		}
		if i := strings.LastIndex(url, ":"); i >= 0 {
			return url[i+1:]
		}
	}
	mainRepo, err := FindMainRepo(dir)
	if err == nil {
		return filepath.Base(mainRepo)
	}
	return ""
}

// FindMainRepo returns the main repository directory for a git directory (worktree or main repo).
func FindMainRepo(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	gitCommon := strings.TrimSpace(out)
	if !filepath.IsAbs(gitCommon) {
		gitCommon = filepath.Join(dir, gitCommon)
	}
	return filepath.Dir(filepath.Clean(gitCommon)), nil
}

// ListBranches lists all branches (local + remote) from a git repo.
func ListBranches(repoDir string) ([]Branch, error) {
	out, err := runGit(repoDir, "branch", "-a", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}

	wtBranches := listWorktreeBranches(repoDir)
	// Mark remote counterparts of worktree branches too
	for branch := range wtBranches {
		wtBranches["origin/"+branch] = true
	}

	var branches []Branch
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "->") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		branches = append(branches, Branch{
			Name:        line,
			IsLocal:     !strings.HasPrefix(line, "origin/"),
			HasWorktree: wtBranches[line],
		})
	}
	return branches, nil
}

func listWorktreeBranches(repoDir string) map[string]bool {
	entries, err := ListRepoWorktrees(repoDir)
	if err != nil {
		return nil
	}
	result := make(map[string]bool)
	for _, e := range entries {
		if e.Branch != "" {
			result[e.Branch] = true
		}
	}
	return result
}

// WorktreeListEntry is one parsed entry from `git worktree list --porcelain`.
// Branch is empty when the worktree is detached or bare.
type WorktreeListEntry struct {
	Path     string
	Branch   string
	Detached bool
	Bare     bool
}

// ListRepoWorktrees runs `git worktree list --porcelain` from any worktree of
// a repo (main or linked) and returns all entries registered with that repo.
// The first entry is the main working tree; linked worktrees follow.
func ListRepoWorktrees(repoDir string) ([]WorktreeListEntry, error) {
	out, err := runGit(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeListPorcelain(out), nil
}

// parseWorktreeListPorcelain parses the stanza-format output of
// `git worktree list --porcelain` into WorktreeListEntry values. Entries are
// separated by blank lines; each has a `worktree <path>` header plus optional
// `branch refs/heads/<name>`, `detached`, or `bare` lines.
func parseWorktreeListPorcelain(out string) []WorktreeListEntry {
	var entries []WorktreeListEntry
	var cur WorktreeListEntry
	flush := func() {
		if cur.Path != "" {
			entries = append(entries, cur)
		}
		cur = WorktreeListEntry{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			cur.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		}
	}
	flush() // handle trailing entry with no blank-line terminator
	return entries
}

// CreateWorktreeFromBranch creates a new worktree checking out an existing branch.
// For remote branches (origin/...), it creates a local tracking branch automatically.
func CreateWorktreeFromBranch(repoDir, targetPath, branch string) error {
	if strings.HasPrefix(branch, "origin/") {
		localName := strings.TrimPrefix(branch, "origin/")
		_, err := runGit(repoDir, "worktree", "add", "-b", localName, targetPath, branch)
		return err
	}
	_, err := runGit(repoDir, "worktree", "add", targetPath, branch)
	return err
}

// CreateWorktreeNewBranch creates a new worktree with a new branch based on a base branch.
func CreateWorktreeNewBranch(repoDir, targetPath, newBranch, baseBranch string) error {
	_, err := runGit(repoDir, "worktree", "add", "-b", newBranch, targetPath, baseBranch)
	return err
}

// RemoveWorktree removes a git worktree by path, using --force if needed.
func RemoveWorktree(repoDir, targetPath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, targetPath)
	_, err := runGit(repoDir, args...)
	return err
}

// BranchToFolderName converts a branch name to a suitable folder name.
func BranchToFolderName(branch string) string {
	name := strings.TrimPrefix(branch, "origin/")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}
