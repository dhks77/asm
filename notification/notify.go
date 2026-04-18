package notification

import "github.com/nhn/asm/platform"

// Send sends a desktop notification. Best-effort: errors are silently ignored.
func Send(title, body string) {
	platform.Current().Notify(title, body)
}
