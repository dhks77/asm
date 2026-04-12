package notification

import (
	"os/exec"
	"runtime"
)

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
	script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
	exec.Command("osascript", "-e", script).Run() //nolint:errcheck
}

func sendLinux(title, body string) {
	exec.Command("notify-send", "-u", "normal", "-t", "5000", title, body).Run() //nolint:errcheck
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
	exec.Command("powershell", "-NoProfile", "-Command", script).Run() //nolint:errcheck
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
