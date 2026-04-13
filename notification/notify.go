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

func sendDarwin(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
	_ = exec.CommandContext(ctx, "osascript", "-e", script).Run()
}

func sendLinux(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, "notify-send", "-u", "normal", "-t", "5000", title, body).Run()
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
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script).Run()
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
