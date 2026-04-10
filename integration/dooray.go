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

var defaultPattern = regexp.MustCompile(`\d+`)

type DoorayClient struct {
	settings      config.DooraySettings
	cache         *internal.Cache
	customPattern *regexp.Regexp // nil means use default (last number)
	client        *http.Client
}

type postsResult struct {
	Header struct {
		IsSuccessful bool `json:"isSuccessful"`
	} `json:"header"`
	Result []struct {
		Subject string `json:"subject"`
	} `json:"result"`
	TotalCount int `json:"totalCount"`
}

func NewDoorayClient(settings config.DooraySettings) *DoorayClient {
	if !settings.Enabled() {
		return nil
	}

	var customPattern *regexp.Regexp
	if settings.TaskNumberPattern != "" {
		p, err := regexp.Compile(settings.TaskNumberPattern)
		if err != nil {
			return nil
		}
		customPattern = p
	}

	return &DoorayClient{
		settings:      settings,
		cache:         internal.NewCache("dooray-tasks"),
		customPattern: customPattern,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ExtractTaskNumber extracts a task number from a branch name.
// If customPattern is set, uses its first capture group.
// Otherwise, returns the last number found in the branch name.
func (c *DoorayClient) ExtractTaskNumber(branch string) string {
	if c == nil {
		return ""
	}

	if c.customPattern != nil {
		matches := c.customPattern.FindStringSubmatch(branch)
		if len(matches) < 2 {
			return ""
		}
		return matches[1]
	}

	// Default: last number in branch name
	all := defaultPattern.FindAllString(branch, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// GetTaskName fetches the task subject by post number.
func (c *DoorayClient) GetTaskName(postNumber string) (string, error) {
	if c == nil {
		return "", nil
	}

	if cached, ok := c.cache.Get(postNumber); ok {
		return cached, nil
	}

	url := fmt.Sprintf("%s/project/v1/projects/%s/posts?postNumber=%s&size=1",
		c.settings.APIURL(), c.settings.ProjectID, postNumber)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "dooray-api "+c.settings.Token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result postsResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if !result.Header.IsSuccessful || len(result.Result) == 0 {
		return "", fmt.Errorf("dooray API: no post found for number %s", postNumber)
	}

	name := result.Result[0].Subject
	c.cache.Set(postNumber, name, time.Hour)

	return name, nil
}

// ResolveTaskName extracts a task number from a branch name and fetches the task subject.
func (c *DoorayClient) ResolveTaskName(branch string) string {
	if c == nil {
		return ""
	}

	postNumber := c.ExtractTaskNumber(branch)
	if postNumber == "" {
		return ""
	}

	name, err := c.GetTaskName(postNumber)
	if err != nil {
		return ""
	}
	return name
}
