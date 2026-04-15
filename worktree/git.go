package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// gitCommandTimeout bounds every git subprocess we spawn so a stuck lock or
// slow filesystem can't leak a goroutine forever. Tuned to be well above the
// normal command latency but short enough that hangs surface quickly.
const gitCommandTimeout = 5 * time.Second

// gitFetchTimeout is the longer deadline used for `git fetch`, which hits
// the network and is bounded by the remote's responsiveness, not local
// filesystem latency. 30s lets typical fetches complete while still failing
// fast on a dead remote.
const gitFetchTimeout = 30 * time.Second

// CurrentBranch returns the current branch name of the git work tree at dir,
// or an empty string if it cannot be resolved.
func CurrentBranch(dir string) string {
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// HasChanges returns true if the work tree has any modified, staged or
// untracked files. Used on-demand (e.g. when confirming a delete); not part
// of any background polling.
func HasChanges(dir string) bool {
	out, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s timed out after %s", strings.Join(args, " "), gitCommandTimeout)
		}
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return string(out), nil
}
