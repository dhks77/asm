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

// Configurable represents anything that exposes configurable fields.
type Configurable interface {
	ConfigFields() []Field
	ConfigGet() map[string]string
	ConfigSet(values map[string]string) error
}

// Entry represents a configurable plugin (provider or tracker).
type Entry struct {
	Name     string       // display name
	Category string       // "provider" or "tracker"
	Source   Configurable // in-process configurable (for built-ins)
	Path     string       // path to plugin executable (for plugins)
}

// GetFields returns the field definitions for the entry.
func (e Entry) GetFields() []Field {
	if e.Source != nil {
		return e.Source.ConfigFields()
	}
	fields, _ := getPluginFields(e.Path)
	return fields
}

// GetValues returns the current values for the entry.
func (e Entry) GetValues() map[string]string {
	if e.Source != nil {
		return e.Source.ConfigGet()
	}
	values, _ := getPluginValues(e.Path)
	if values == nil {
		values = make(map[string]string)
	}
	return values
}

// SetValues saves the values for the entry.
func (e Entry) SetValues(values map[string]string) error {
	if e.Source != nil {
		return e.Source.ConfigSet(values)
	}
	return setPluginValues(e.Path, values)
}

func getPluginFields(pluginPath string) ([]Field, error) {
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

func getPluginValues(pluginPath string) (map[string]string, error) {
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

func setPluginValues(pluginPath string, values map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, _ := json.Marshal(values)
	cmd := exec.CommandContext(ctx, pluginPath, "config-set")
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}
