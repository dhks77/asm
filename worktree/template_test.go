package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func setupTemplate(t *testing.T, projectRoot, repoName string, files map[string]string) {
	t.Helper()
	tmplDir := TemplateDirForRepo(projectRoot, repoName)
	for rel, content := range files {
		writeFile(t, filepath.Join(tmplDir, rel), content)
	}
}

func TestApplyTemplate_NoTemplateDir_AutoCreates(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyTemplate(root, "my-repo", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 0 || res.Skipped != 0 || len(res.Warnings) != 0 {
		t.Fatalf("expected silent no-op, got %+v", res)
	}
	// Auto-create: the per-repo template dir must now exist so the user has
	// an obvious place to drop files into.
	info, err := os.Stat(TemplateDirForRepo(root, "my-repo"))
	if err != nil {
		t.Fatalf("expected template dir to be auto-created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected a directory, got mode %v", info.Mode())
	}
}

func TestApplyTemplate_CopiesFilesWithRelativePaths(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	setupTemplate(t, root, "my-repo", map[string]string{
		".env":                  "SECRET=1\n",
		".vscode/settings.json": "{\"a\":1}\n",
		"config/local.json":     "{}\n",
	})

	res, err := ApplyTemplate(root, "my-repo", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 3 {
		t.Fatalf("expected 3 copied, got %d (warnings=%v)", res.Copied, res.Warnings)
	}
	if got := readFile(t, filepath.Join(target, ".env")); got != "SECRET=1\n" {
		t.Errorf(".env content mismatch: %q", got)
	}
	if got := readFile(t, filepath.Join(target, ".vscode/settings.json")); got != "{\"a\":1}\n" {
		t.Errorf(".vscode/settings.json mismatch: %q", got)
	}
	if got := readFile(t, filepath.Join(target, "config/local.json")); got != "{}\n" {
		t.Errorf("config/local.json mismatch: %q", got)
	}
}

func TestApplyTemplate_SkipConflict(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(target, ".env"), "EXISTING\n")
	setupTemplate(t, root, "r", map[string]string{".env": "TEMPLATE\n"})

	res, err := ApplyTemplate(root, "r", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 0 || res.Skipped != 1 {
		t.Fatalf("expected skip, got %+v", res)
	}
	if got := readFile(t, filepath.Join(target, ".env")); got != "EXISTING\n" {
		t.Errorf("expected existing file untouched, got %q", got)
	}
}

func TestApplyTemplate_OverwriteConflict(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(target, ".env"), "EXISTING\n")
	setupTemplate(t, root, "r", map[string]string{".env": "TEMPLATE\n"})

	res, err := ApplyTemplate(root, "r", target, ConflictOverwrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 1 || res.Skipped != 0 {
		t.Fatalf("expected overwrite copy, got %+v", res)
	}
	if got := readFile(t, filepath.Join(target, ".env")); got != "TEMPLATE\n" {
		t.Errorf("expected overwrite, got %q", got)
	}
}

func TestApplyTemplate_MergesOverlappingDirectories(t *testing.T) {
	// When the template and the destination both have a directory at the
	// same relative path, existing files inside it must be left alone and
	// new files from the template must land alongside them.
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(target, ".vscode", "extensions.json"), "EXISTING\n")
	setupTemplate(t, root, "r", map[string]string{
		".vscode/settings.json": "NEW\n",
		".vscode/launch.json":   "NEW2\n",
	})

	res, err := ApplyTemplate(root, "r", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 2 {
		t.Fatalf("expected 2 copied, got %+v", res)
	}
	// Pre-existing sibling must be untouched.
	if got := readFile(t, filepath.Join(target, ".vscode", "extensions.json")); got != "EXISTING\n" {
		t.Errorf("existing sibling clobbered: %q", got)
	}
	// Template files land in place.
	if got := readFile(t, filepath.Join(target, ".vscode", "settings.json")); got != "NEW\n" {
		t.Errorf("settings.json: %q", got)
	}
	if got := readFile(t, filepath.Join(target, ".vscode", "launch.json")); got != "NEW2\n" {
		t.Errorf("launch.json: %q", got)
	}
}

func TestApplyTemplate_SkipsDirectoryAtDestination(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	// Destination has a directory named like the template file — must not be clobbered.
	if err := os.MkdirAll(filepath.Join(target, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	setupTemplate(t, root, "r", map[string]string{".env": "TEMPLATE\n"})

	res, err := ApplyTemplate(root, "r", target, ConflictOverwrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 0 || res.Skipped != 1 {
		t.Fatalf("expected skip, got %+v", res)
	}
	if len(res.Warnings) == 0 {
		t.Errorf("expected a warning about directory conflict")
	}
}

func TestApplyTemplate_IgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	tmpl := TemplateDirForRepo(root, "r")
	if err := os.MkdirAll(tmpl, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a regular file to confirm copy still works when a symlink coexists.
	writeFile(t, filepath.Join(tmpl, "real.txt"), "ok\n")
	// Create a dangling symlink.
	if err := os.Symlink("/nonexistent/target", filepath.Join(tmpl, "link.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := ApplyTemplate(root, "r", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 1 {
		t.Fatalf("expected 1 copied (symlink skipped), got %+v", res)
	}
	if _, err := os.Lstat(filepath.Join(target, "link.txt")); !os.IsNotExist(err) {
		t.Errorf("expected symlink not to be copied, but lstat err=%v", err)
	}
}

func TestApplyTemplate_SkipsGitDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	setupTemplate(t, root, "r", map[string]string{
		".git/config": "nope\n",
		"keep.txt":    "yes\n",
	})

	res, err := ApplyTemplate(root, "r", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 1 {
		t.Fatalf("expected 1 copied, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); !os.IsNotExist(err) {
		t.Errorf("expected .git to be excluded")
	}
}

func TestOpenTemplatesDir_PrecreatesPerRepoFolders(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()

	// Create a real git repo so RepoName / FindMainRepo can resolve it.
	mainRepo := filepath.Join(root, "demo-repo")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", mainRepo, "init", "-q").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	dir, err := OpenTemplatesDir(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != filepath.Join(root, ".asm", "templates") {
		t.Errorf("unexpected templates root: %s", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo-repo")); err != nil {
		t.Errorf("expected per-repo dir to be precreated: %v", err)
	}
}

func TestApplyTemplate_EmptyRepoName(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "wt-new")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyTemplate(root, "", target, ConflictSkip)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Copied != 0 || len(res.Warnings) == 0 {
		t.Fatalf("expected warning on empty repo name, got %+v", res)
	}
}
