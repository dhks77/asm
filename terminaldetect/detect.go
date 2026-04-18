package terminaldetect

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const commandTimeout = 3 * time.Second

// Kind identifies the detected terminal environment.
type Kind string

const (
	KindUnknown  Kind = "unknown"
	KindCMUX     Kind = "cmux"
	KindITerm    Kind = "iterm"
	KindTerminal Kind = "terminal"
)

// App is the best-effort originating application identity for the active
// terminal client.
type App struct {
	Name     string
	BundleID string
}

// CMUXMetadata contains the native cmux routing metadata needed for delivery.
type CMUXMetadata struct {
	WorkspaceID    string
	SurfaceID      string
	TabID          string
	PanelID        string
	SocketPath     string
	Socket         string
	BundledCLIPath string
	BundleID       string
}

// Info is the detected terminal context for the notification sender.
type Info struct {
	Kind      Kind
	App       App
	CMUX      *CMUXMetadata
	ClientPID int
	Env       map[string]string
}

type detector struct {
	listClients    func(sessionName string) ([]client, error)
	inspectCommand func(pid int) (string, error)
}

type client struct {
	PID      int
	Activity int64
}

var defaultDetector = detector{
	listClients:    listClients,
	inspectCommand: inspectCommand,
}

// Detect inspects the most recently active tmux client for the given session
// and classifies the terminal environment from its process environment.
func Detect(sessionName string) (Info, error) {
	return defaultDetector.detect(sessionName)
}

func (d detector) detect(sessionName string) (Info, error) {
	sessionName = strings.TrimSpace(sessionName)
	if sessionName == "" {
		return Info{Kind: KindUnknown}, nil
	}

	clients, err := d.listClients(sessionName)
	if err != nil {
		return Info{Kind: KindUnknown}, err
	}
	current, ok := mostRecentClient(clients)
	if !ok {
		return Info{Kind: KindUnknown}, nil
	}

	command, err := d.inspectCommand(current.PID)
	if err != nil {
		return Info{Kind: KindUnknown}, err
	}

	env := collectKnownEnv(command)
	info := Info{
		Kind:      KindUnknown,
		App:       detectApp(env),
		ClientPID: current.PID,
		Env:       env,
	}

	if kind := detectDirectTerminalKind(env); kind != KindUnknown {
		info.Kind = kind
		return info, nil
	}

	if workspaceID := env["CMUX_WORKSPACE_ID"]; workspaceID != "" {
		info.Kind = KindCMUX
		info.CMUX = &CMUXMetadata{
			WorkspaceID:    workspaceID,
			SurfaceID:      env["CMUX_SURFACE_ID"],
			TabID:          env["CMUX_TAB_ID"],
			PanelID:        env["CMUX_PANEL_ID"],
			SocketPath:     env["CMUX_SOCKET_PATH"],
			Socket:         env["CMUX_SOCKET"],
			BundledCLIPath: env["CMUX_BUNDLED_CLI_PATH"],
			BundleID:       env["CMUX_BUNDLE_ID"],
		}
		if info.CMUX.BundleID != "" {
			info.App.BundleID = info.CMUX.BundleID
		}
		info.App.Name = "cmux"
		return info, nil
	}

	return info, nil
}

func listClients(sessionName string) ([]client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx,
		"tmux", "list-clients", "-t", sessionName,
		"-F", "#{client_tty}\t#{client_pid}\t#{client_activity}",
	).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return parseClients(string(out)), nil
}

func inspectCommand(pid int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ps", "eww", "-p", strconv.Itoa(pid), "-o", "command=").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func parseClients(out string) []client {
	var clients []client
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
		if pid <= 0 {
			continue
		}
		clients = append(clients, client{
			PID:      pid,
			Activity: parseActivity(fields[2]),
		})
	}
	return clients
}

func parseActivity(raw string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return v
}

func mostRecentClient(clients []client) (client, bool) {
	if len(clients) == 0 {
		return client{}, false
	}
	best := clients[0]
	for _, candidate := range clients[1:] {
		if candidate.Activity > best.Activity {
			best = candidate
		}
	}
	return best, true
}

func collectKnownEnv(command string) map[string]string {
	env := make(map[string]string)
	for _, key := range []string{
		"HOME",
		"PATH",
		"LANG",
		"TMPDIR",
		"USER",
		"LOGNAME",
		"SHELL",
		"CMUX_WORKSPACE_ID",
		"CMUX_SURFACE_ID",
		"CMUX_TAB_ID",
		"CMUX_PANEL_ID",
		"CMUX_SOCKET_PATH",
		"CMUX_SOCKET",
		"CMUX_BUNDLED_CLI_PATH",
		"CMUX_BUNDLE_ID",
		"TERM_PROGRAM",
		"TERM_PROGRAM_VERSION",
		"LC_TERMINAL",
		"ITERM_SESSION_ID",
		"TERM_SESSION_ID",
		"__CFBundleIdentifier",
	} {
		if value, ok := findEnvValue(command, key); ok {
			env[key] = value
		}
	}
	return env
}

func detectApp(env map[string]string) App {
	bundleID := strings.TrimSpace(env["__CFBundleIdentifier"])
	name := normalizeTerminalName(firstNonEmpty(
		env["TERM_PROGRAM"],
		env["LC_TERMINAL"],
		bundleID,
	))
	switch {
	case bundleID != "" || name != "":
		return App{Name: name, BundleID: bundleID}
	default:
		return App{}
	}
}

func detectDirectTerminalKind(env map[string]string) Kind {
	switch {
	case env["ITERM_SESSION_ID"] != "":
		return KindITerm
	case env["TERM_PROGRAM"] == "iTerm.app", env["LC_TERMINAL"] == "iTerm2":
		return KindITerm
	case env["TERM_PROGRAM"] == "Apple_Terminal":
		return KindTerminal
	default:
		return KindUnknown
	}
}

func normalizeTerminalName(name string) string {
	switch strings.TrimSpace(name) {
	case "iTerm.app", "iTerm2":
		return "iTerm"
	case "Apple_Terminal":
		return "Terminal"
	default:
		return strings.TrimSpace(name)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func findEnvValue(command, key string) (string, bool) {
	needle := key + "="
	for idx := strings.Index(command, needle); idx >= 0; {
		if idx == 0 || command[idx-1] == ' ' || command[idx-1] == '\t' {
			start := idx + len(needle)
			end := len(command)
			for i := start; i < len(command); i++ {
				if command[i] != ' ' && command[i] != '\t' {
					continue
				}
				j := i + 1
				if j >= len(command) || !isEnvNameStart(command[j]) {
					continue
				}
				j++
				for j < len(command) && isEnvNamePart(command[j]) {
					j++
				}
				if j < len(command) && command[j] == '=' {
					end = i
					break
				}
			}
			return command[start:end], true
		}
		next := strings.Index(command[idx+1:], needle)
		if next < 0 {
			break
		}
		idx += next + 1
	}
	return "", false
}

func isEnvNameStart(ch byte) bool {
	return ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}

func isEnvNamePart(ch byte) bool {
	return isEnvNameStart(ch) || ch >= '0' && ch <= '9'
}
