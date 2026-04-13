package tracker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/nhn/asm/plugincfg"
)

// DoorayConfig holds configuration for the built-in Dooray tracker.
type DoorayConfig struct {
	Token       string `toml:"token"`
	ProjectID   string `toml:"project_id"`
	APIBaseURL  string `toml:"api_base_url"`
	WebURL      string `toml:"web_url"`
	TaskPattern string `toml:"task_pattern"`
}

// DoorayTracker is the built-in Dooray tracker implementation.
type DoorayTracker struct {
	cfg       *DoorayConfig
	saveFn    func(*DoorayConfig) error
	client    *http.Client
	defaultRe *regexp.Regexp
}

// NewDoorayTracker creates a built-in Dooray tracker.
// saveFn is called to persist config changes from the settings UI.
func NewDoorayTracker(cfg *DoorayConfig, saveFn func(*DoorayConfig) error) *DoorayTracker {
	return &DoorayTracker{
		cfg:       cfg,
		saveFn:    saveFn,
		client:    &http.Client{Timeout: 5 * time.Second},
		defaultRe: regexp.MustCompile(`[0-9]+`),
	}
}

func (t *DoorayTracker) Name() string { return "dooray" }

func (t *DoorayTracker) Resolve(branch string) TaskInfo {
	if t.cfg.Token == "" || t.cfg.ProjectID == "" || t.cfg.APIBaseURL == "" {
		return TaskInfo{}
	}

	taskNum := t.extractTaskNumber(branch)
	if taskNum == "" {
		return TaskInfo{}
	}

	const maxRetries = 2
	for attempt := 0; attempt < maxRetries; attempt++ {
		info, ok := t.fetchTask(taskNum)
		if ok {
			return info
		}
	}
	return TaskInfo{}
}

func (t *DoorayTracker) extractTaskNumber(branch string) string {
	re := t.defaultRe
	if t.cfg.TaskPattern != "" {
		if custom, err := regexp.Compile(t.cfg.TaskPattern); err == nil {
			re = custom
		}
	}
	matches := re.FindAllString(branch, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func (t *DoorayTracker) fetchTask(taskNum string) (TaskInfo, bool) {
	url := fmt.Sprintf("%s/project/v1/projects/%s/posts?postNumber=%s&size=1",
		strings.TrimRight(t.cfg.APIBaseURL, "/"), t.cfg.ProjectID, taskNum)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return TaskInfo{}, false
	}
	req.Header.Set("Authorization", "dooray-api "+t.cfg.Token)

	resp, err := t.client.Do(req)
	if err != nil {
		return TaskInfo{}, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TaskInfo{}, false
	}

	var result struct {
		Header struct {
			IsSuccessful bool `json:"isSuccessful"`
		} `json:"header"`
		Result []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return TaskInfo{}, false
	}
	if !result.Header.IsSuccessful || len(result.Result) == 0 {
		return TaskInfo{}, false
	}

	post := result.Result[0]
	info := TaskInfo{Name: post.Subject}
	if t.cfg.WebURL != "" && post.ID != "" {
		info.URL = fmt.Sprintf("%s/project/tasks/%s",
			strings.TrimRight(t.cfg.WebURL, "/"), post.ID)
	}
	return info, true
}

// Configurable implementation for settings UI.

func (t *DoorayTracker) ConfigFields() []plugincfg.Field {
	return []plugincfg.Field{
		{Key: "token", Label: "Token", Secret: true},
		{Key: "project_id", Label: "Project ID"},
		{Key: "api_base_url", Label: "API Base URL"},
		{Key: "web_url", Label: "Web URL", Placeholder: "https://nhnent.dooray.com"},
		{Key: "task_pattern", Label: "Task Pattern", Placeholder: "[0-9]+ (last match)"},
	}
}

func (t *DoorayTracker) ConfigGet() map[string]string {
	return map[string]string{
		"token":        t.cfg.Token,
		"project_id":   t.cfg.ProjectID,
		"api_base_url": t.cfg.APIBaseURL,
		"web_url":      t.cfg.WebURL,
		"task_pattern": t.cfg.TaskPattern,
	}
}

func (t *DoorayTracker) ConfigSet(values map[string]string) error {
	t.cfg.Token = values["token"]
	t.cfg.ProjectID = values["project_id"]
	t.cfg.APIBaseURL = values["api_base_url"]
	t.cfg.WebURL = values["web_url"]
	t.cfg.TaskPattern = values["task_pattern"]
	if t.saveFn != nil {
		return t.saveFn(t.cfg)
	}
	return nil
}
