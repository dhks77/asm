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
