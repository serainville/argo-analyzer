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
//
// Rate limiting
// ─────────────
// All outbound requests pass through a token-bucket limiter so the tool never
// floods the Argo server. The bucket is refilled at a steady rate
// (Config.RatePerSecond tokens/sec) up to a maximum of Config.Burst tokens.
// If the bucket is empty the call blocks until a token is available — no
// requests are ever silently dropped.
//
// Defaults: 5 requests/sec, burst of 5.
// Disable:  set RatePerSecond to 0 (unlimited).
package client

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"argo-analyzer/internal/models"
)

const (
	phasesFailed = "Failed"
	phasesError  = "Error"

	defaultRatePerSecond = 5
	defaultBurst         = 5
)

// rateLimiter is a token-bucket rate limiter implemented with a buffered
// channel and a background refill goroutine.
//
// Design: the channel holds up to `burst` tokens. The refiller adds one token
// every (1s / rate) interval, blocking when the bucket is full. Callers
// consume one token per request by receiving from the channel, blocking when
// empty. This gives smooth, predictable pacing with no external dependencies.
type rateLimiter struct {
	tokens chan struct{}
	stop   chan struct{}
	once   sync.Once
}

func newRateLimiter(ratePerSecond, burst int) *rateLimiter {
	if burst <= 0 {
		burst = 1
	}
	rl := &rateLimiter{
		tokens: make(chan struct{}, burst),
		stop:   make(chan struct{}),
	}
	// Pre-fill the bucket so the first `burst` requests are not delayed.
	for i := 0; i < burst; i++ {
		rl.tokens <- struct{}{}
	}
	interval := time.Second / time.Duration(ratePerSecond)
	go rl.refill(interval)
	return rl
}

// refill adds one token to the bucket every interval until stopped.
func (rl *rateLimiter) refill(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			select {
			case rl.tokens <- struct{}{}:
				// token added
			default:
				// bucket full — discard
			}
		}
	}
}

// Wait blocks until a token is available.
func (rl *rateLimiter) Wait() {
	<-rl.tokens
}

// Stop shuts down the background refill goroutine.
func (rl *rateLimiter) Stop() {
	rl.once.Do(func() { close(rl.stop) })
}

// noopLimiter is used when rate limiting is disabled (RatePerSecond == 0).
type noopLimiter struct{}

func (noopLimiter) Wait() {}
func (noopLimiter) Stop() {}

type limiter interface {
	Wait()
	Stop()
}

// ── Client ────────────────────────────────────────────────────────────────────

// Client is the Argo Workflows REST API client.
type Client struct {
	baseURL    string
	namespace  string
	token      string
	httpClient *http.Client
	limiter    limiter
}

// Config holds client configuration.
type Config struct {
	BaseURL            string
	Namespace          string
	Token              string
	InsecureSkipVerify bool
	Timeout            time.Duration

	// RatePerSecond is the maximum number of requests sent to the Argo server
	// per second. The token bucket is pre-filled to Burst tokens so short
	// bursts are allowed without waiting.
	// Default: 5. Set to 0 to disable rate limiting entirely.
	RatePerSecond int

	// Burst is the maximum number of requests that may be sent back-to-back
	// before pacing kicks in. Must be >= 1 if RatePerSecond > 0.
	// Default: equal to RatePerSecond.
	Burst int
}

// New creates a new Argo Workflows client.
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Apply rate-limit defaults
	var lim limiter
	if cfg.RatePerSecond == 0 {
		lim = noopLimiter{}
	} else {
		rate := cfg.RatePerSecond
		burst := cfg.Burst
		if burst <= 0 {
			burst = rate
		}
		lim = newRateLimiter(rate, burst)
		fmt.Printf("Rate limiter: %d req/s (burst %d)\n", rate, burst)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
	}
	return &Client{
		baseURL:    cfg.BaseURL,
		namespace:  cfg.Namespace,
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: cfg.Timeout, Transport: transport},
		limiter:    lim,
	}
}

// Close stops the background rate-limit refill goroutine. Safe to call
// multiple times. Optional — the goroutine is also reclaimed at process exit.
func (c *Client) Close() {
	c.limiter.Stop()
}

// ── Public fetch methods ──────────────────────────────────────────────────────

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

func (c *Client) hydrateFailed(summaries []models.Workflow) ([]models.Workflow, error) {
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
			result = append(result, wf)
			continue
		}

		if wf.Metadata.UID == "" {
			fmt.Printf("  Warning: workflow %s has no UID, skipping node hydration\n", wf.Metadata.Name)
			result = append(result, wf)
			continue
		}

		full, err := c.fetchWorkflowByUID(wf.Metadata.UID)
		if err != nil {
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

func (c *Client) fetchWorkflowByUID(uid string) (*models.Workflow, error) {
	body, err := c.get(fmt.Sprintf("%s/api/v1/archived-workflows/%s", c.baseURL, uid))
	if err != nil {
		return nil, err
	}

	var wf models.Workflow
	if err := json.Unmarshal(body, &wf); err != nil {
		return nil, fmt.Errorf("parsing workflow detail: %w", err)
	}

	return &wf, nil
}

// get acquires a rate-limit token, then performs an authenticated GET.
// All requests — list pages and individual workflow detail — pass through here.
func (c *Client) get(rawURL string) ([]byte, error) {
	c.limiter.Wait() // blocks until a token is available

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
		return nil, fmt.Errorf("GET %s returned HTTP %d: %s",
			rawURL, resp.StatusCode, truncate(string(body), 200))
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

// ── Helpers ───────────────────────────────────────────────────────────────────

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
