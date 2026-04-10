package worktree

import (
	"os/exec"
	"strconv"
	"strings"
)

type GitStatus struct {
	Branch     string
	Ahead      int
	Behind     int
	Staged     int
	Unstaged   int
	Untracked  int
}

func (s GitStatus) Summary() string {
	var parts []string
	parts = append(parts, s.Branch)

	if s.Ahead > 0 {
		parts = append(parts, "↑"+strconv.Itoa(s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, "↓"+strconv.Itoa(s.Behind))
	}

	changes := s.Staged + s.Unstaged + s.Untracked
	if changes == 0 {
		parts = append(parts, "✓")
	} else {
		if s.Staged > 0 {
			parts = append(parts, "+"+strconv.Itoa(s.Staged))
		}
		if s.Unstaged > 0 {
			parts = append(parts, "~"+strconv.Itoa(s.Unstaged))
		}
		if s.Untracked > 0 {
			parts = append(parts, "?"+strconv.Itoa(s.Untracked))
		}
	}

	return strings.Join(parts, " ")
}

func GetGitStatus(dir string) GitStatus {
	status := GitStatus{}

	// Branch name
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		status.Branch = "unknown"
		return status
	}
	status.Branch = strings.TrimSpace(out)

	// Ahead/behind
	out, err = runGit(dir, "rev-list", "--left-right", "--count", status.Branch+"...@{upstream}")
	if err == nil {
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) == 2 {
			status.Ahead, _ = strconv.Atoi(parts[0])
			status.Behind, _ = strconv.Atoi(parts[1])
		}
	}

	// Porcelain status
	out, err = runGit(dir, "status", "--porcelain")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			if len(line) < 2 {
				continue
			}
			x, y := line[0], line[1]
			if x == '?' {
				status.Untracked++
			} else {
				if x != ' ' && x != '?' {
					status.Staged++
				}
				if y != ' ' && y != '?' {
					status.Unstaged++
				}
			}
		}
	}

	return status
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
