// Package analyzer orchestrates the full analysis pipeline:
//  1. Extract failed leaf nodes from each workflow's DAG
//  2. Classify each failed node (via the classifier package)
//  3. Compute enriched Metrics (category breakdown, duration stats, slowest lists)
//  4. Detect cross-workflow patterns  (via the patterns package)
//  5. Generate DevEx insights         (via the insights package)
package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"argo-analyzer/internal/classifier"
	"argo-analyzer/internal/insights"
	"argo-analyzer/internal/models"
	"argo-analyzer/internal/patterns"
)

const (
	phaseFailed    = "Failed"
	phaseError     = "Error"
	phaseSucceeded = "Succeeded"

	labelWorkflowTemplate = "workflows.argoproj.io/workflow-template"
)

// Analyze runs the full pipeline on a slice of raw Argo Workflows.
func Analyze(workflows []models.Workflow) *models.Report {
	report := &models.Report{GeneratedAt: time.Now()}

	var analyzedFailed []models.AnalyzedWorkflow
	successCount := 0

	for _, wf := range workflows {
		analyzed := analyzeWorkflow(wf)
		if isFailedPhase(wf.Status.Phase) {
			analyzedFailed = append(analyzedFailed, analyzed)
		} else if wf.Status.Phase == phaseSucceeded {
			successCount++
		}
	}

	report.Metrics = computeMetrics(workflows, analyzedFailed, successCount)
	report.FailedWorkflows = analyzedFailed
	report.Patterns = patterns.Detect(analyzedFailed)
	report.Insights = insights.Generate(report.Patterns, report.Metrics)

	return report
}

// ── Workflow analysis ─────────────────────────────────────────────────────────

func analyzeWorkflow(wf models.Workflow) models.AnalyzedWorkflow {
	result := models.AnalyzedWorkflow{
		Name:      wf.Metadata.Name,
		Namespace: wf.Metadata.Namespace,
		Phase:     wf.Status.Phase,
		Message:   wf.Status.Message,
	}

	result.StartedAt, _ = parseTime(wf.Status.StartedAt)
	result.FinishedAt, _ = parseTime(wf.Status.FinishedAt)
	if !result.StartedAt.IsZero() && !result.FinishedAt.IsZero() {
		result.Duration = result.FinishedAt.Sub(result.StartedAt)
	}

	wfTemplate := wf.Metadata.Labels[labelWorkflowTemplate]
	nodes := wf.Status.Nodes
	if len(nodes) == 0 {
		// nodes absent — synthesise a single entry from workflow-level message
		if isFailedPhase(wf.Status.Phase) {
			c := classifier.Classify(wf.Status.Message, "", wf.Spec.Entrypoint)
			result.FailedNodes = append(result.FailedNodes, models.FailedNode{
				WorkflowName:     wf.Metadata.Name,
				WorkflowTemplate: wfTemplate,
				Namespace:        wf.Metadata.Namespace,
				NodeID:           "(workflow)",
				NodeName:         wf.Metadata.Name,
				NodePath:         []string{wf.Metadata.Name},
				TemplateName:     wf.Spec.Entrypoint,
				Phase:            wf.Status.Phase,
				Message:          wf.Status.Message,
				StartedAt:        result.StartedAt,
				FinishedAt:       result.FinishedAt,
				Duration:         result.Duration,
				Classification:   c,
			})
		}
		return result
	}

	parentOf := buildParentMap(nodes)

	for nodeID, node := range nodes {
		if !isFailedPhase(node.Phase) {
			continue
		}
		if isOrchestratorType(node.Type) {
			continue
		}
		if hasFailedChild(nodeID, nodes) {
			continue
		}

		exitCode := ""
		if node.Outputs != nil {
			exitCode = node.Outputs.ExitCode
		}

		displayName := node.Name
		if displayName == "" {
			displayName = node.FullName
		}

		fn := models.FailedNode{
			WorkflowName:     wf.Metadata.Name,
			WorkflowTemplate: wfTemplate,
			Namespace:        wf.Metadata.Namespace,
			NodeID:           nodeID,
			NodeName:         displayName,
			NodePath:         buildPath(nodeID, nodes, parentOf),
			TemplateName:     node.TemplateName,
			Phase:            node.Phase,
			Message:          node.Message,
			ExitCode:         exitCode,
			Classification:   classifier.Classify(node.Message, exitCode, node.TemplateName),
		}

		fn.StartedAt, _ = parseTime(node.StartedAt)
		fn.FinishedAt, _ = parseTime(node.FinishedAt)
		if !fn.StartedAt.IsZero() && !fn.FinishedAt.IsZero() {
			fn.Duration = fn.FinishedAt.Sub(fn.StartedAt)
		}

		result.FailedNodes = append(result.FailedNodes, fn)
	}

	// Fallback: nodes present but none qualified as a leaf — surface workflow message.
	if len(result.FailedNodes) == 0 && isFailedPhase(wf.Status.Phase) {
		c := classifier.Classify(wf.Status.Message, "", wf.Spec.Entrypoint)
		result.FailedNodes = append(result.FailedNodes, models.FailedNode{
			WorkflowName:     wf.Metadata.Name,
			WorkflowTemplate: wfTemplate,
			Namespace:        wf.Metadata.Namespace,
			NodeID:           "(workflow)",
			NodeName:         wf.Metadata.Name,
			NodePath:         []string{wf.Metadata.Name},
			TemplateName:     wf.Spec.Entrypoint,
			Phase:            wf.Status.Phase,
			Message:          wf.Status.Message,
			StartedAt:        result.StartedAt,
			FinishedAt:       result.FinishedAt,
			Duration:         result.Duration,
			Classification:   c,
		})
	}

	return result
}

// ── Graph helpers ─────────────────────────────────────────────────────────────

// buildParentMap inverts each node's children[] array into childID → parentID.
// This is the authoritative graph structure — do NOT use string prefix matching.
func buildParentMap(nodes map[string]models.NodeStatus) map[string]string {
	parentOf := make(map[string]string, len(nodes))
	for id, node := range nodes {
		for _, childID := range node.Children {
			parentOf[childID] = id
		}
	}
	return parentOf
}

// hasFailedChild returns true if any direct child of nodeID is also Failed/Error.
// If so, the parent's failure is propagation — not the root cause.
func hasFailedChild(nodeID string, nodes map[string]models.NodeStatus) bool {
	for _, childID := range nodes[nodeID].Children {
		if child, ok := nodes[childID]; ok && isFailedPhase(child.Phase) {
			return true
		}
	}
	return false
}

// buildPath walks parentOf upward from nodeID to the root and returns
// displayNames in root-first order.
func buildPath(nodeID string, nodes map[string]models.NodeStatus, parentOf map[string]string) []string {
	const maxDepth = 50
	var ids []string
	cur := nodeID
	seen := make(map[string]struct{})
	for i := 0; i < maxDepth; i++ {
		if _, already := seen[cur]; already {
			break
		}
		seen[cur] = struct{}{}
		ids = append(ids, cur)
		parent, ok := parentOf[cur]
		if !ok {
			break
		}
		cur = parent
	}
	// Reverse to get root → leaf order
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	path := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, ok := nodes[id]; ok {
			name := n.Name
			if name == "" {
				name = n.FullName
			}
			if name == "" {
				name = id
			}
			path = append(path, name)
		}
	}
	return path
}

func isOrchestratorType(t string) bool {
	return t == "DAG" || t == "Steps" || t == "StepGroup"
}

// ── Metrics ───────────────────────────────────────────────────────────────────

func computeMetrics(
	raw []models.Workflow,
	failed []models.AnalyzedWorkflow,
	successCount int,
) models.Metrics {

	m := models.Metrics{
		TotalWorkflows:  len(raw),
		SuccessfulCount: successCount,
		FailedCount:     len(failed),
	}

	terminal := m.SuccessfulCount + m.FailedCount
	if terminal > 0 {
		m.SuccessPercentage = float64(m.SuccessfulCount) / float64(terminal) * 100
		m.FailurePercentage = float64(m.FailedCount) / float64(terminal) * 100
	}

	// ── Duration collection (all terminal workflows) ───────────────────────────
	var allDurations, failedDurations, successDurations []time.Duration
	var slowestWF []models.SlowestEntry

	for _, wf := range raw {
		start, err1 := parseTime(wf.Status.StartedAt)
		end, err2 := parseTime(wf.Status.FinishedAt)
		if err1 != nil || err2 != nil || start.IsZero() || end.IsZero() {
			continue
		}
		d := end.Sub(start)
		if d < 0 {
			continue
		}

		allDurations = append(allDurations, d)
		slowestWF = append(slowestWF, models.SlowestEntry{
			Name:         wf.Metadata.Name,
			TemplateName: wf.Metadata.Labels[labelWorkflowTemplate],
			Phase:        wf.Status.Phase,
			Duration:     d,
		})

		switch {
		case isFailedPhase(wf.Status.Phase):
			failedDurations = append(failedDurations, d)
		case wf.Status.Phase == phaseSucceeded:
			successDurations = append(successDurations, d)
		}
	}

	m.AllWorkflowDuration = durationStats(allDurations)
	m.FailedDuration = durationStats(failedDurations)
	m.SuccessfulDuration = durationStats(successDurations)

	// Top-10 slowest individual workflow runs
	sort.Slice(slowestWF, func(i, j int) bool {
		return slowestWF[i].Duration > slowestWF[j].Duration
	})
	if len(slowestWF) > 10 {
		m.SlowestWorkflows = slowestWF[:10]
	} else {
		m.SlowestWorkflows = slowestWF
	}

	// ── Category breakdown + template stats (failed nodes) ────────────────────
	templateCats := map[string]map[models.FailureCategory]int{}
	var slowestNodes []models.SlowestEntry

	for _, wf := range failed {
		for _, node := range wf.FailedNodes {
			switch node.Classification.Category {
			case models.CategoryPlatform:
				m.PlatformCount++
			case models.CategoryApplication:
				m.ApplicationCount++
			case models.CategoryDevEx:
				m.DevExCount++
			default:
				m.UnknownCount++
			}

			tmpl := node.TemplateName
			if tmpl == "" {
				tmpl = "(unknown)"
			}
			if templateCats[tmpl] == nil {
				templateCats[tmpl] = map[models.FailureCategory]int{}
			}
			templateCats[tmpl][node.Classification.Category]++

			if node.Duration > 0 {
				slowestNodes = append(slowestNodes, models.SlowestEntry{
					Name:         node.NodeName,
					WorkflowName: wf.Name,
					TemplateName: node.TemplateName,
					Phase:        node.Phase,
					Duration:     node.Duration,
				})
			}
		}
	}

	// Top-10 slowest failed template nodes
	sort.Slice(slowestNodes, func(i, j int) bool {
		return slowestNodes[i].Duration > slowestNodes[j].Duration
	})
	if len(slowestNodes) > 10 {
		m.SlowestTemplates = slowestNodes[:10]
	} else {
		m.SlowestTemplates = slowestNodes
	}

	// Top-10 failing templates by failure count
	type tEntry struct {
		name  string
		count int
		dom   models.FailureCategory
	}
	var tList []tEntry
	for tmpl, cats := range templateCats {
		total, domCount := 0, 0
		dom := models.CategoryUnknown
		for cat, n := range cats {
			total += n
			if n > domCount {
				domCount = n
				dom = cat
			}
		}
		tList = append(tList, tEntry{tmpl, total, dom})
	}
	sort.Slice(tList, func(i, j int) bool {
		if tList[i].count != tList[j].count {
			return tList[i].count > tList[j].count
		}
		return tList[i].name < tList[j].name
	})
	if len(tList) > 10 {
		tList = tList[:10]
	}
	for _, e := range tList {
		m.TopFailingTemplates = append(m.TopFailingTemplates, models.TemplateFailureCount{
			TemplateName:     e.name,
			Count:            e.count,
			DominantCategory: e.dom,
		})
	}

	return m
}

// durationStats computes min/max/mean/median over a slice of durations.
// Returns zero-value DurationStats if the slice is empty.
func durationStats(ds []time.Duration) models.DurationStats {
	if len(ds) == 0 {
		return models.DurationStats{}
	}
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}

	n := len(sorted)
	var median time.Duration
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	} else {
		median = sorted[n/2]
	}

	return models.DurationStats{
		Min:    sorted[0],
		Max:    sorted[n-1],
		Mean:   total / time.Duration(n),
		Median: median,
		Count:  n,
	}
}

// ── Misc helpers ──────────────────────────────────────────────────────────────

func isFailedPhase(phase string) bool {
	return phase == phaseFailed || phase == phaseError
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	for _, f := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable: %s", s)
}

// NodePathString returns the path as a human-readable "a → b → c" string.
func NodePathString(path []string) string {
	return strings.Join(path, " → ")
}
