package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type ProjectSettings struct {
	Dooray DooraySettings `json:"dooray"`
}

type DooraySettings struct {
	Token             string `json:"token"`
	ProjectID         string `json:"projectId"`
	APIBaseURL        string `json:"apiBaseUrl"`
	TaskNumberPattern string `json:"taskNumberPattern,omitempty"`
}

func (d DooraySettings) Enabled() bool {
	return d.Token != "" && d.ProjectID != "" && d.APIBaseURL != ""
}

func (d DooraySettings) APIURL() string {
	return strings.TrimRight(d.APIBaseURL, "/")
}

func LoadProjectSettings(rootPath string) (*ProjectSettings, error) {
	// Try .asm/ first, fall back to .csm/
	path := filepath.Join(rootPath, ".asm", "settings.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = filepath.Join(rootPath, ".csm", "settings.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectSettings{}, nil
		}
		return nil, err
	}

	var settings ProjectSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return &settings, nil
}

func SaveProjectSettings(rootPath string, settings *ProjectSettings) error {
	dir := filepath.Join(rootPath, ".asm")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)
}
