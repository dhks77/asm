package plugincfg

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// Field describes a configurable field exposed by a plugin.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// Entry represents a configurable plugin (provider or tracker).
type Entry struct {
	Name        string // display name
	Category    string // "provider" or "tracker"
	PluginPath  string // path to executable
}

// GetFields calls `<plugin> config-fields` and returns field definitions.
func GetFields(pluginPath string) ([]Field, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, pluginPath, "config-fields").Output()
	if err != nil {
		return nil, err
	}

	var fields []Field
	if err := json.Unmarshal(out, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// GetValues calls `<plugin> config-get` and returns current values.
func GetValues(pluginPath string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, pluginPath, "config-get").Output()
	if err != nil {
		return nil, err
	}

	var values map[string]string
	if err := json.Unmarshal(out, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// SetValues calls `echo JSON | <plugin> config-set` to save values.
func SetValues(pluginPath string, values map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, _ := json.Marshal(values)
	cmd := exec.CommandContext(ctx, pluginPath, "config-set")
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}
