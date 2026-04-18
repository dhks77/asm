package trash

import "github.com/nhn/asm/platform"

// Move sends path to the user's desktop trash when possible.
func Move(path string) error {
	return platform.Current().MoveToTrash(path)
}
