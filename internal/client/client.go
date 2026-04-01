// Package client fetches archived workflows from the Argo Workflows REST API.
//
// Two-phase fetch strategy
// ────────────────────────
// The /api/v1/archived-workflows list endpoint returns workflow metadata and
// status.phase, but status.nodes is always empty in list responses.
// Full node graphs are only available via the individual workflow endpoint:
//
//	GET /api/v1/archived-workflows/{uid}
//
// To avoid fetching the full detail for every workflow (which can be large),
// the client:
//  1. Fetches the list to get UIDs and phases.
//  2. Identifies which workflows are in a Failed/Error phase.
//  3. Fetches the full detail only for those failed workflows.
//  4. Returns the merged result: successful workflows from the list (no nodes
//     needed) + fully-hydrated failed workflows.
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

const (
	phasesFailed = "Failed"
	phasesError  = "Error"
)

// Client is the Argo Workflows REST API client.
type Client struct {
	baseURL    string
	namespace  string
	token      string
	httpClient *http.Client
}

// Config holds client configuration.
type Config struct {
	BaseURL            string
	Namespace          string
	Token              string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

// New creates a new Argo Workflows client.
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
	}
	return &Client{
		baseURL:    cfg.BaseURL,
		namespace:  cfg.Namespace,
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: cfg.Timeout, Transport: transport},
	}
}

// FetchByCount retrieves the most recent `limit` archived workflows.
// It fetches summary data for all workflows, then hydrates full node graphs
// for any workflow in a failed/error phase.
func (c *Client) FetchByCount(limit int) ([]models.Workflow, error) {
	fmt.Printf("Fetching up to %d archived workflows...\n", limit)

	summaries, err := c.fetchSummaries(limit, "", "")
	if err != nil {
		return nil, err
	}

	return c.hydrateFailed(summaries)
}

// FetchByTimeWindow retrieves archived workflows whose startedAt falls within
// [from, to), then hydrates failed ones with full node data.
func (c *Client) FetchByTimeWindow(from, to time.Time) ([]models.Workflow, error) {
	fmt.Printf("Fetching archived workflows from %s to %s...\n",
		from.Format(time.RFC3339), to.Format(time.RFC3339))

	// Fetch all pages; filter by time client-side (the list API has no
	// server-side time filter for archived workflows).
	var summaries []models.Workflow
	continueToken := ""
	for {
		page, next, err := c.fetchPage(50, continueToken, "", "")
		if err != nil {
			return nil, err
		}
		for _, wf := range page {
			t, err := parseTime(wf.Status.StartedAt)
			if err != nil {
				continue
			}
			if (t.Equal(from) || t.After(from)) && t.Before(to) {
				summaries = append(summaries, wf)
			}
		}
		if next == "" || len(page) == 0 {
			break
		}
		continueToken = next
	}

	return c.hydrateFailed(summaries)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// fetchSummaries pages through the archived-workflow list until it has
// collected `limit` entries (or exhausted all pages).
func (c *Client) fetchSummaries(limit int, labelSelector, fieldSelector string) ([]models.Workflow, error) {
	var all []models.Workflow
	continueToken := ""

	for {
		remaining := limit - len(all)
		if remaining <= 0 {
			break
		}
		pageSize := 50
		if remaining < pageSize {
			pageSize = remaining
		}

		page, next, err := c.fetchPage(pageSize, continueToken, labelSelector, fieldSelector)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)

		if next == "" || len(page) == 0 {
			break
		}
		continueToken = next
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// hydrateFailed takes a list of workflow summaries, fetches the full detail
// (including status.nodes) for each failed workflow, and returns the merged
// slice: succeeded summaries as-is, failed ones replaced with full detail.
func (c *Client) hydrateFailed(summaries []models.Workflow) ([]models.Workflow, error) {
	// Count how many we need to hydrate for progress reporting.
	failCount := 0
	for _, wf := range summaries {
		if isFailedPhase(wf.Status.Phase) {
			failCount++
		}
	}
	if failCount > 0 {
		fmt.Printf("Fetching full node graph for %d failed workflow(s)...\n", failCount)
	}

	result := make([]models.Workflow, 0, len(summaries))
	done := 0

	for _, wf := range summaries {
		if !isFailedPhase(wf.Status.Phase) {
			// Successful (or still-running) workflows: summary is sufficient.
			result = append(result, wf)
			continue
		}

		if wf.Metadata.UID == "" {
			// No UID — can't fetch detail; keep the summary as-is so it still
			// counts toward the failure total even without node data.
			fmt.Printf("  Warning: workflow %s has no UID, skipping node hydration\n", wf.Metadata.Name)
			result = append(result, wf)
			continue
		}

		full, err := c.fetchWorkflowByUID(wf.Metadata.UID)
		if err != nil {
			// Non-fatal: log and fall back to the summary without nodes.
			fmt.Printf("  Warning: could not fetch detail for %s (%s): %v\n",
				wf.Metadata.Name, wf.Metadata.UID, err)
			result = append(result, wf)
			continue
		}

		result = append(result, *full)
		done++
		if done%10 == 0 {
			fmt.Printf("  Hydrated %d/%d failed workflows...\n", done, failCount)
		}
	}

	return result, nil
}

// fetchPage retrieves one page of archived workflow summaries.
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

	body, err := c.get(fmt.Sprintf("%s?%s", endpoint, params.Encode()))
	if err != nil {
		return nil, "", err
	}

	var wfList models.ArgoWorkflowList
	if err := json.Unmarshal(body, &wfList); err != nil {
		return nil, "", fmt.Errorf("parsing workflow list: %w", err)
	}

	return wfList.Items, wfList.Metadata.Continue, nil
}

// fetchWorkflowByUID fetches the full workflow detail including status.nodes.
func (c *Client) fetchWorkflowByUID(uid string) (*models.Workflow, error) {
	endpoint := fmt.Sprintf("%s/api/v1/archived-workflows/%s", c.baseURL, uid)

	body, err := c.get(endpoint)
	if err != nil {
		return nil, err
	}

	var wf models.Workflow
	if err := json.Unmarshal(body, &wf); err != nil {
		return nil, fmt.Errorf("parsing workflow detail: %w", err)
	}

	return &wf, nil
}

// get performs an authenticated GET and returns the response body.
func (c *Client) get(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", rawURL, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", rawURL, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned HTTP %d: %s", rawURL, resp.StatusCode, truncate(string(body), 200))
	}

	return body, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func isFailedPhase(phase string) bool {
	return phase == phasesFailed || phase == phasesError
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}
	for _, f := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse time: %s", s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
