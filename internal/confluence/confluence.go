// Package confluence provides two output modes for sharing reports with
// non-engineering stakeholders via Confluence:
//
//  1. Markdown rendering  — writes a self-contained .md file that can be
//     imported into Confluence manually (Pages → Import → Markdown) or read
//     standalone in any Markdown viewer.
//
//  2. Direct publish      — creates or updates a Confluence page via the
//     Confluence Cloud/Data-Center REST API using Confluence Storage Format
//     (a subset of XHTML). Storage format is the only reliable way to get
//     fully-rendered tables and structured panels into a Confluence page
//     programmatically; Markdown submitted via the API is not re-rendered.
//
// Authentication uses HTTP Basic auth: username + API token.
// For Confluence Cloud: username = your Atlassian account email.
// For Confluence Data Center / Server: username = your username.
//
// Environment variables (alternative to flags):
//
//	CONFLUENCE_URL    base URL, e.g. https://yourorg.atlassian.net
//	CONFLUENCE_USER   email or username
//	CONFLUENCE_TOKEN  API token or personal access token
package confluence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"argo-analyzer/internal/models"
)

// PublishConfig holds everything needed to talk to the Confluence REST API.
type PublishConfig struct {
	BaseURL  string // e.g. https://yourorg.atlassian.net
	User     string // Atlassian account email or username
	Token    string // API token or personal access token
	SpaceKey string // e.g. "PLAT" or "~username"
	Title    string // page title (created or updated)
	ParentID string // optional parent page ID; root of space if empty
}

// FromEnv fills any zero-value fields from environment variables.
func (c *PublishConfig) FromEnv() {
	if c.BaseURL == "" {
		c.BaseURL = os.Getenv("CONFLUENCE_URL")
	}
	if c.User == "" {
		c.User = os.Getenv("CONFLUENCE_USER")
	}
	if c.Token == "" {
		c.Token = os.Getenv("CONFLUENCE_TOKEN")
	}
}

// Validate returns an error if required fields are missing.
func (c *PublishConfig) Validate() error {
	var missing []string
	if c.BaseURL == "" {
		missing = append(missing, "--confluence-url / CONFLUENCE_URL")
	}
	if c.User == "" {
		missing = append(missing, "--confluence-user / CONFLUENCE_USER")
	}
	if c.Token == "" {
		missing = append(missing, "--confluence-token / CONFLUENCE_TOKEN")
	}
	if c.SpaceKey == "" {
		missing = append(missing, "--confluence-space")
	}
	if c.Title == "" {
		missing = append(missing, "--confluence-title")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing Confluence configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ── Markdown output ───────────────────────────────────────────────────────────

// WriteMarkdown renders the report as a Markdown file and writes it to path.
func WriteMarkdown(report *models.Report, path string) error {
	md := RenderMarkdown(report)
	if err := os.WriteFile(path, []byte(md), 0644); err != nil {
		return fmt.Errorf("writing markdown file: %w", err)
	}
	return nil
}

// RenderMarkdown returns the full report as a Markdown string.
// The output is structured for readability both as a standalone document and
// when imported into Confluence.
func RenderMarkdown(report *models.Report) string {
	var b strings.Builder
	m := report.Metrics

	// ── Header ────────────────────────────────────────────────────────────────
	b.WriteString("# Argo Workflows — Failure Analysis Report\n\n")
	b.WriteString(fmt.Sprintf("**Generated:** %s  \n", report.GeneratedAt.Format(time.RFC1123)))
	b.WriteString(fmt.Sprintf("**Query:** %s = %s  \n\n", report.QueryType, report.QueryValue))

	// ── Summary metrics ───────────────────────────────────────────────────────
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Count | % |\n")
	b.WriteString("|--------|------:|---:|\n")
	b.WriteString(fmt.Sprintf("| Total workflows | %d | — |\n", m.TotalWorkflows))
	b.WriteString(fmt.Sprintf("| ✅ Successful | %d | %.1f%% |\n", m.SuccessfulCount, m.SuccessPercentage))
	b.WriteString(fmt.Sprintf("| ❌ Failed | %d | %.1f%% |\n\n", m.FailedCount, m.FailurePercentage))

	// ── Failure category breakdown ────────────────────────────────────────────
	if m.FailedCount > 0 {
		b.WriteString("## Failure Breakdown by Category\n\n")
		b.WriteString("| Category | Count | % of Failures | Meaning |\n")
		b.WriteString("|----------|------:|--------------:|---------|\n")
		b.WriteString(fmt.Sprintf("| 🔧 Platform | %d | %s | Infrastructure, scheduler, Argo controller |\n",
			m.PlatformCount, mdPctOf(m.PlatformCount, m.FailedCount)))
		b.WriteString(fmt.Sprintf("| 💻 Application | %d | %s | User workload code or configuration |\n",
			m.ApplicationCount, mdPctOf(m.ApplicationCount, m.FailedCount)))
		b.WriteString(fmt.Sprintf("| 🛠 DevEx gap | %d | %s | Missing guardrails, retries, validation, docs |\n",
			m.DevExCount, mdPctOf(m.DevExCount, m.FailedCount)))
		b.WriteString(fmt.Sprintf("| ❓ Unknown | %d | %s | Could not be classified |\n\n",
			m.UnknownCount, mdPctOf(m.UnknownCount, m.FailedCount)))
	}

	// ── Duration metrics ──────────────────────────────────────────────────────
	if m.AllWorkflowDuration.Count > 0 {
		b.WriteString("## Duration Metrics\n\n")
		b.WriteString("| Scope | Count | Min | Max | Mean | Median |\n")
		b.WriteString("|-------|------:|----:|----:|-----:|-------:|\n")
		writeMDDurationRow(&b, "All (terminal)", m.AllWorkflowDuration)
		writeMDDurationRow(&b, "Successful", m.SuccessfulDuration)
		writeMDDurationRow(&b, "Failed", m.FailedDuration)
		b.WriteString("\n")
	}

	// ── Slowest workflows ─────────────────────────────────────────────────────
	if len(m.SlowestWorkflows) > 0 {
		b.WriteString("## Slowest Workflows (Top 10)\n\n")
		b.WriteString("| # | Workflow | WF Template | Phase | Duration |\n")
		b.WriteString("|---|---------|-------------|-------|----------|\n")
		for i, e := range m.SlowestWorkflows {
			b.WriteString(fmt.Sprintf("| %d | `%s` | %s | %s | %s |\n",
				i+1, e.Name, mdDash(e.TemplateName), e.Phase, mdDuration(e.Duration)))
		}
		b.WriteString("\n")
	}

	// ── Slowest failed template steps ─────────────────────────────────────────
	if len(m.SlowestTemplates) > 0 {
		b.WriteString("## Slowest Failed Template Steps (Top 10)\n\n")
		b.WriteString("| # | Node | Template | Workflow | Duration |\n")
		b.WriteString("|---|------|----------|----------|----------|\n")
		for i, e := range m.SlowestTemplates {
			b.WriteString(fmt.Sprintf("| %d | `%s` | %s | `%s` | %s |\n",
				i+1, e.Name, mdDash(e.TemplateName), e.WorkflowName, mdDuration(e.Duration)))
		}
		b.WriteString("\n")
	}

	// ── Top failing templates ─────────────────────────────────────────────────
	if len(m.TopFailingTemplates) > 0 {
		b.WriteString("## Top Failing Templates\n\n")
		b.WriteString("| # | Template | Failures | Dominant Category |\n")
		b.WriteString("|---|----------|---------|------------------|\n")
		for i, tf := range m.TopFailingTemplates {
			b.WriteString(fmt.Sprintf("| %d | `%s` | %d | %s |\n",
				i+1, tf.TemplateName, tf.Count, string(tf.DominantCategory)))
		}
		b.WriteString("\n")
	}

	// ── Recurring patterns ────────────────────────────────────────────────────
	if len(report.Patterns) > 0 {
		b.WriteString("## Recurring Failure Patterns\n\n")
		b.WriteString("| # | Category | Subtype | Template | Occurrences | Workflows | Flaky | Representative Message |\n")
		b.WriteString("|---|----------|---------|----------|------------:|----------:|:-----:|------------------------|\n")
		for i, p := range report.Patterns {
			flaky := ""
			if p.IsFlaky {
				flaky = "✓"
			}
			b.WriteString(fmt.Sprintf("| %d | %s | `%s` | %s | %d | %d | %s | %s |\n",
				i+1,
				string(p.Category),
				string(p.Subtype),
				mdCode(p.TemplateName),
				p.OccurrenceCount,
				len(p.AffectedWorkflows),
				flaky,
				mdEscape(truncate(p.RepresentativeMessage, 80)),
			))
		}
		b.WriteString("\n")
	}

	// ── DevEx insights ────────────────────────────────────────────────────────
	if len(report.Insights) > 0 {
		b.WriteString("## DevEx Insights & Recommendations\n\n")
		for i, ins := range report.Insights {
			icon := mdPriorityIcon(ins.Priority)
			b.WriteString(fmt.Sprintf("### %d. %s %s\n\n", i+1, icon, ins.Title))
			b.WriteString(fmt.Sprintf("**Priority:** %s  \n", strings.ToUpper(string(ins.Priority))))
			if len(ins.AffectedTemplates) > 0 {
				quoted := make([]string, len(ins.AffectedTemplates))
				for j, t := range ins.AffectedTemplates {
					quoted[j] = "`" + t + "`"
				}
				b.WriteString(fmt.Sprintf("**Affected templates:** %s  \n", strings.Join(quoted, ", ")))
			}
			b.WriteString(fmt.Sprintf("**Supporting data:** %s  \n\n", ins.SupportingData))
			b.WriteString(ins.Description + "\n\n")
			b.WriteString(fmt.Sprintf("**Recommendation:** %s\n\n", ins.Recommendation))
		}
	}

	// ── Failed workflow summary ───────────────────────────────────────────────
	if len(report.FailedWorkflows) > 0 {
		b.WriteString("## Failed Workflows\n\n")
		b.WriteString("| Workflow | WF Template | Namespace | Duration | Failed Nodes | Categories |\n")
		b.WriteString("|----------|-------------|-----------|----------|-------------:|------------|\n")
		for _, wf := range report.FailedWorkflows {
			tmpl := ""
			if len(wf.FailedNodes) > 0 {
				tmpl = wf.FailedNodes[0].WorkflowTemplate
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %d | %s |\n",
				wf.Name,
				mdDash(tmpl),
				wf.Namespace,
				mdDuration(wf.Duration),
				len(wf.FailedNodes),
				mdCategoryBadges(wf.FailedNodes),
			))
		}
		b.WriteString("\n")

		// ── Failed node details ───────────────────────────────────────────────
		b.WriteString("## Failed Node Details\n\n")
		b.WriteString("| WF Template | Node | Template | Path | Category | Subtype | Confidence | Exit | Failure Reason |\n")
		b.WriteString("|-------------|------|----------|------|----------|---------|:----------:|:----:|----------------|\n")
		for _, wf := range report.FailedWorkflows {
			for _, node := range wf.FailedNodes {
				c := node.Classification
				path := strings.Join(node.NodePath, " → ")
				b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s | %s | `%s` | %s | %s | %s |\n",
					mdCode(node.WorkflowTemplate),
					node.NodeName,
					mdCode(node.TemplateName),
					mdEscape(truncate(path, 60)),
					string(c.Category),
					string(c.Subtype),
					string(c.Confidence),
					mdDash(node.ExitCode),
					mdEscape(truncate(node.Message, 80)),
				))
			}
		}
		b.WriteString("\n")
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*Report generated by argo-analyzer on %s*\n",
		report.GeneratedAt.Format(time.RFC1123)))

	return b.String()
}

// ── Confluence REST API publish ───────────────────────────────────────────────

// Publish creates or updates a Confluence page with the report content.
// If a page with cfg.Title already exists in cfg.SpaceKey, it is updated
// (version incremented). Otherwise a new page is created.
func Publish(report *models.Report, cfg PublishConfig) error {
	cfg.FromEnv()
	if err := cfg.Validate(); err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	body := renderStorageFormat(report)

	// Check whether the page already exists
	existingID, existingVersion, err := findPage(client, cfg)
	if err != nil {
		return fmt.Errorf("looking up existing page: %w", err)
	}

	if existingID == "" {
		return createPage(client, cfg, body)
	}
	return updatePage(client, cfg, existingID, existingVersion+1, body)
}

// ── Confluence Storage Format renderer ───────────────────────────────────────

// renderStorageFormat produces Confluence Storage Format (XHTML subset).
// This is what the Confluence REST API accepts and renders natively —
// tables, section macros, info panels, and code blocks all work correctly.
func renderStorageFormat(report *models.Report) string {
	var b strings.Builder
	m := report.Metrics

	// Helper: write a Confluence info/note/warning panel
	panel := func(panelType, title, body string) {
		b.WriteString(fmt.Sprintf(
			`<ac:structured-macro ac:name="%s"><ac:parameter ac:name="title">%s</ac:parameter>`+
				`<ac:rich-text-body><p>%s</p></ac:rich-text-body></ac:structured-macro>`,
			panelType, xmlEsc(title), body))
		b.WriteString("\n")
	}

	// ── Header info panel ─────────────────────────────────────────────────────
	panel("info", "Report Details",
		fmt.Sprintf("Generated: <strong>%s</strong> &nbsp;|&nbsp; Query: <strong>%s = %s</strong>",
			xmlEsc(report.GeneratedAt.Format(time.RFC1123)),
			xmlEsc(report.QueryType),
			xmlEsc(report.QueryValue),
		))

	// ── Summary table ─────────────────────────────────────────────────────────
	b.WriteString("<h2>Summary</h2>\n")
	b.WriteString(sfTable(
		[]string{"Metric", "Count", "%"},
		[][]string{
			{"Total workflows", itoa(m.TotalWorkflows), "—"},
			{"✅ Successful", itoa(m.SuccessfulCount), pctStr(m.SuccessPercentage)},
			{"❌ Failed", itoa(m.FailedCount), pctStr(m.FailurePercentage)},
		},
	))

	// ── Category breakdown ────────────────────────────────────────────────────
	if m.FailedCount > 0 {
		b.WriteString("<h2>Failure Breakdown by Category</h2>\n")
		b.WriteString(sfTable(
			[]string{"Category", "Count", "% of Failures", "Meaning"},
			[][]string{
				{"🔧 Platform", itoa(m.PlatformCount), sfPctOf(m.PlatformCount, m.FailedCount), "Infrastructure, scheduler, Argo controller"},
				{"💻 Application", itoa(m.ApplicationCount), sfPctOf(m.ApplicationCount, m.FailedCount), "User workload code or configuration"},
				{"🛠 DevEx gap", itoa(m.DevExCount), sfPctOf(m.DevExCount, m.FailedCount), "Missing guardrails, retries, validation, docs"},
				{"❓ Unknown", itoa(m.UnknownCount), sfPctOf(m.UnknownCount, m.FailedCount), "Could not be classified"},
			},
		))
	}

	// ── Duration metrics ──────────────────────────────────────────────────────
	if m.AllWorkflowDuration.Count > 0 {
		b.WriteString("<h2>Duration Metrics</h2>\n")
		rows := [][]string{}
		addDur := func(label string, s models.DurationStats) {
			if s.Count > 0 {
				rows = append(rows, []string{
					label,
					itoa(s.Count),
					sfDuration(s.Min),
					sfDuration(s.Max),
					sfDuration(s.Mean),
					sfDuration(s.Median),
				})
			}
		}
		addDur("All (terminal)", m.AllWorkflowDuration)
		addDur("Successful", m.SuccessfulDuration)
		addDur("Failed", m.FailedDuration)
		b.WriteString(sfTable([]string{"Scope", "Count", "Min", "Max", "Mean", "Median"}, rows))
	}

	// ── Slowest workflows ─────────────────────────────────────────────────────
	if len(m.SlowestWorkflows) > 0 {
		b.WriteString("<h2>Slowest Workflows (Top 10)</h2>\n")
		rows := make([][]string, len(m.SlowestWorkflows))
		for i, e := range m.SlowestWorkflows {
			rows[i] = []string{itoa(i + 1), sfCode(e.Name), sfDash(e.TemplateName), e.Phase, sfDuration(e.Duration)}
		}
		b.WriteString(sfTable([]string{"#", "Workflow", "WF Template", "Phase", "Duration"}, rows))
	}

	// ── Slowest failed template steps ─────────────────────────────────────────
	if len(m.SlowestTemplates) > 0 {
		b.WriteString("<h2>Slowest Failed Template Steps (Top 10)</h2>\n")
		rows := make([][]string, len(m.SlowestTemplates))
		for i, e := range m.SlowestTemplates {
			rows[i] = []string{itoa(i + 1), sfCode(e.Name), sfDash(e.TemplateName), sfCode(e.WorkflowName), sfDuration(e.Duration)}
		}
		b.WriteString(sfTable([]string{"#", "Node", "Template", "Workflow", "Duration"}, rows))
	}

	// ── Top failing templates ─────────────────────────────────────────────────
	if len(m.TopFailingTemplates) > 0 {
		b.WriteString("<h2>Top Failing Templates</h2>\n")
		rows := make([][]string, len(m.TopFailingTemplates))
		for i, tf := range m.TopFailingTemplates {
			rows[i] = []string{itoa(i + 1), sfCode(tf.TemplateName), itoa(tf.Count), string(tf.DominantCategory)}
		}
		b.WriteString(sfTable([]string{"#", "Template", "Failures", "Dominant Category"}, rows))
	}

	// ── Recurring patterns ────────────────────────────────────────────────────
	if len(report.Patterns) > 0 {
		b.WriteString("<h2>Recurring Failure Patterns</h2>\n")
		rows := make([][]string, len(report.Patterns))
		for i, p := range report.Patterns {
			flaky := ""
			if p.IsFlaky {
				flaky = "✓"
			}
			rows[i] = []string{
				itoa(i + 1),
				string(p.Category),
				sfCode(string(p.Subtype)),
				sfDash(p.TemplateName),
				itoa(p.OccurrenceCount),
				itoa(len(p.AffectedWorkflows)),
				flaky,
				xmlEsc(truncate(p.RepresentativeMessage, 80)),
			}
		}
		b.WriteString(sfTable(
			[]string{"#", "Category", "Subtype", "Template", "Occurrences", "Workflows", "Flaky", "Representative Message"},
			rows,
		))
	}

	// ── DevEx insights ────────────────────────────────────────────────────────
	if len(report.Insights) > 0 {
		b.WriteString("<h2>DevEx Insights &amp; Recommendations</h2>\n")
		for i, ins := range report.Insights {
			panelType := "note"
			if ins.Priority == models.PriorityHigh {
				panelType = "warning"
			} else if ins.Priority == models.PriorityLow {
				panelType = "info"
			}

			var body strings.Builder
			body.WriteString(fmt.Sprintf("<strong>Priority:</strong> %s<br/>", strings.ToUpper(string(ins.Priority))))
			if len(ins.AffectedTemplates) > 0 {
				escaped := make([]string, len(ins.AffectedTemplates))
				for j, t := range ins.AffectedTemplates {
					escaped[j] = "<code>" + xmlEsc(t) + "</code>"
				}
				body.WriteString(fmt.Sprintf("<strong>Affected templates:</strong> %s<br/>", strings.Join(escaped, ", ")))
			}
			body.WriteString(fmt.Sprintf("<strong>Supporting data:</strong> %s<br/><br/>", xmlEsc(ins.SupportingData)))
			body.WriteString(xmlEsc(ins.Description) + "<br/><br/>")
			body.WriteString(fmt.Sprintf("<strong>Recommendation:</strong> %s", xmlEsc(ins.Recommendation)))

			panel(panelType, fmt.Sprintf("%d. %s", i+1, ins.Title), body.String())
		}
	}

	// ── Failed workflows summary ──────────────────────────────────────────────
	if len(report.FailedWorkflows) > 0 {
		b.WriteString("<h2>Failed Workflows</h2>\n")
		rows := make([][]string, len(report.FailedWorkflows))
		for i, wf := range report.FailedWorkflows {
			tmpl := ""
			if len(wf.FailedNodes) > 0 {
				tmpl = wf.FailedNodes[0].WorkflowTemplate
			}
			rows[i] = []string{
				sfCode(wf.Name),
				sfDash(tmpl),
				wf.Namespace,
				sfDuration(wf.Duration),
				itoa(len(wf.FailedNodes)),
				sfCategoryBadges(wf.FailedNodes),
			}
		}
		b.WriteString(sfTable(
			[]string{"Workflow", "WF Template", "Namespace", "Duration", "Failed Nodes", "Categories"},
			rows,
		))

		// ── Failed node details ───────────────────────────────────────────────
		b.WriteString("<h2>Failed Node Details</h2>\n")
		var nodeRows [][]string
		for _, wf := range report.FailedWorkflows {
			for _, node := range wf.FailedNodes {
				c := node.Classification
				path := strings.Join(node.NodePath, " → ")
				nodeRows = append(nodeRows, []string{
					sfDash(node.WorkflowTemplate),
					sfCode(node.NodeName),
					sfDash(node.TemplateName),
					xmlEsc(truncate(path, 60)),
					string(c.Category),
					sfCode(string(c.Subtype)),
					string(c.Confidence),
					sfDash(node.ExitCode),
					xmlEsc(truncate(node.Message, 100)),
				})
			}
		}
		b.WriteString(sfTable(
			[]string{"WF Template", "Node", "Template", "Path", "Category", "Subtype", "Confidence", "Exit", "Failure Reason"},
			nodeRows,
		))
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString(fmt.Sprintf(
		`<p><em>Report generated by argo-analyzer on %s</em></p>`,
		xmlEsc(report.GeneratedAt.Format(time.RFC1123)),
	))

	return b.String()
}

// ── Confluence REST API helpers ───────────────────────────────────────────────

type cfPage struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Version cfVersion `json:"version"`
}

type cfVersion struct {
	Number int `json:"number"`
}

type cfPageList struct {
	Results []cfPage `json:"results"`
}

type cfCreateBody struct {
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Space     cfSpace `json:"space"`
	Ancestors []cfAnc `json:"ancestors,omitempty"`
	Body      cfBody  `json:"body"`
}

type cfUpdateBody struct {
	Version cfVersion `json:"version"`
	Title   string    `json:"title"`
	Type    string    `json:"type"`
	Body    cfBody    `json:"body"`
}

type cfSpace struct {
	Key string `json:"key"`
}
type cfAnc struct {
	ID string `json:"id"`
}
type cfBody struct {
	Storage cfStorage `json:"storage"`
}
type cfStorage struct {
	Value          string `json:"value"`
	Representation string `json:"representation"`
}

// findPage searches for an existing page by title in the configured space.
// Returns ("", 0, nil) if not found.
func findPage(client *http.Client, cfg PublishConfig) (id string, version int, err error) {
	u := fmt.Sprintf("%s/wiki/rest/api/content?title=%s&spaceKey=%s&expand=version",
		strings.TrimRight(cfg.BaseURL, "/"),
		urlEncode(cfg.Title),
		urlEncode(cfg.SpaceKey),
	)
	body, err := cfGET(client, cfg, u)
	if err != nil {
		return "", 0, err
	}
	var list cfPageList
	if err := json.Unmarshal(body, &list); err != nil {
		return "", 0, fmt.Errorf("parsing page list: %w", err)
	}
	if len(list.Results) == 0 {
		return "", 0, nil
	}
	p := list.Results[0]
	return p.ID, p.Version.Number, nil
}

func createPage(client *http.Client, cfg PublishConfig, storageBody string) error {
	payload := cfCreateBody{
		Type:  "page",
		Title: cfg.Title,
		Space: cfSpace{Key: cfg.SpaceKey},
		Body:  cfBody{Storage: cfStorage{Value: storageBody, Representation: "storage"}},
	}
	if cfg.ParentID != "" {
		payload.Ancestors = []cfAnc{{ID: cfg.ParentID}}
	}
	data, _ := json.Marshal(payload)
	u := fmt.Sprintf("%s/wiki/rest/api/content", strings.TrimRight(cfg.BaseURL, "/"))
	return cfPOST(client, cfg, u, data)
}

func updatePage(client *http.Client, cfg PublishConfig, pageID string, newVersion int, storageBody string) error {
	payload := cfUpdateBody{
		Version: cfVersion{Number: newVersion},
		Title:   cfg.Title,
		Type:    "page",
		Body:    cfBody{Storage: cfStorage{Value: storageBody, Representation: "storage"}},
	}
	data, _ := json.Marshal(payload)
	u := fmt.Sprintf("%s/wiki/rest/api/content/%s", strings.TrimRight(cfg.BaseURL, "/"), pageID)
	return cfPUT(client, cfg, u, data)
}

func cfGET(client *http.Client, cfg PublishConfig, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(cfg.User, cfg.Token)
	req.Header.Set("Accept", "application/json")
	return doRequest(client, req)
}

func cfPOST(client *http.Client, cfg PublishConfig, url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.User, cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	_, err = doRequest(client, req)
	return err
}

func cfPUT(client *http.Client, cfg PublishConfig, url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.User, cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	_, err = doRequest(client, req)
	return err
}

func doRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Confluence API %s %s → HTTP %d: %s",
			req.Method, req.URL, resp.StatusCode, truncate(string(body), 300))
	}
	return body, nil
}

// ── Storage format rendering helpers ─────────────────────────────────────────

// sfTable renders a Confluence storage-format table.
func sfTable(headers []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("<table><tbody>\n")
	b.WriteString("<tr>")
	for _, h := range headers {
		b.WriteString(fmt.Sprintf("<th><strong>%s</strong></th>", xmlEsc(h)))
	}
	b.WriteString("</tr>\n")
	for _, row := range rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString(fmt.Sprintf("<td>%s</td>", cell)) // cells already escaped by callers
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table>\n")
	return b.String()
}

func sfCode(s string) string {
	if s == "" {
		return "—"
	}
	return "<code>" + xmlEsc(s) + "</code>"
}

func sfDash(s string) string {
	if s == "" {
		return "—"
	}
	return xmlEsc(s)
}

func sfDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	return formatDuration(d)
}

func sfPctOf(n, total int) string {
	if total == 0 || n == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

func sfCategoryBadges(nodes []models.FailedNode) string {
	counts := map[models.FailureCategory]int{}
	for _, n := range nodes {
		counts[n.Classification.Category]++
	}
	var parts []string
	for _, cat := range []models.FailureCategory{
		models.CategoryPlatform, models.CategoryApplication,
		models.CategoryDevEx, models.CategoryUnknown,
	} {
		if n := counts[cat]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s×%d", catAbbrev(cat), n))
		}
	}
	return strings.Join(parts, " ")
}

// ── Markdown rendering helpers ────────────────────────────────────────────────

func writeMDDurationRow(b *strings.Builder, label string, s models.DurationStats) {
	if s.Count == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("| %s | %d | %s | %s | %s | %s |\n",
		label, s.Count,
		mdDuration(s.Min), mdDuration(s.Max),
		mdDuration(s.Mean), mdDuration(s.Median),
	))
}

func mdDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	return formatDuration(d)
}

func mdDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func mdCode(s string) string {
	if s == "" {
		return "—"
	}
	return "`" + s + "`"
}

func mdPctOf(n, total int) string {
	if total == 0 || n == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

func mdPriorityIcon(p models.InsightPriority) string {
	switch p {
	case models.PriorityHigh:
		return "🔴"
	case models.PriorityMedium:
		return "🟡"
	default:
		return "🟢"
	}
}

func mdEscape(s string) string {
	// Escape pipe characters inside table cells
	return strings.ReplaceAll(s, "|", "\\|")
}

func mdCategoryBadges(nodes []models.FailedNode) string {
	counts := map[models.FailureCategory]int{}
	for _, n := range nodes {
		counts[n.Classification.Category]++
	}
	var parts []string
	for _, cat := range []models.FailureCategory{
		models.CategoryPlatform, models.CategoryApplication,
		models.CategoryDevEx, models.CategoryUnknown,
	} {
		if n := counts[cat]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s×%d", catAbbrev(cat), n))
		}
	}
	return strings.Join(parts, " ")
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func xmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func urlEncode(s string) string {
	return strings.ReplaceAll(s, " ", "+")
}

func formatDuration(d time.Duration) string {
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

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func pctStr(f float64) string {
	if f == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", f)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
