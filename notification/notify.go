package notification

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// notifyTimeout bounds each native notification invocation so a hung
// notification daemon (Notification Center, dbus, etc) can't leak the
// goroutine fired by Send.
const notifyTimeout = 5 * time.Second

// Send sends a desktop notification. Best-effort: errors are silently ignored.
func Send(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		sendDarwin(title, body)
	case "linux":
		sendLinux(title, body)
	case "windows":
		sendWindows(title, body)
	}
}

// runNotify runs the native notification command bounded by notifyTimeout.
// Errors are intentionally dropped — notifications are best-effort and must
// not surface to the caller on platforms where the daemon is unavailable.
func runNotify(name string, args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, name, args...).Run()
}

func sendDarwin(title, body string) {
	script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
	runNotify("osascript", "-e", script)
}

func sendLinux(title, body string) {
	runNotify("notify-send", "-u", "normal", "-t", "5000", title, body)
}

func sendWindows(title, body string) {
	script := `
$xml = @"
<toast>
  <visual>
    <binding template="ToastGeneric">
      <text>` + title + `</text>
      <text>` + body + `</text>
    </binding>
  </visual>
</toast>
"@
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xdoc = New-Object Windows.Data.Xml.Dom.XmlDocument
$xdoc.LoadXml($xml)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xdoc)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("ASM").Show($toast)
`
	runNotify("powershell", "-NoProfile", "-Command", script)
}

func escapeAppleScript(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			result = append(result, '\\', '"')
		case '\\':
			result = append(result, '\\', '\\')
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}
