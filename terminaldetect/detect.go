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
	KindUnknown Kind = "unknown"
	KindCMUX    Kind = "cmux"
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
	ClientTTY string
	Env       map[string]string
}

type detector struct {
	listClients    func(sessionName string) ([]client, error)
	inspectCommand func(pid int) (string, error)
}

type client struct {
	TTY      string
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

	env := collectEnv(command)
	info := Info{
		Kind:      KindUnknown,
		App:       detectApp(env),
		ClientPID: current.PID,
		ClientTTY: strings.TrimSpace(current.TTY),
		Env:       env,
	}

	if shouldPreferDirectApp(info.App, env) {
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
			TTY:      strings.TrimSpace(fields[0]),
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

func collectEnv(command string) map[string]string {
	env := make(map[string]string)
	for i := 0; i < len(command); i++ {
		if i > 0 && command[i-1] != ' ' && command[i-1] != '\t' {
			continue
		}
		if !isEnvNameStart(command[i]) {
			continue
		}

		j := i + 1
		for j < len(command) && isEnvNamePart(command[j]) {
			j++
		}
		if j >= len(command) || command[j] != '=' {
			continue
		}

		key := command[i:j]
		value, ok := findEnvValue(command[i:], key)
		if !ok {
			continue
		}
		env[key] = value
		i = j
	}
	return env
}

func detectApp(env map[string]string) App {
	bundleID := strings.TrimSpace(env["__CFBundleIdentifier"])
	name := normalizeAppLabel(firstNonEmpty(
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

func shouldPreferDirectApp(app App, env map[string]string) bool {
	bundleID := strings.TrimSpace(app.BundleID)
	if bundleID == "" {
		return false
	}

	cmuxBundleID := strings.TrimSpace(env["CMUX_BUNDLE_ID"])
	if cmuxBundleID == "" {
		return true
	}

	return bundleID != cmuxBundleID
}

func normalizeAppLabel(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".app")
	name = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name)
	return strings.Join(strings.Fields(name), " ")
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
