package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/nhn/csm/config"
	"github.com/nhn/csm/internal"
)

type DoorayClient struct {
	cfg       config.DoorayConfig
	cache     *internal.Cache
	pattern   *regexp.Regexp
	client    *http.Client
}

type taskResult struct {
	Header struct {
		IsSuccessful bool `json:"isSuccessful"`
	} `json:"header"`
	Result struct {
		Subject string `json:"subject"`
	} `json:"result"`
}

func NewDoorayClient(cfg config.DoorayConfig, taskIDPattern string) *DoorayClient {
	if !cfg.Enabled {
		return nil
	}

	pattern, err := regexp.Compile(taskIDPattern)
	if err != nil {
		return nil
	}

	return &DoorayClient{
		cfg:     cfg,
		cache:   internal.NewCache("dooray-tasks"),
		pattern: pattern,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ExtractTaskID extracts a task ID from a branch/folder name.
func (c *DoorayClient) ExtractTaskID(name string) string {
	if c == nil {
		return ""
	}
	matches := c.pattern.FindStringSubmatch(name)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// GetTaskName returns the task subject for a given task ID.
func (c *DoorayClient) GetTaskName(taskID string) (string, error) {
	if c == nil {
		return "", nil
	}

	// Check cache first
	if cached, ok := c.cache.Get(taskID); ok {
		return cached, nil
	}

	// Fetch from API
	url := fmt.Sprintf("%s/common/v1/tasks/%s", c.cfg.APIURL, taskID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "dooray-api "+c.cfg.Token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result taskResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if !result.Header.IsSuccessful {
		return "", fmt.Errorf("dooray API error for task %s", taskID)
	}

	name := result.Result.Subject
	// Cache for 1 hour
	c.cache.Set(taskID, name, time.Hour)

	return name, nil
}

// ResolveTaskName extracts a task ID from name and fetches the task subject.
func (c *DoorayClient) ResolveTaskName(folderName string) string {
	if c == nil {
		return ""
	}

	taskID := c.ExtractTaskID(folderName)
	if taskID == "" {
		return ""
	}

	name, err := c.GetTaskName(taskID)
	if err != nil {
		return ""
	}
	return name
}
