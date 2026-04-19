package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PluginInfo is the JSON response from `<plugin> info`.
type PluginInfo struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	// ResumeArgs is prepended to Args when asm wants to resume a prior
	// session. Plugins that can't resume omit this field (stays nil).
	ResumeArgs           []string `json:"resume_args,omitempty"`
	NeedsContent         bool     `json:"needs_content"`
	CMUXNotificationHook string   `json:"cmux_notification_hook,omitempty"`
}

type detectStateRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type detectStateResponse struct {
	State string `json:"state"`
}

// PluginProvider implements Provider by executing an external plugin binary.
//
// Plugin protocol:
//
//	<plugin> info                → PluginInfo JSON (called once, cached)
//	echo JSON | <plugin> detect-state  → {"state":"thinking"} (called per tick)
type PluginProvider struct {
	path string     // path to plugin executable
	info PluginInfo // cached info from `<plugin> info`
}

// LoadPlugin loads a plugin from the given executable path.
// It calls `<plugin> info` and caches the result.
func LoadPlugin(path string) (*PluginProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "info").Output()
	if err != nil {
		return nil, err
	}

	var info PluginInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}

	// Verify the command binary exists on PATH
	if _, err := exec.LookPath(info.Command); err != nil {
		return nil, fmt.Errorf("%s: command %q not found", info.Name, info.Command)
	}

	return &PluginProvider{path: path, info: info}, nil
}

func (p *PluginProvider) Name() string        { return p.info.Name }
func (p *PluginProvider) DisplayName() string { return p.info.DisplayName }
func (p *PluginProvider) Command() string     { return p.info.Command }
func (p *PluginProvider) Args() []string      { return p.info.Args }

// ResumeArgs returns the plugin's declared resume args regardless of cwd —
// plugins can't gate on per-cwd session existence (the protocol is static
// JSON). Plugin authors that can't tolerate a "resume with no history" call
// should leave resume_args unset.
func (p *PluginProvider) ResumeArgs(cwd string) []string {
	if args, ok := p.dynamicResumeArgs(cwd); ok {
		return args
	}
	return p.info.ResumeArgs
}
func (p *PluginProvider) PluginPath() string { return p.path }
func (p *PluginProvider) NeedsContent(title string) bool {
	if needsContent, ok := p.dynamicNeedsContent(title); ok {
		return needsContent
	}
	return p.info.NeedsContent
}
func (p *PluginProvider) CMUXNotificationHook() string {
	return strings.TrimSpace(p.info.CMUXNotificationHook)
}

// DetectState calls `<plugin> detect-state` with title+content via stdin.
func (p *PluginProvider) DetectState(title, content string) State {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input, _ := json.Marshal(detectStateRequest{Title: title, Content: content})

	cmd := exec.CommandContext(ctx, p.path, "detect-state")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return StateUnknown
	}

	var resp detectStateResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return StateUnknown
	}

	return parseStateName(resp.State)
}

func parseStateName(s string) State {
	switch s {
	case "idle":
		return StateIdle
	case "busy":
		return StateBusy
	case "thinking":
		return StateThinking
	case "tool_use":
		return StateToolUse
	case "responding":
		return StateResponding
	default:
		return StateUnknown
	}
}

func (p *PluginProvider) dynamicResumeArgs(cwd string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.path, "resume-args", cwd).Output()
	if err != nil {
		return nil, false
	}
	var args []string
	if err := json.Unmarshal(out, &args); err != nil {
		return nil, false
	}
	return args, true
}

func (p *PluginProvider) dynamicNeedsContent(title string) (bool, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.path, "needs-content", title).Output()
	if err != nil {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(out, &value); err != nil {
		return false, false
	}
	return value, true
}
