package trash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const finderTrashTimeout = 5 * time.Second

// Move sends path to the user's desktop trash when possible.
func Move(path string) error {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return os.ErrInvalid
	}

	switch runtime.GOOS {
	case "windows":
		return moveWindows(clean)
	default:
		return moveByRename(clean)
	}
}

func moveByRename(path string) error {
	trashDir, err := userTrashDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return err
	}

	dst, err := uniqueDestination(trashDir, filepath.Base(path))
	if err != nil {
		return err
	}
	if err := os.Rename(path, dst); err != nil {
		if runtime.GOOS == "darwin" {
			if fallbackErr := moveDarwinWithFinder(path); fallbackErr == nil {
				return nil
			} else {
				return fmt.Errorf("rename to trash failed: %w (finder fallback: %v)", err, fallbackErr)
			}
		}
		return err
	}
	return nil
}

func userTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".Trash"), nil
	default:
		return filepath.Join(home, ".local", "share", "Trash", "files"), nil
	}
}

func uniqueDestination(dir, name string) (string, error) {
	base, ext := splitName(name)
	candidate := filepath.Join(dir, name)
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil {
		return "", err
	}

	for i := 2; i < 10_000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s %d%s", base, i, ext))
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate trash destination for %q", name)
}

func splitName(name string) (string, string) {
	ext := filepath.Ext(name)
	if ext == "" {
		return name, ""
	}
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		return name, ""
	}
	return base, ext
}

func moveDarwinWithFinder(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), finderTrashTimeout)
	defer cancel()

	script := `tell application "Finder" to delete POSIX file "` + escapeAppleScript(path) + `"`
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func moveWindows(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	command := `[Reflection.Assembly]::LoadWithPartialName("Microsoft.VisualBasic") | Out-Null; `
	if info.IsDir() {
		command += `[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($args[0], 'OnlyErrorDialogs', 'SendToRecycleBin')`
	} else {
		command += `[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($args[0], 'OnlyErrorDialogs', 'SendToRecycleBin')`
	}

	ctx, cancel := context.WithTimeout(context.Background(), finderTrashTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command, path).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func escapeAppleScript(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
