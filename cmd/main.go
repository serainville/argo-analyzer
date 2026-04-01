package main

import (
	"fmt"
	"os"
	"time"

	"argo-analyzer/internal/analyzer"
	"argo-analyzer/internal/client"
	"argo-analyzer/internal/reporter"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	server    string
	namespace string
	token     string
	insecure  bool
	timeout   int

	// Output flags
	csvFile  string
	jsonFile string
	verbose  bool

	// Root command
	rootCmd = &cobra.Command{
		Use:   "argo-analyzer",
		Short: "Analyze failed archived workflows from an Argo Workflows server",
		Long: `argo-analyzer pulls archived workflows from an Argo Workflows REST API,
identifies failed workflows and their failed leaf nodes, then reports
metrics and a detailed failure breakdown to the console, CSV, and JSON.

Examples:
  # Analyze the last 100 archived workflows
  argo-analyzer count --server https://argo.example.com --count 100

  # Analyze workflows in a time window
  argo-analyzer window --server https://argo.example.com \
    --from 2024-01-01T00:00:00Z --to 2024-01-31T23:59:59Z

  # Output CSV and JSON reports
  argo-analyzer count --server https://argo.example.com --count 50 \
    --csv report.csv --json report.json`,
	}

	// count sub-command
	countCmd = &cobra.Command{
		Use:   "count",
		Short: "Fetch and analyze the N most recent archived workflows",
		RunE:  runCount,
	}

	// window sub-command
	windowCmd = &cobra.Command{
		Use:   "window",
		Short: "Fetch and analyze archived workflows within a time range",
		RunE:  runWindow,
	}
)

// count flags
var countLimit int

// window flags
var (
	fromStr string
	toStr   string
)

func init() {
	// Persistent flags (available to all sub-commands)
	rootCmd.PersistentFlags().StringVarP(&server, "server", "s", "", "Argo Workflows server URL (required)")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (empty = all namespaces)")
	rootCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "Bearer token for authentication (or set ARGO_TOKEN env var)")
	rootCmd.PersistentFlags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification")
	rootCmd.PersistentFlags().IntVar(&timeout, "timeout", 30, "HTTP request timeout in seconds")
	rootCmd.PersistentFlags().StringVar(&csvFile, "csv", "", "Write CSV report to this file path")
	rootCmd.PersistentFlags().StringVar(&jsonFile, "json", "", "Write JSON report to this file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show additional detail (e.g. node IDs)")

	// count flags
	countCmd.Flags().IntVarP(&countLimit, "count", "c", 50, "Number of recent archived workflows to fetch")

	// window flags
	windowCmd.Flags().StringVar(&fromStr, "from", "", "Start of time window (RFC3339, e.g. 2024-01-01T00:00:00Z) (required)")
	windowCmd.Flags().StringVar(&toStr, "to", "", "End of time window (RFC3339, e.g. 2024-01-31T23:59:59Z) (defaults to now)")

	rootCmd.AddCommand(countCmd)
	rootCmd.AddCommand(windowCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ── Sub-command handlers ──────────────────────────────────────────────────────

func runCount(cmd *cobra.Command, args []string) error {
	c, err := buildClient()
	if err != nil {
		return err
	}

	workflows, err := c.FetchByCount(countLimit)
	if err != nil {
		return fmt.Errorf("fetching workflows: %w", err)
	}

	fmt.Printf("Fetched %d archived workflows\n", len(workflows))

	report := analyzer.Analyze(workflows)
	report.QueryType = "count"
	report.QueryValue = fmt.Sprintf("%d", countLimit)

	return reporter.Generate(report, reporterOpts())
}

func runWindow(cmd *cobra.Command, args []string) error {
	if fromStr == "" {
		return fmt.Errorf("--from is required for the window command")
	}

	from, err := parseTime(fromStr)
	if err != nil {
		return fmt.Errorf("parsing --from: %w", err)
	}

	var to time.Time
	if toStr == "" {
		to = time.Now()
	} else {
		to, err = parseTime(toStr)
		if err != nil {
			return fmt.Errorf("parsing --to: %w", err)
		}
	}

	if !from.Before(to) {
		return fmt.Errorf("--from must be before --to")
	}

	c, err := buildClient()
	if err != nil {
		return err
	}

	workflows, err := c.FetchByTimeWindow(from, to)
	if err != nil {
		return fmt.Errorf("fetching workflows: %w", err)
	}

	fmt.Printf("Fetched %d archived workflows\n", len(workflows))

	report := analyzer.Analyze(workflows)
	report.QueryType = "window"
	report.QueryValue = fmt.Sprintf("%s → %s", from.Format(time.RFC3339), to.Format(time.RFC3339))

	return reporter.Generate(report, reporterOpts())
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func buildClient() (*client.Client, error) {
	// Allow token from environment variable
	tok := token
	if tok == "" {
		tok = os.Getenv("ARGO_TOKEN")
	}

	if server == "" {
		return nil, fmt.Errorf("--server is required (or set via the flag)")
	}

	return client.New(client.Config{
		BaseURL:            server,
		Namespace:          namespace,
		Token:              tok,
		InsecureSkipVerify: insecure,
		Timeout:            time.Duration(timeout) * time.Second,
	}), nil
}

func reporterOpts() reporter.Options {
	return reporter.Options{
		CSVFile:  csvFile,
		JSONFile: jsonFile,
		Verbose:  verbose,
	}
}

func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as RFC3339 timestamp", s)
}
