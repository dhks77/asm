package shelljoin

import (
	"strings"
	"unicode"
)

// Join quotes each argument for POSIX shell consumption and joins them with
// spaces. Safe for command lines sent through tmux send-keys into a shell.
func Join(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quote(arg)
	}
	return strings.Join(quoted, " ")
}

// JoinCommand is like Join, but leaves the first token unquoted when it is a
// shell-safe bareword. This preserves shell features such as alias and tilde
// expansion for the command name while still quoting all arguments.
func JoinCommand(command string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	if isBareCommand(command) {
		parts = append(parts, command)
	} else {
		parts = append(parts, quote(command))
	}
	for _, arg := range args {
		parts = append(parts, quote(arg))
	}
	return strings.Join(parts, " ")
}

func quote(arg string) string {
	if arg == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
}

func isBareCommand(arg string) bool {
	if arg == "" {
		return false
	}
	for _, r := range arg {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			continue
		case strings.ContainsRune("_-./~:+@%=", r):
			continue
		default:
			return false
		}
	}
	return true
}
