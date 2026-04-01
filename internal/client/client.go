package client

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"argo-analyzer/internal/models"
)

// Client is the Argo Workflows REST API client
type Client struct {
	baseURL    string
	namespace  string
	token      string
	httpClient *http.Client
}

// Config holds client configuration
type Config struct {
	BaseURL            string
	Namespace          string
	Token              string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

// New creates a new Argo Workflows client
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		},
	}

	return &Client{
		baseURL:   cfg.BaseURL,
		namespace: cfg.Namespace,
		token:     cfg.Token,
		httpClient: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}
}

// FetchByCount retrieves the most recent `limit` archived workflows
func (c *Client) FetchByCount(limit int) ([]models.Workflow, error) {
	fmt.Printf("Fetching up to %d archived workflows...\n", limit)

	var allWorkflows []models.Workflow
	continueToken := ""
	pageSize := 50
	if limit < pageSize {
		pageSize = limit
	}

	for {
		remaining := limit - len(allWorkflows)
		if remaining <= 0 {
			break
		}
		if remaining < pageSize {
			pageSize = remaining
		}

		workflows, next, err := c.fetchPage(pageSize, continueToken, "", "")
		if err != nil {
			return nil, err
		}

		allWorkflows = append(allWorkflows, workflows...)

		if next == "" || len(workflows) == 0 {
			break
		}
		continueToken = next
	}

	if len(allWorkflows) > limit {
		allWorkflows = allWorkflows[:limit]
	}

	return allWorkflows, nil
}

// FetchByTimeWindow retrieves archived workflows within the given time range
func (c *Client) FetchByTimeWindow(from, to time.Time) ([]models.Workflow, error) {
	fmt.Printf("Fetching archived workflows from %s to %s...\n",
		from.Format(time.RFC3339), to.Format(time.RFC3339))

	var allWorkflows []models.Workflow
	continueToken := ""

	for {
		workflows, next, err := c.fetchPage(50, continueToken, "", "")
		if err != nil {
			return nil, err
		}

		for _, wf := range workflows {
			startedAt, err := parseTime(wf.Status.StartedAt)
			if err != nil {
				continue
			}
			if (startedAt.Equal(from) || startedAt.After(from)) && startedAt.Before(to) {
				allWorkflows = append(allWorkflows, wf)
			}
		}

		if next == "" || len(workflows) == 0 {
			break
		}
		continueToken = next
	}

	return allWorkflows, nil
}

// fetchPage retrieves a single page of archived workflows
func (c *Client) fetchPage(limit int, continueToken, labelSelector, fieldSelector string) ([]models.Workflow, string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/archived-workflows", c.baseURL)

	params := url.Values{}
	params.Set("listOptions.limit", strconv.Itoa(limit))
	if c.namespace != "" {
		params.Set("namespace", c.namespace)
	}
	if continueToken != "" {
		params.Set("listOptions.continue", continueToken)
	}
	if labelSelector != "" {
		params.Set("listOptions.labelSelector", labelSelector)
	}
	if fieldSelector != "" {
		params.Set("listOptions.fieldSelector", fieldSelector)
	}

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("executing request to %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response body: %w", err)
	}

	var wfList models.ArgoWorkflowList
	if err := json.Unmarshal(body, &wfList); err != nil {
		return nil, "", fmt.Errorf("parsing response JSON: %w", err)
	}

	return wfList.Items, wfList.Metadata.Continue, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse time: %s", s)
}
