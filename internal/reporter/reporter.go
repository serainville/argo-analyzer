// Package reporter renders a Report to the console (tablewriter), CSV, and JSON.
//
// Console sections (in order):
//  1. Header + query info
//  2. Metrics (totals + category breakdown)
//  3. Top failing templates
//  4. Failure patterns (cross-workflow)
//  5. DevEx insights (actionable recommendations)
//  6. Failed workflow summary
//  7. Failed node details
package reporter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"argo-analyzer/internal/models"

	"github.com/olekukonko/tablewriter"
)

// Options controls reporter output.
type Options struct {
	JSONFile  string
	CSVFile   string
	NoConsole bool
	Verbose   bool // show node IDs and classifier reasoning
}

// Generate writes all configured outputs.
func Generate(report *models.Report, opts Options) error {
	if !opts.NoConsole {
		printConsole(report, opts.Verbose)
	}
	if opts.CSVFile != "" {
		if err := writeCSV(report, opts.CSVFile); err != nil {
			return fmt.Errorf("writing CSV: %w", err)
		}
		fmt.Printf("✓ CSV  → %s\n", opts.CSVFile)
	}
	if opts.JSONFile != "" {
		if err := writeJSON(report, opts.JSONFile); err != nil {
			return fmt.Errorf("writing JSON: %w", err)
		}
		fmt.Printf("✓ JSON → %s\n", opts.JSONFile)
	}
	return nil
}

// ── Console ───────────────────────────────────────────────────────────────────

func printConsole(r *models.Report, verbose bool) {
	sep := strings.Repeat("═", 72)
	thin := strings.Repeat("─", 72)

	fmt.Println()
	fmt.Println(sep)
	fmt.Println("         ARGO WORKFLOWS — FAILURE ANALYSIS REPORT")
	fmt.Println(sep)
	fmt.Printf("  Generated : %s\n", r.GeneratedAt.Format(time.RFC1123))
	fmt.Printf("  Query     : %s = %s\n", r.QueryType, r.QueryValue)
	fmt.Println()

	printMetrics(r)
	printDurationMetrics(r)
	printByTemplate(r)
	printTopTemplates(r)
	printSlowestLists(r)

	if r.Metrics.FailedCount == 0 {
		fmt.Println("  ✓ No failed workflows found.")
		fmt.Println()
		return
	}

	printPatterns(r, thin)
	printInsights(r, thin)
	printFailedWorkflows(r, thin)
	printNodeDetails(r, thin, verbose)
}

func printMetrics(r *models.Report) {
	m := r.Metrics
	fmt.Println("  ── METRICS ─────────────────────────────────────────────────────────")
	fmt.Println()

	t := newTable([]string{"Metric", "Count", "%"})
	t.Append(row("Total workflows", itoa(m.TotalWorkflows), ""))
	t.Append(row("  Successful", itoa(m.SuccessfulCount), pct(m.SuccessPercentage)))
	t.Append(row("  Failed", itoa(m.FailedCount), pct(m.FailurePercentage)))
	t.Render()

	if m.FailedCount == 0 {
		return
	}

	fmt.Println()
	fmt.Println("  ── FAILURE BREAKDOWN BY CATEGORY ───────────────────────────────────")
	fmt.Println()

	t2 := newTable([]string{"Category", "Count", "% of Failures", "Meaning"})
	t2.Append(row(
		"Platform",
		itoa(m.PlatformCount),
		pctOf(m.PlatformCount, m.FailedCount),
		"Infrastructure, scheduler, Argo controller",
	))
	t2.Append(row(
		"Application",
		itoa(m.ApplicationCount),
		pctOf(m.ApplicationCount, m.FailedCount),
		"User workload code or configuration",
	))
	t2.Append(row(
		"DevEx gap",
		itoa(m.DevExCount),
		pctOf(m.DevExCount, m.FailedCount),
		"Missing guardrails, retries, validation, docs",
	))
	t2.Append(row(
		"Unknown",
		itoa(m.UnknownCount),
		pctOf(m.UnknownCount, m.FailedCount),
		"Could not be classified",
	))
	t2.Render()
	fmt.Println()
}

func printTopTemplates(r *models.Report) {
	if len(r.Metrics.TopFailingTemplates) == 0 {
		return
	}
	fmt.Println("  ── TOP FAILING TEMPLATES ───────────────────────────────────────────")
	fmt.Println()

	t := newTable([]string{"#", "Template", "Failures", "Dominant Category"})
	for i, tf := range r.Metrics.TopFailingTemplates {
		t.Append(row(
			strconv.Itoa(i+1),
			tf.TemplateName,
			itoa(tf.Count),
			string(tf.DominantCategory),
		))
	}
	t.Render()
	fmt.Println()
}

func printDurationMetrics(r *models.Report) {
	m := r.Metrics
	// Only render if we have at least one data point
	if m.AllWorkflowDuration.Count == 0 {
		return
	}
	fmt.Println("  ── DURATION METRICS ────────────────────────────────────────────────")
	fmt.Println()

	t := newTable([]string{"Scope", "Count", "Min", "Max", "Mean", "Median"})
	addDurationRow := func(label string, s models.DurationStats) {
		if s.Count == 0 {
			return
		}
		t.Append(row(
			label,
			itoa(s.Count),
			formatDuration(s.Min),
			formatDuration(s.Max),
			formatDuration(s.Mean),
			formatDuration(s.Median),
		))
	}
	addDurationRow("All (terminal)", m.AllWorkflowDuration)
	addDurationRow("  Successful", m.SuccessfulDuration)
	addDurationRow("  Failed", m.FailedDuration)
	t.Render()
	fmt.Println()
}

func printByTemplate(r *models.Report) {
	if len(r.Metrics.ByTemplate) == 0 {
		return
	}
	fmt.Println("  ── METRICS BY WORKFLOW TEMPLATE ────────────────────────────────────")
	fmt.Println()

	// Sort template names for stable output
	names := make([]string, 0, len(r.Metrics.ByTemplate))
	for k := range r.Metrics.ByTemplate {
		names = append(names, k)
	}
	sort.Strings(names)

	// Run counts table
	tCounts := newTable([]string{"WF Template", "Total", "Successful", "Failed", "Fail %",
		"PLT", "APP", "DEV", "UNK"})
	for _, name := range names {
		s := r.Metrics.ByTemplate[name]
		tCounts.Append(row(
			name,
			itoa(s.TotalCount),
			itoa(s.SuccessCount),
			itoa(s.FailCount),
			pctOf(s.FailCount, s.TotalCount),
			itoa(s.PlatformCount),
			itoa(s.ApplicationCount),
			itoa(s.DevExCount),
			itoa(s.UnknownCount),
		))
	}
	tCounts.Render()
	fmt.Println()

	// Duration stats table
	tDur := newTable([]string{"WF Template", "Scope", "Count", "Min", "Max", "Mean", "Median"})
	for _, name := range names {
		s := r.Metrics.ByTemplate[name]
		addRow := func(scope string, ds models.DurationStats) {
			if ds.Count == 0 {
				return
			}
			tDur.Append(row(
				name, scope,
				itoa(ds.Count),
				formatDuration(ds.Min),
				formatDuration(ds.Max),
				formatDuration(ds.Mean),
				formatDuration(ds.Median),
			))
		}
		addRow("All", s.AllDuration)
		addRow("  Successful", s.SuccessDuration)
		addRow("  Failed", s.FailedDuration)
	}
	tDur.Render()
	fmt.Println()
}

func printSlowestLists(r *models.Report) {
	m := r.Metrics
	if len(m.SlowestWorkflows) == 0 && len(m.SlowestTemplates) == 0 {
		return
	}

	if len(m.SlowestWFTemplates) > 0 {
		fmt.Println("  ── SLOWEST WORKFLOW TEMPLATES (median duration) ───────────────────")
		fmt.Println()
		t := newTable([]string{"#", "WF Template", "Runs", "Median Duration"})
		for i, e := range m.SlowestWFTemplates {
			t.Append(row(
				strconv.Itoa(i+1),
				e.Name,
				e.Phase, // holds "N runs" string
				formatDuration(e.Duration),
			))
		}
		t.Render()
		fmt.Println()
	}

	if len(m.SlowestWorkflows) > 0 {
		fmt.Println("  ── SLOWEST WORKFLOWS ───────────────────────────────────────────────")
		fmt.Println()
		t := newTable([]string{"#", "Workflow", "WF Template", "Phase", "Duration"})
		for i, e := range m.SlowestWorkflows {
			t.Append(row(
				strconv.Itoa(i+1),
				e.Name,
				orDash(e.TemplateName),
				e.Phase,
				formatDuration(e.Duration),
			))
		}
		t.Render()
		fmt.Println()
	}

	if len(m.SlowestTemplates) > 0 {
		fmt.Println("  ── SLOWEST FAILED TEMPLATE STEPS ──────────────────────────────────")
		fmt.Println()
		t := newTable([]string{"#", "Node", "Template", "Workflow", "Duration"})
		for i, e := range m.SlowestTemplates {
			t.Append(row(
				strconv.Itoa(i+1),
				e.Name,
				orDash(e.TemplateName),
				e.WorkflowName,
				formatDuration(e.Duration),
			))
		}
		t.Render()
		fmt.Println()
	}
}

func printPatterns(r *models.Report, thin string) {
	if len(r.Patterns) == 0 {
		return
	}
	fmt.Println("  ── RECURRING FAILURE PATTERNS ──────────────────────────────────────")
	fmt.Println()

	t := newTable([]string{"#", "WF Template", "Category", "Subtype", "Template", "Hits", "Workflows", "Flaky", "Representative Message"})
	for i, p := range r.Patterns {
		flaky := ""
		if p.IsFlaky {
			flaky = "✓"
		}
		t.Append(row(
			strconv.Itoa(i+1),
			orDash(strings.Join(p.AffectedWFTemplates, ", ")),
			string(p.Category),
			string(p.Subtype),
			orDash(p.TemplateName),
			itoa(p.OccurrenceCount),
			itoa(len(p.AffectedWorkflows)),
			flaky,
			truncate(p.RepresentativeMessage, 55),
		))
	}
	t.Render()
	fmt.Println()
	_ = thin
}

func printInsights(r *models.Report, _ string) {
	if len(r.Insights) == 0 {
		return
	}
	fmt.Println("  ── DEVEX INSIGHTS & RECOMMENDATIONS ────────────────────────────────")
	fmt.Println()

	for i, ins := range r.Insights {
		priorityIcon := priorityIcon(ins.Priority)
		fmt.Printf("  %d. %s [%s] %s\n", i+1, priorityIcon, strings.ToUpper(string(ins.Priority)), ins.Title)
		fmt.Printf("     %s\n", wordWrap(ins.Description, 68, "     "))
		fmt.Printf("     → %s\n", wordWrap(ins.Recommendation, 66, "       "))
		if ins.SupportingData != "" {
			fmt.Printf("     Data: %s\n", ins.SupportingData)
		}
		fmt.Println()
	}
}

func printFailedWorkflows(r *models.Report, _ string) {
	fmt.Println("  ── FAILED WORKFLOWS ────────────────────────────────────────────────")
	fmt.Println()

	t := newTable([]string{"Workflow", "Template", "Namespace", "Duration", "Failed Nodes", "Categories"})
	for _, wf := range r.FailedWorkflows {
		cats := categoryBadges(wf.FailedNodes)
		t.Append(row(
			wf.Name,
			orDash(wf.WorkflowTemplate),
			wf.Namespace,
			formatDuration(wf.Duration),
			itoa(len(wf.FailedNodes)),
			cats,
		))
	}
	t.Render()
	fmt.Println()
}

func printNodeDetails(r *models.Report, _ string, verbose bool) {
	fmt.Println("  ── FAILED NODE DETAILS ─────────────────────────────────────────────")
	fmt.Println()

	headers := []string{"WF Template", "Node", "Template", "Path", "Category", "Subtype", "Conf.", "Exit", "Failure Reason"}
	if verbose {
		headers = append(headers, "Classifier Reasoning")
	}

	t := newTable(headers)
	for _, wf := range r.FailedWorkflows {
		for _, node := range wf.FailedNodes {
			c := node.Classification
			pathStr := strings.Join(node.NodePath, " → ")
			r := []string{
				orDash(node.WorkflowTemplate),
				node.NodeName,
				orDash(node.TemplateName),
				truncate(pathStr, 55),
				string(c.Category),
				string(c.Subtype),
				string(c.Confidence),
				orDash(node.ExitCode),
				truncate(node.Message, 55),
			}
			if verbose {
				r = append(r, truncate(c.Reasoning, 80))
			}
			t.Append(r)
		}
	}
	t.Render()
	fmt.Println()
}

// ── CSV ───────────────────────────────────────────────────────────────────────

func writeCSV(report *models.Report, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Sheet 1 style: all failed nodes (flat)
	header := []string{
		"workflow_name", "workflow_template", "namespace", "workflow_phase",
		"workflow_started_at", "workflow_finished_at", "workflow_duration_sec",
		"node_id", "node_name", "node_path", "template_name",
		"node_phase", "node_exit_code",
		"node_started_at", "node_finished_at", "node_duration_sec",
		"failure_message",
		"category", "subtype", "confidence", "classified_by", "reasoning",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, wf := range report.FailedWorkflows {
		for _, node := range wf.FailedNodes {
			c := node.Classification
			pathStr := strings.Join(node.NodePath, " → ")
			if err := w.Write([]string{
				wf.Name, node.WorkflowTemplate, wf.Namespace, wf.Phase,
				timeStr(wf.StartedAt), timeStr(wf.FinishedAt), durationSec(wf.Duration),
				node.NodeID, node.NodeName, pathStr, node.TemplateName,
				node.Phase, node.ExitCode,
				timeStr(node.StartedAt), timeStr(node.FinishedAt), durationSec(node.Duration),
				node.Message,
				string(c.Category), string(c.Subtype), string(c.Confidence), c.ClassifiedBy, c.Reasoning,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ── JSON ──────────────────────────────────────────────────────────────────────

type jsonReport struct {
	GeneratedAt     string                `json:"generated_at"`
	QueryType       string                `json:"query_type"`
	QueryValue      string                `json:"query_value"`
	Metrics         jsonMetrics           `json:"metrics"`
	Patterns        []jsonPattern         `json:"patterns"`
	Insights        []models.DevExInsight `json:"insights"`
	FailedWorkflows []jsonWorkflow        `json:"failed_workflows"`
}

type jsonDurationStats struct {
	Count     int     `json:"count"`
	MinSec    float64 `json:"min_sec"`
	MaxSec    float64 `json:"max_sec"`
	MeanSec   float64 `json:"mean_sec"`
	MedianSec float64 `json:"median_sec"`
}

type jsonSlowestEntry struct {
	Name         string  `json:"name"`
	WorkflowName string  `json:"workflow_name,omitempty"`
	TemplateName string  `json:"template_name,omitempty"`
	Phase        string  `json:"phase"`
	DurationSec  float64 `json:"duration_sec"`
}

type jsonMetrics struct {
	TotalWorkflows      int                           `json:"total_workflows"`
	SuccessfulCount     int                           `json:"successful_count"`
	FailedCount         int                           `json:"failed_count"`
	SuccessPercentage   float64                       `json:"success_percentage"`
	FailurePercentage   float64                       `json:"failure_percentage"`
	PlatformCount       int                           `json:"platform_failures"`
	ApplicationCount    int                           `json:"application_failures"`
	DevExCount          int                           `json:"devex_failures"`
	UnknownCount        int                           `json:"unknown_failures"`
	TopFailingTemplates []models.TemplateFailureCount `json:"top_failing_templates"`
	DurationAll         jsonDurationStats             `json:"duration_all_workflows"`
	DurationFailed      jsonDurationStats             `json:"duration_failed_workflows"`
	DurationSuccessful  jsonDurationStats             `json:"duration_successful_workflows"`
	SlowestWFTemplates  []jsonSlowestEntry            `json:"slowest_wf_templates"`
	SlowestWorkflows    []jsonSlowestEntry            `json:"slowest_workflows"`
	SlowestTemplates    []jsonSlowestEntry            `json:"slowest_template_steps"`
	ByTemplate          map[string]jsonTemplateStats  `json:"by_template,omitempty"`
}

type jsonTemplateStats struct {
	TemplateName     string            `json:"template_name"`
	TotalCount       int               `json:"total_count"`
	SuccessCount     int               `json:"success_count"`
	FailCount        int               `json:"fail_count"`
	PlatformCount    int               `json:"platform_failures"`
	ApplicationCount int               `json:"application_failures"`
	DevExCount       int               `json:"devex_failures"`
	UnknownCount     int               `json:"unknown_failures"`
	AllDuration      jsonDurationStats `json:"duration_all"`
	SuccessDuration  jsonDurationStats `json:"duration_successful"`
	FailedDuration   jsonDurationStats `json:"duration_failed"`
}

type jsonPattern struct {
	PatternKey            string   `json:"pattern_key"`
	Category              string   `json:"category"`
	Subtype               string   `json:"subtype"`
	TemplateName          string   `json:"template_name,omitempty"`
	OccurrenceCount       int      `json:"occurrence_count"`
	AffectedWorkflows     []string `json:"affected_workflows"`
	AffectedNamespaces    []string `json:"affected_namespaces"`
	FirstSeen             string   `json:"first_seen,omitempty"`
	LastSeen              string   `json:"last_seen,omitempty"`
	IsFlaky               bool     `json:"is_flaky"`
	TypicalExitCodes      []string `json:"typical_exit_codes,omitempty"`
	AffectedWFTemplates   []string `json:"affected_wf_templates,omitempty"`
	RepresentativeMessage string   `json:"representative_message"`
}

type jsonWorkflow struct {
	Name        string     `json:"name"`
	Namespace   string     `json:"namespace"`
	Phase       string     `json:"phase"`
	StartedAt   string     `json:"started_at"`
	FinishedAt  string     `json:"finished_at"`
	DurationSec float64    `json:"duration_sec"`
	Message     string     `json:"message,omitempty"`
	FailedNodes []jsonNode `json:"failed_nodes"`
}

type jsonNode struct {
	WorkflowTemplate string                `json:"workflow_template,omitempty"`
	NodeID           string                `json:"node_id"`
	NodeName         string                `json:"node_name"`
	NodePath         []string              `json:"node_path"`
	TemplateName     string                `json:"template_name,omitempty"`
	Phase            string                `json:"phase"`
	ExitCode         string                `json:"exit_code,omitempty"`
	StartedAt        string                `json:"started_at"`
	FinishedAt       string                `json:"finished_at"`
	DurationSec      float64               `json:"duration_sec"`
	Message          string                `json:"message,omitempty"`
	Classification   models.Classification `json:"classification"`
}

func writeJSON(report *models.Report, path string) error {
	jr := jsonReport{
		GeneratedAt: report.GeneratedAt.Format(time.RFC3339),
		QueryType:   report.QueryType,
		QueryValue:  report.QueryValue,
		Metrics: jsonMetrics{
			TotalWorkflows:      report.Metrics.TotalWorkflows,
			SuccessfulCount:     report.Metrics.SuccessfulCount,
			FailedCount:         report.Metrics.FailedCount,
			SuccessPercentage:   round2(report.Metrics.SuccessPercentage),
			FailurePercentage:   round2(report.Metrics.FailurePercentage),
			PlatformCount:       report.Metrics.PlatformCount,
			ApplicationCount:    report.Metrics.ApplicationCount,
			DevExCount:          report.Metrics.DevExCount,
			UnknownCount:        report.Metrics.UnknownCount,
			TopFailingTemplates: report.Metrics.TopFailingTemplates,
			DurationAll:         toJSONDuration(report.Metrics.AllWorkflowDuration),
			DurationFailed:      toJSONDuration(report.Metrics.FailedDuration),
			DurationSuccessful:  toJSONDuration(report.Metrics.SuccessfulDuration),
			SlowestWFTemplates:  toJSONSlowest(report.Metrics.SlowestWFTemplates),
			SlowestWorkflows:    toJSONSlowest(report.Metrics.SlowestWorkflows),
			SlowestTemplates:    toJSONSlowest(report.Metrics.SlowestTemplates),
			ByTemplate:          toJSONByTemplate(report.Metrics.ByTemplate),
		},
		Insights: report.Insights,
	}

	for _, p := range report.Patterns {
		jp := jsonPattern{
			PatternKey:            p.PatternKey,
			Category:              string(p.Category),
			Subtype:               string(p.Subtype),
			TemplateName:          p.TemplateName,
			OccurrenceCount:       p.OccurrenceCount,
			AffectedWorkflows:     p.AffectedWorkflows,
			AffectedNamespaces:    p.AffectedNamespaces,
			IsFlaky:               p.IsFlaky,
			TypicalExitCodes:      p.TypicalExitCodes,
			AffectedWFTemplates:   p.AffectedWFTemplates,
			RepresentativeMessage: p.RepresentativeMessage,
		}
		if !p.FirstSeen.IsZero() {
			jp.FirstSeen = p.FirstSeen.Format(time.RFC3339)
		}
		if !p.LastSeen.IsZero() {
			jp.LastSeen = p.LastSeen.Format(time.RFC3339)
		}
		jr.Patterns = append(jr.Patterns, jp)
	}

	for _, wf := range report.FailedWorkflows {
		jw := jsonWorkflow{
			Name:        wf.Name,
			Namespace:   wf.Namespace,
			Phase:       wf.Phase,
			StartedAt:   timeStr(wf.StartedAt),
			FinishedAt:  timeStr(wf.FinishedAt),
			DurationSec: wf.Duration.Seconds(),
			Message:     wf.Message,
		}
		for _, node := range wf.FailedNodes {
			jw.FailedNodes = append(jw.FailedNodes, jsonNode{
				WorkflowTemplate: node.WorkflowTemplate,
				NodeID:           node.NodeID,
				NodeName:         node.NodeName,
				NodePath:         node.NodePath,
				TemplateName:     node.TemplateName,
				Phase:            node.Phase,
				ExitCode:         node.ExitCode,
				StartedAt:        timeStr(node.StartedAt),
				FinishedAt:       timeStr(node.FinishedAt),
				DurationSec:      node.Duration.Seconds(),
				Message:          node.Message,
				Classification:   node.Classification,
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

// toJSONByTemplate converts the ByTemplate map to its JSON-serialisable form.
func toJSONByTemplate(byTmpl map[string]models.WorkflowTemplateStats) map[string]jsonTemplateStats {
	if len(byTmpl) == 0 {
		return nil
	}
	out := make(map[string]jsonTemplateStats, len(byTmpl))
	for k, s := range byTmpl {
		out[k] = jsonTemplateStats{
			TemplateName:     s.TemplateName,
			TotalCount:       s.TotalCount,
			SuccessCount:     s.SuccessCount,
			FailCount:        s.FailCount,
			PlatformCount:    s.PlatformCount,
			ApplicationCount: s.ApplicationCount,
			DevExCount:       s.DevExCount,
			UnknownCount:     s.UnknownCount,
			AllDuration:      toJSONDuration(s.AllDuration),
			SuccessDuration:  toJSONDuration(s.SuccessDuration),
			FailedDuration:   toJSONDuration(s.FailedDuration),
		}
	}
	return out
}

// toJSONDuration converts a DurationStats to its JSON-serialisable form.
func toJSONDuration(s models.DurationStats) jsonDurationStats {
	return jsonDurationStats{
		Count:     s.Count,
		MinSec:    round2(s.Min.Seconds()),
		MaxSec:    round2(s.Max.Seconds()),
		MeanSec:   round2(s.Mean.Seconds()),
		MedianSec: round2(s.Median.Seconds()),
	}
}

// toJSONSlowest converts a []SlowestEntry to its JSON-serialisable form.
func toJSONSlowest(entries []models.SlowestEntry) []jsonSlowestEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]jsonSlowestEntry, len(entries))
	for i, e := range entries {
		out[i] = jsonSlowestEntry{
			Name:         e.Name,
			WorkflowName: e.WorkflowName,
			TemplateName: e.TemplateName,
			Phase:        e.Phase,
			DurationSec:  round2(e.Duration.Seconds()),
		}
	}
	return out
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newTable(headers []string) *tablewriter.Table {
	t := tablewriter.NewWriter(os.Stdout)
	t.SetHeader(headers)
	t.SetBorder(false)
	t.SetColumnSeparator("│")
	t.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	t.SetAlignment(tablewriter.ALIGN_LEFT)
	t.SetHeaderLine(true)
	t.SetTablePadding("  ")
	t.SetColWidth(40)
	t.SetAutoWrapText(true)
	return t
}

func row(cells ...string) []string { return cells }

func itoa(n int) string { return strconv.Itoa(n) }

func pct(f float64) string {
	if f == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", f)
}

func pctOf(n, total int) string {
	if total == 0 || n == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

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

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// categoryBadges produces a compact summary of failure categories for a node list.
func categoryBadges(nodes []models.FailedNode) string {
	counts := map[models.FailureCategory]int{}
	for _, n := range nodes {
		counts[n.Classification.Category]++
	}
	var parts []string
	for _, cat := range []models.FailureCategory{
		models.CategoryPlatform,
		models.CategoryApplication,
		models.CategoryDevEx,
		models.CategoryUnknown,
	} {
		if n := counts[cat]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s×%d", catAbbrev(cat), n))
		}
	}
	return strings.Join(parts, " ")
}

func catAbbrev(c models.FailureCategory) string {
	switch c {
	case models.CategoryPlatform:
		return "PLT"
	case models.CategoryApplication:
		return "APP"
	case models.CategoryDevEx:
		return "DEV"
	default:
		return "UNK"
	}
}

func priorityIcon(p models.InsightPriority) string {
	switch p {
	case models.PriorityHigh:
		return "●"
	case models.PriorityMedium:
		return "◐"
	default:
		return "○"
	}
}

// wordWrap wraps text at approximately maxWidth chars, indenting continuation
// lines with the given prefix.
func wordWrap(s string, maxWidth int, prefix string) string {
	if len(s) <= maxWidth {
		return s
	}
	words := strings.Fields(s)
	var lines []string
	line := ""
	for _, w := range words {
		if len(line)+len(w)+1 > maxWidth && line != "" {
			lines = append(lines, line)
			line = w
		} else {
			if line == "" {
				line = w
			} else {
				line += " " + w
			}
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"+prefix)
}
