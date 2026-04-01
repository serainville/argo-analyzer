package reporter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"argo-analyzer/internal/models"

	"github.com/olekukonko/tablewriter"
)

// Options controls reporter output behavior
type Options struct {
	OutputDir string
	JSONFile  string
	CSVFile   string
	NoConsole bool
	Verbose   bool
}

// Generate produces all configured outputs for the report
func Generate(report *models.Report, opts Options) error {
	if !opts.NoConsole {
		printConsole(report, opts.Verbose)
	}

	if opts.CSVFile != "" {
		if err := writeCSV(report, opts.CSVFile); err != nil {
			return fmt.Errorf("writing CSV: %w", err)
		}
		fmt.Printf("\n✓ CSV report written to: %s\n", opts.CSVFile)
	}

	if opts.JSONFile != "" {
		if err := writeJSON(report, opts.JSONFile); err != nil {
			return fmt.Errorf("writing JSON: %w", err)
		}
		fmt.Printf("✓ JSON report written to: %s\n", opts.JSONFile)
	}

	return nil
}

// printConsole renders the report to stdout using tablewriter
func printConsole(report *models.Report, verbose bool) {
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Println("                ARGO WORKFLOWS FAILURE REPORT                  ")
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Printf("  Generated at : %s\n", report.GeneratedAt.Format(time.RFC1123))
	fmt.Printf("  Query        : %s = %s\n", report.QueryType, report.QueryValue)
	fmt.Println()

	// ── Metrics summary ──────────────────────────────────────────────────────
	fmt.Println("  METRICS")
	fmt.Println("  ───────────────────────────────────────────")

	metricsTable := tablewriter.NewWriter(os.Stdout)
	metricsTable.SetHeader([]string{"Metric", "Count", "Percentage"})
	metricsTable.SetBorder(false)
	metricsTable.SetColumnSeparator("│")
	metricsTable.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	metricsTable.SetAlignment(tablewriter.ALIGN_LEFT)
	metricsTable.SetHeaderLine(true)
	metricsTable.SetTablePadding("  ")

	metricsTable.Append([]string{
		"Total Workflows",
		strconv.Itoa(report.TotalWorkflows),
		"—",
	})
	metricsTable.Append([]string{
		"Successful",
		strconv.Itoa(report.SuccessfulCount),
		fmt.Sprintf("%.1f%%", report.SuccessPercentage),
	})
	metricsTable.Append([]string{
		"Failed",
		strconv.Itoa(report.FailedCount),
		fmt.Sprintf("%.1f%%", report.FailurePercentage),
	})
	metricsTable.Render()

	if report.FailedCount == 0 {
		fmt.Println("\n  ✓ No failed workflows found.")
		fmt.Println()
		return
	}

	// ── Failed workflows summary ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  FAILED WORKFLOWS")
	fmt.Println("  ───────────────────────────────────────────")

	wfTable := tablewriter.NewWriter(os.Stdout)
	wfTable.SetHeader([]string{"Workflow", "Namespace", "Phase", "Duration", "Failed Nodes", "Message"})
	wfTable.SetBorder(false)
	wfTable.SetColumnSeparator("│")
	wfTable.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	wfTable.SetAlignment(tablewriter.ALIGN_LEFT)
	wfTable.SetHeaderLine(true)
	wfTable.SetTablePadding("  ")
	wfTable.SetColWidth(40)
	wfTable.SetAutoWrapText(true)

	for _, wf := range report.FailedWorkflows {
		msg := truncate(wf.Message, 60)
		if msg == "" && len(wf.FailedNodes) > 0 {
			msg = truncate(wf.FailedNodes[0].Message, 60)
		}
		wfTable.Append([]string{
			wf.Name,
			wf.Namespace,
			wf.Phase,
			formatDuration(wf.Duration),
			strconv.Itoa(len(wf.FailedNodes)),
			msg,
		})
	}
	wfTable.Render()

	// ── Failed node details ───────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  FAILED NODE DETAILS")
	fmt.Println("  ───────────────────────────────────────────")

	nodeTable := tablewriter.NewWriter(os.Stdout)
	headers := []string{"Workflow", "Node", "Template", "Phase", "Exit Code", "Duration", "Failure Reason"}
	if verbose {
		headers = append(headers, "Node ID")
	}
	nodeTable.SetHeader(headers)
	nodeTable.SetBorder(false)
	nodeTable.SetColumnSeparator("│")
	nodeTable.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	nodeTable.SetAlignment(tablewriter.ALIGN_LEFT)
	nodeTable.SetHeaderLine(true)
	nodeTable.SetTablePadding("  ")
	nodeTable.SetColWidth(45)
	nodeTable.SetAutoWrapText(true)

	for _, wf := range report.FailedWorkflows {
		for _, node := range wf.FailedNodes {
			row := []string{
				wf.Name,
				node.NodeName,
				node.TemplateName,
				node.Phase,
				exitCodeDisplay(node.ExitCode),
				formatDuration(node.Duration),
				truncate(node.Message, 80),
			}
			if verbose {
				row = append(row, node.NodeID)
			}
			nodeTable.Append(row)
		}
	}
	nodeTable.Render()
	fmt.Println()
}

// writeCSV writes a flat CSV of all failed nodes
func writeCSV(report *models.Report, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"workflow_name",
		"namespace",
		"workflow_phase",
		"workflow_started_at",
		"workflow_finished_at",
		"workflow_duration_sec",
		"workflow_message",
		"node_id",
		"node_name",
		"template_name",
		"node_phase",
		"node_exit_code",
		"node_started_at",
		"node_finished_at",
		"node_duration_sec",
		"node_failure_reason",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, wf := range report.FailedWorkflows {
		for _, node := range wf.FailedNodes {
			row := []string{
				wf.Name,
				wf.Namespace,
				wf.Phase,
				timeStr(wf.StartedAt),
				timeStr(wf.FinishedAt),
				durationSec(wf.Duration),
				wf.Message,
				node.NodeID,
				node.NodeName,
				node.TemplateName,
				node.Phase,
				node.ExitCode,
				timeStr(node.StartedAt),
				timeStr(node.FinishedAt),
				durationSec(node.Duration),
				node.Message,
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}

	return nil
}

// JSONReport is the serialisable structure for JSON output
type JSONReport struct {
	GeneratedAt     string         `json:"generated_at"`
	QueryType       string         `json:"query_type"`
	QueryValue      string         `json:"query_value"`
	Metrics         JSONMetrics    `json:"metrics"`
	FailedWorkflows []JSONWorkflow `json:"failed_workflows"`
}

type JSONMetrics struct {
	TotalWorkflows    int     `json:"total_workflows"`
	SuccessfulCount   int     `json:"successful_count"`
	FailedCount       int     `json:"failed_count"`
	SuccessPercentage float64 `json:"success_percentage"`
	FailurePercentage float64 `json:"failure_percentage"`
}

type JSONWorkflow struct {
	Name        string     `json:"name"`
	Namespace   string     `json:"namespace"`
	Phase       string     `json:"phase"`
	StartedAt   string     `json:"started_at"`
	FinishedAt  string     `json:"finished_at"`
	DurationSec float64    `json:"duration_sec"`
	Message     string     `json:"message,omitempty"`
	FailedNodes []JSONNode `json:"failed_nodes"`
}

type JSONNode struct {
	NodeID       string  `json:"node_id"`
	NodeName     string  `json:"node_name"`
	TemplateName string  `json:"template_name,omitempty"`
	Phase        string  `json:"phase"`
	ExitCode     string  `json:"exit_code,omitempty"`
	StartedAt    string  `json:"started_at"`
	FinishedAt   string  `json:"finished_at"`
	DurationSec  float64 `json:"duration_sec"`
	Message      string  `json:"message,omitempty"`
}

// writeJSON writes the full report as a JSON file
func writeJSON(report *models.Report, path string) error {
	jr := JSONReport{
		GeneratedAt: report.GeneratedAt.Format(time.RFC3339),
		QueryType:   report.QueryType,
		QueryValue:  report.QueryValue,
		Metrics: JSONMetrics{
			TotalWorkflows:    report.TotalWorkflows,
			SuccessfulCount:   report.SuccessfulCount,
			FailedCount:       report.FailedCount,
			SuccessPercentage: roundTo(report.SuccessPercentage, 2),
			FailurePercentage: roundTo(report.FailurePercentage, 2),
		},
	}

	for _, wf := range report.FailedWorkflows {
		jw := JSONWorkflow{
			Name:        wf.Name,
			Namespace:   wf.Namespace,
			Phase:       wf.Phase,
			StartedAt:   timeStr(wf.StartedAt),
			FinishedAt:  timeStr(wf.FinishedAt),
			DurationSec: wf.Duration.Seconds(),
			Message:     wf.Message,
		}
		for _, node := range wf.FailedNodes {
			jw.FailedNodes = append(jw.FailedNodes, JSONNode{
				NodeID:       node.NodeID,
				NodeName:     node.NodeName,
				TemplateName: node.TemplateName,
				Phase:        node.Phase,
				ExitCode:     node.ExitCode,
				StartedAt:    timeStr(node.StartedAt),
				FinishedAt:   timeStr(node.FinishedAt),
				DurationSec:  node.Duration.Seconds(),
				Message:      node.Message,
			})
		}
		jr.FailedWorkflows = append(jr.FailedWorkflows, jw)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(jr)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func durationSec(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 2, 64)
}

func timeStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func exitCodeDisplay(code string) string {
	if code == "" {
		return "—"
	}
	return code
}

func roundTo(f float64, decimals int) float64 {
	pow := 1.0
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int(f*pow+0.5)) / pow
}
