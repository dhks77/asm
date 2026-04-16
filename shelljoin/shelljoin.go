package shelljoin

import "strings"

// Join quotes each argument for POSIX shell consumption and joins them with
// spaces. Safe for command lines sent through tmux send-keys into a shell.
func Join(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quote(arg)
	}
	return strings.Join(quoted, " ")
}

func quote(arg string) string {
	if arg == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
}
