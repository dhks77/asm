package osnotify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nhn/asm/terminaldetect"
)

const commandTimeout = 5 * time.Second

var runCommand = defaultRunCommand

// Send delivers a generic OS notification.
func Send(title, body string, info terminaldetect.Info) error {
	title = defaultTitle(title)
	body = notificationMessage(title, body)
	if strings.TrimSpace(info.App.BundleID) != "" {
		return sendHelperNotification(title, body, info.App)
	}
	return sendAppleScriptNotification(title, body)
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

func sendAppleScriptNotification(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return runCommand(ctx, "osascript", []string{"-e", appleScriptNotification(title, body)})
}

func appleScriptNotification(title, body string) string {
	return fmt.Sprintf("display notification %q with title %q", appleScriptQuote(body), appleScriptQuote(title))
}

func appleScriptQuote(s string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"")
	return replacer.Replace(strings.TrimSpace(s))
}

func defaultRunCommand(ctx context.Context, name string, args []string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
