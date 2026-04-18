package worktree

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhn/asm/platform"
)

// ConflictPolicy controls how ApplyTemplate handles files that already exist
// at the destination.
type ConflictPolicy string

const (
	ConflictSkip      ConflictPolicy = "skip"
	ConflictOverwrite ConflictPolicy = "overwrite"
)

// TemplateResult reports how ApplyTemplate completed.
type TemplateResult struct {
	Copied   int      // number of files successfully copied
	Skipped  int      // files skipped due to conflict policy
	Warnings []string // human-readable notes about files that could not be copied
}

// TemplatesRoot returns the templates directory for a given project root.
func TemplatesRoot(projectRoot string) string {
	return filepath.Join(projectRoot, ".asm", "templates")
}

// TemplateDirForRepo returns the template directory for a specific repo name.
// repoName is sanitised via filepath.Base to prevent directory traversal.
func TemplateDirForRepo(projectRoot, repoName string) string {
	name := filepath.Base(repoName)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	return filepath.Join(TemplatesRoot(projectRoot), name)
}

// ApplyTemplate copies files from {projectRoot}/.asm/templates/{repoName}/ to
// targetPath, preserving relative paths. Only regular files are copied;
// directories are treated as path components (created on demand via MkdirAll)
// and symlinks are ignored. The conflict policy controls behaviour when a
// destination file already exists.
//
// The returned TemplateResult is always non-nil so callers can surface partial
// progress even when err != nil.
func ApplyTemplate(projectRoot, repoName, targetPath string, policy ConflictPolicy) (TemplateResult, error) {
	result := TemplateResult{}

	if repoName == "" {
		result.Warnings = append(result.Warnings, "repo name could not be resolved; skipping template copy")
		return result, nil
	}
	srcRoot := TemplateDirForRepo(projectRoot, repoName)
	if srcRoot == "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("invalid repo name %q; skipping template copy", repoName))
		return result, nil
	}

	info, err := os.Stat(srcRoot)
	if os.IsNotExist(err) {
		// Auto-create the per-repo template directory so the user has an
		// obvious place to drop files into. The directory will simply be
		// empty on first worktree creation; subsequent creations pick up
		// whatever files the user has added.
		if mkErr := os.MkdirAll(srcRoot, 0o755); mkErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("cannot create template dir %s: %v", srcRoot, mkErr))
		}
		return result, nil
	}
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot stat template dir %s: %v", srcRoot, err))
		return result, nil
	}
	if !info.IsDir() {
		result.Warnings = append(result.Warnings, fmt.Sprintf("template path %s is not a directory; skipping", srcRoot))
		return result, nil
	}

	if policy != ConflictOverwrite {
		policy = ConflictSkip
	}

	walkErr := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("walk %s: %v", path, err))
			return nil
		}
		rel, relErr := filepath.Rel(srcRoot, path)
		if relErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("rel %s: %v", path, relErr))
			return nil
		}
		if rel == "." {
			return nil
		}
		// Safety: never descend into .git anywhere under the template.
		topSegment := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if topSegment == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil // directories are traversal only; created on demand
		}
		// Only copy regular files. Ignore symlinks, devices, sockets, etc.
		if !d.Type().IsRegular() {
			return nil
		}

		dst := filepath.Join(targetPath, rel)
		copied, skipped, warning := copyTemplateFile(path, dst, policy)
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
		if copied {
			result.Copied++
		} else if skipped {
			result.Skipped++
		}
		return nil
	})
	if walkErr != nil {
		return result, walkErr
	}
	return result, nil
}

// DiscoverRepoNames walks projectRoot for git worktrees/repos and returns the
// unique repo names (as resolved by RepoName). Unresolvable entries are
// silently dropped. Useful for pre-seeding the templates tree.
func DiscoverRepoNames(projectRoot string) []string {
	wts, err := Scan(projectRoot)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, w := range wts {
		name := RepoName(w.Path)
		if name == "" {
			continue
		}
		// Sanitise the same way TemplateDirForRepo will.
		name = filepath.Base(name)
		if name == "" || name == "." || name == string(filepath.Separator) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// OpenTemplatesDir ensures {projectRoot}/.asm/templates/ exists and reveals it
// in the platform's file explorer so the user can drop template files in.
// Every repo discovered under projectRoot gets its own pre-created subdirectory
// so the user has a ready-to-fill folder per repo. Returns the templates root
// plus any error from directory creation; the file explorer launch is
// best-effort and its failure is not returned.
func OpenTemplatesDir(projectRoot string) (string, error) {
	dir := TemplatesRoot(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dir, err
	}
	// Pre-create a folder for every repo we can identify under projectRoot
	// so the user sees one entry per repo rather than an empty root.
	for _, repo := range DiscoverRepoNames(projectRoot) {
		_ = os.MkdirAll(filepath.Join(dir, repo), 0o755)
	}
	_ = platform.Current().RevealPath(dir) // best-effort; errors surface via the user not seeing a window
	return dir, nil
}

// copyTemplateFile copies src to dst following the given conflict policy.
// Returns (copied, skipped, warning). `warning` is empty on a clean copy or an
// uninteresting skip.
func copyTemplateFile(src, dst string, policy ConflictPolicy) (bool, bool, string) {
	dstInfo, err := os.Lstat(dst)
	switch {
	case os.IsNotExist(err):
		// proceed
	case err != nil:
		return false, false, fmt.Sprintf("stat %s: %v", dst, err)
	case dstInfo.IsDir():
		return false, true, fmt.Sprintf("skipped %s: destination is a directory", dst)
	default:
		if policy != ConflictOverwrite {
			return false, true, ""
		}
	}

	// Ensure parent directories exist. If any intermediate path is a regular
	// file, MkdirAll returns an error — surface that as a warning.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, false, fmt.Sprintf("mkdir %s: %v", filepath.Dir(dst), err)
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, false, fmt.Sprintf("stat %s: %v", src, err)
	}

	in, err := os.Open(src)
	if err != nil {
		return false, false, fmt.Sprintf("open %s: %v", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return false, false, fmt.Sprintf("create %s: %v", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return false, false, fmt.Sprintf("copy %s -> %s: %v", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return false, false, fmt.Sprintf("close %s: %v", dst, err)
	}
	// Preserve file mode (O_CREATE respects the mode only on first create;
	// explicitly chmod to keep behaviour consistent on overwrite).
	if err := os.Chmod(dst, srcInfo.Mode().Perm()); err != nil {
		return true, false, fmt.Sprintf("chmod %s: %v", dst, err)
	}
	return true, false, ""
}
