package main

import (
	"fmt"
	"os"
	"time"

	"argo-analyzer/internal/analyzer"
	"argo-analyzer/internal/client"
	"argo-analyzer/internal/confluence"
	"argo-analyzer/internal/models"
	"argo-analyzer/internal/reporter"

	"github.com/spf13/cobra"
)

var (
	// Connection flags
	server        string
	namespace     string
	token         string
	insecure      bool
	timeout       int
	ratePerSecond int
	burst         int

	// Standard output flags
	csvFile  string
	jsonFile string
	mdFile   string
	verbose  bool

	// Confluence publish flags
	cfURL      string
	cfUser     string
	cfToken    string
	cfSpace    string
	cfTitle    string
	cfParentID string

	rootCmd = &cobra.Command{
		Use:   "argo-analyzer",
		Short: "Analyze failed archived workflows from an Argo Workflows server",
		Long: `argo-analyzer pulls archived workflows from an Argo Workflows REST API,
classifies every failed leaf node by responsibility category (platform /
application / devex), detects recurring failure patterns across runs, and
produces actionable DevEx insights alongside the standard failure report.

Output formats: console, CSV, JSON, Markdown, Confluence page.

Examples:

  # Analyze the last 100 archived workflows
  argo-analyzer count --server https://argo.example.com --count 100

  # Export Markdown for manual Confluence import
  argo-analyzer count --server https://argo.example.com --count 100 \
    --md report.md

  # Publish directly to Confluence
  argo-analyzer window --server https://argo.example.com \
    --from 2024-06-01T00:00:00Z --to 2024-06-30T23:59:59Z \
    --confluence-url https://yourorg.atlassian.net \
    --confluence-user you@example.com \
    --confluence-token YOUR_API_TOKEN \
    --confluence-space PLAT \
    --confluence-title "Argo Workflows — June 2024 Failure Report" \
    --confluence-parent-id 123456

  # All outputs at once
  argo-analyzer count --server https://argo.example.com --count 50 \
    --csv report.csv --json report.json --md report.md -v

Environment variables:
  ARGO_TOKEN          Bearer token for Argo Workflows
  CONFLUENCE_URL      Confluence base URL
  CONFLUENCE_USER     Confluence username / email
  CONFLUENCE_TOKEN    Confluence API token`,
	}

	countCmd = &cobra.Command{
		Use:   "count",
		Short: "Fetch and analyze the N most recent archived workflows",
		RunE:  runCount,
	}

	windowCmd = &cobra.Command{
		Use:   "window",
		Short: "Fetch and analyze archived workflows within a time range",
		RunE:  runWindow,
	}
)

var countLimit int
var fromStr, toStr string

func init() {
	pf := rootCmd.PersistentFlags()

	// Argo connection
	pf.StringVarP(&server, "server", "s", "", "Argo Workflows server URL (required)")
	pf.StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (empty = all namespaces)")
	pf.StringVarP(&token, "token", "t", "", "Bearer token (or set ARGO_TOKEN env var)")
	pf.BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification")
	pf.IntVar(&timeout, "timeout", 30, "HTTP request timeout in seconds")
	pf.IntVar(&ratePerSecond, "rate", 5, "Maximum Argo API requests per second (0 = unlimited)")
	pf.IntVar(&burst, "burst", 5, "Maximum burst size for the rate limiter")

	// Standard outputs
	pf.StringVar(&csvFile, "csv", "", "Write CSV report to this path")
	pf.StringVar(&jsonFile, "json", "", "Write JSON report to this path")
	pf.StringVar(&mdFile, "md", "", "Write Markdown report to this path (import into Confluence or read standalone)")
	pf.BoolVarP(&verbose, "verbose", "v", false, "Show classifier reasoning and node IDs")

	// Confluence direct publish
	pf.StringVar(&cfURL, "confluence-url", "", "Confluence base URL, e.g. https://yourorg.atlassian.net (or CONFLUENCE_URL)")
	pf.StringVar(&cfUser, "confluence-user", "", "Confluence username / Atlassian email (or CONFLUENCE_USER)")
	pf.StringVar(&cfToken, "confluence-token", "", "Confluence API token (or CONFLUENCE_TOKEN)")
	pf.StringVar(&cfSpace, "confluence-space", "", "Confluence space key, e.g. PLAT")
	pf.StringVar(&cfTitle, "confluence-title", "", "Confluence page title (created or updated)")
	pf.StringVar(&cfParentID, "confluence-parent-id", "", "Parent page ID (optional; defaults to space root)")

	// Sub-command flags
	countCmd.Flags().IntVarP(&countLimit, "count", "c", 50, "Number of recent workflows to fetch")
	windowCmd.Flags().StringVar(&fromStr, "from", "", "Window start (RFC3339) — required")
	windowCmd.Flags().StringVar(&toStr, "to", "", "Window end (RFC3339) — defaults to now")

	rootCmd.AddCommand(countCmd, windowCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCount(cmd *cobra.Command, args []string) error {
	c, err := buildClient()
	if err != nil {
		return err
	}
	defer c.Close()
	workflows, err := c.FetchByCount(countLimit)
	if err != nil {
		return fmt.Errorf("fetching workflows: %w", err)
	}
	fmt.Printf("Fetched %d archived workflows\n", len(workflows))

	report := analyzer.Analyze(workflows)
	report.QueryType = "count"
	report.QueryValue = fmt.Sprintf("%d most recent", countLimit)

	return generateOutputs(report)
}

func runWindow(cmd *cobra.Command, args []string) error {
	if fromStr == "" {
		return fmt.Errorf("--from is required")
	}
	from, err := parseTime(fromStr)
	if err != nil {
		return fmt.Errorf("parsing --from: %w", err)
	}
	to := time.Now()
	if toStr != "" {
		if to, err = parseTime(toStr); err != nil {
			return fmt.Errorf("parsing --to: %w", err)
		}
	}
	if !from.Before(to) {
		return fmt.Errorf("--from must be earlier than --to")
	}

	c, err := buildClient()
	if err != nil {
		return err
	}
	defer c.Close()
	workflows, err := c.FetchByTimeWindow(from, to)
	if err != nil {
		return fmt.Errorf("fetching workflows: %w", err)
	}
	fmt.Printf("Fetched %d archived workflows\n", len(workflows))

	report := analyzer.Analyze(workflows)
	report.QueryType = "window"
	report.QueryValue = fmt.Sprintf("%s → %s", from.Format(time.RFC3339), to.Format(time.RFC3339))

	return generateOutputs(report)
}

// generateOutputs drives all output formats for a completed report.
func generateOutputs(report *models.Report) error {
	// 1. Console + CSV + JSON
	if err := reporter.Generate(report, reporterOpts()); err != nil {
		return err
	}

	// 2. Markdown file
	if mdFile != "" {
		if err := confluence.WriteMarkdown(report, mdFile); err != nil {
			return fmt.Errorf("writing markdown: %w", err)
		}
		fmt.Printf("✓ Markdown  → %s\n", mdFile)
	}

	// 3. Confluence direct publish — triggered when any confluence flag or env
	// var is present. Validate() will catch any incomplete configuration.
	cfg := confluenceConfig()
	cfg.FromEnv()
	if cfg.BaseURL != "" || cfg.SpaceKey != "" || cfg.Title != "" {
		fmt.Println("Publishing to Confluence...")
		if err := confluence.Publish(report, cfg); err != nil {
			return fmt.Errorf("publishing to Confluence: %w", err)
		}
		fmt.Printf("✓ Confluence → %s/wiki/spaces/%s — %q\n",
			cfg.BaseURL, cfg.SpaceKey, cfg.Title)
	}

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildClient() (*client.Client, error) {
	tok := token
	if tok == "" {
		tok = os.Getenv("ARGO_TOKEN")
	}
	if server == "" {
		return nil, fmt.Errorf("--server is required")
	}
	return client.New(client.Config{
		BaseURL:            server,
		Namespace:          namespace,
		Token:              tok,
		InsecureSkipVerify: insecure,
		Timeout:            time.Duration(timeout) * time.Second,
		RatePerSecond:      ratePerSecond,
		Burst:              burst,
	}), nil
}

func reporterOpts() reporter.Options {
	return reporter.Options{
		CSVFile:  csvFile,
		JSONFile: jsonFile,
		Verbose:  verbose,
	}
}

func confluenceConfig() confluence.PublishConfig {
	return confluence.PublishConfig{
		BaseURL:  cfURL,
		User:     cfUser,
		Token:    cfToken,
		SpaceKey: cfSpace,
		Title:    cfTitle,
		ParentID: cfParentID,
	}
}

func parseTime(s string) (time.Time, error) {
	for _, f := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as a timestamp", s)
}
