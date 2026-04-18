package platform

import "path/filepath"

func newDarwinPlatform() Platform {
	p := &platformImpl{
		name: "darwin",
		notify: func(title, body string) {
			script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
			runBestEffort(notificationTimeout, "osascript", "-e", script)
		},
		openURL:    func(url string) error { return startDetached("open", url) },
		revealPath: func(path string) error { return startDetached("open", path) },
		builtinIDEs: []IDEEntry{
			{Name: "intellij", Command: "open", Args: []string{"-a", "IntelliJ IDEA"}},
			{Name: "vscode", Command: "open", Args: []string{"-a", "Visual Studio Code"}},
		},
		prepareIDEOpen: func(name, command string, args []string, path string) (string, []string) {
			if name == "intellij" && command == "open" {
				if appName, ok := openAppName(args); ok {
					return "open", []string{"-n", "-a", appName, "--args", path}
				}
			}
			return appendPath(command, args, path)
		},
	}
	p.moveToTrash = func(path string) error {
		home, err := p.HomeDir()
		if err != nil {
			return err
		}
		return moveByRename(path, filepath.Join(home, ".Trash"), moveDarwinWithFinder)
	}
	return p
}
