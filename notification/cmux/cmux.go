package cmuxnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nhn/asm/terminaldetect"
)

const commandTimeout = 5 * time.Second

var (
	lookPath   = exec.LookPath
	statPath   = os.Stat
	currentEnv = os.Environ
	runCommand = defaultRunCommand
)

// Send delivers a native cmux notification using workspace/surface metadata.
func Send(title, body, provider string, info terminaldetect.Info) error {
	if info.CMUX == nil {
		return fmt.Errorf("cmux metadata not available")
	}
	meta := *info.CMUX
	if strings.TrimSpace(meta.WorkspaceID) == "" {
		return fmt.Errorf("cmux workspace id not available")
	}

	cliPath, err := resolveCLIPath(meta)
	if err != nil {
		return err
	}
	env := buildEnv(info)

	var firstErr error
	if isClaudeProvider(provider) {
		if err := sendClaudeHook(cliPath, env, meta, title, body); err == nil {
			return nil
		} else {
			firstErr = err
		}
	}

	if err := sendGenericNotify(cliPath, env, meta, title, body); err != nil {
		if firstErr != nil {
			return fmt.Errorf("claude-hook notification failed: %w; generic notify failed: %v", firstErr, err)
		}
		return err
	}
	return nil
}

func sendClaudeHook(cliPath string, env []string, meta terminaldetect.CMUXMetadata, title, body string) error {
	payload, err := json.Marshal(struct {
		Message string `json:"message"`
	}{
		Message: notificationMessage(title, body),
	})
	if err != nil {
		return err
	}

	args := []string{
		"claude-hook",
		"notification",
		"--workspace", meta.WorkspaceID,
	}
	if meta.SurfaceID != "" {
		args = append(args, "--surface", meta.SurfaceID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return runCommand(ctx, cliPath, args, env, payload)
}

func sendGenericNotify(cliPath string, env []string, meta terminaldetect.CMUXMetadata, title, body string) error {
	args := []string{
		"notify",
		"--title", defaultTitle(title),
		"--body", notificationMessage(title, body),
		"--workspace", meta.WorkspaceID,
	}
	if meta.SurfaceID != "" {
		args = append(args, "--surface", meta.SurfaceID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return runCommand(ctx, cliPath, args, env, nil)
}

func resolveCLIPath(meta terminaldetect.CMUXMetadata) (string, error) {
	if path := strings.TrimSpace(meta.BundledCLIPath); path != "" {
		if _, err := statPath(path); err == nil {
			return path, nil
		}
	}
	return lookPath("cmux")
}

func buildEnv(info terminaldetect.Info) []string {
	meta := terminaldetect.CMUXMetadata{}
	if info.CMUX != nil {
		meta = *info.CMUX
	}
	var env []string
	for _, key := range []string{"HOME", "PATH", "LANG", "TMPDIR", "USER", "LOGNAME", "SHELL"} {
		if value := strings.TrimSpace(info.Env[key]); value != "" {
			env = append(env, key+"="+value)
			continue
		}
		if value, ok := envValue(currentEnv(), key); ok && value != "" {
			env = append(env, key+"="+value)
		}
	}
	for _, kv := range [][2]string{
		{"CMUX_WORKSPACE_ID", meta.WorkspaceID},
		{"CMUX_SURFACE_ID", meta.SurfaceID},
		{"CMUX_TAB_ID", meta.TabID},
		{"CMUX_PANEL_ID", meta.PanelID},
		{"CMUX_SOCKET_PATH", meta.SocketPath},
		{"CMUX_SOCKET", meta.Socket},
		{"CMUX_BUNDLED_CLI_PATH", meta.BundledCLIPath},
		{"CMUX_BUNDLE_ID", meta.BundleID},
	} {
		if strings.TrimSpace(kv[1]) != "" {
			env = append(env, kv[0]+"="+kv[1])
		}
	}
	return env
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

func isClaudeProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "claude")
}

func notificationMessage(title, body string) string {
	if strings.TrimSpace(body) != "" {
		return strings.TrimSpace(body)
	}
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	return "done"
}

func defaultTitle(title string) string {
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	return "ASM"
}

func defaultRunCommand(ctx context.Context, name string, args []string, env []string, stdin []byte) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append([]string(nil), env...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
