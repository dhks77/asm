package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Branch struct {
	Name        string
	IsLocal     bool
	HasWorktree bool
}

// FindGitRepo finds any git repository under the root path that can be used for git operations.
func FindGitRepo(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '.' {
			continue
		}
		dirPath := filepath.Join(root, entry.Name())
		gitPath := filepath.Join(dirPath, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return dirPath, nil
		}
	}
	return "", fmt.Errorf("no git repository found under %s", root)
}

// ListBranches lists all branches (local + remote) from a git repo.
func ListBranches(repoDir string) ([]Branch, error) {
	out, err := runGit(repoDir, "branch", "-a", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}

	wtBranches := listWorktreeBranches(repoDir)

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
	out, err := runGit(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	result := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "branch refs/heads/") {
			branch := strings.TrimPrefix(line, "branch refs/heads/")
			result[branch] = true
		}
	}
	return result
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

// RemoveWorktree removes a git worktree by path.
func RemoveWorktree(repoDir, targetPath string) error {
	_, err := runGit(repoDir, "worktree", "remove", targetPath)
	return err
}

// BranchToFolderName converts a branch name to a suitable folder name.
func BranchToFolderName(branch string) string {
	name := strings.TrimPrefix(branch, "origin/")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}
