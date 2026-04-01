package analyzer

import (
	"fmt"
	"time"

	"argo-analyzer/internal/models"
)

const (
	PhaseFailed    = "Failed"
	PhaseError     = "Error"
	PhaseSucceeded = "Succeeded"
	PhaseRunning   = "Running"
	PhasePending   = "Pending"
	PhaseSkipped   = "Skipped"
	PhaseOmitted   = "Omitted"
)

// Analyze processes a slice of workflows and returns the full report
func Analyze(workflows []models.Workflow) *models.Report {
	report := &models.Report{
		GeneratedAt:    time.Now(),
		TotalWorkflows: len(workflows),
	}

	for _, wf := range workflows {
		analyzed := analyzeWorkflow(wf)
		if isFailedPhase(wf.Status.Phase) {
			report.FailedCount++
			report.FailedWorkflows = append(report.FailedWorkflows, analyzed)
		} else if wf.Status.Phase == PhaseSucceeded {
			report.SuccessfulCount++
		}
	}

	// Account for workflows that may not be succeeded/failed (running, pending, etc.)
	// by computing percentages only over terminal states
	terminal := report.SuccessfulCount + report.FailedCount
	if terminal > 0 {
		report.SuccessPercentage = float64(report.SuccessfulCount) / float64(terminal) * 100
		report.FailurePercentage = float64(report.FailedCount) / float64(terminal) * 100
	}

	return report
}

// analyzeWorkflow extracts all failed leaf nodes from a workflow
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

	// Build a set of parent node IDs so we can identify leaf nodes.
	// In Argo's node graph, children reference their parent; we need to find
	// nodes that are NOT referenced as a parent by anyone else.
	childIDs := buildChildSet(wf.Status.Nodes)

	for nodeID, node := range wf.Status.Nodes {
		if !isFailedPhase(node.Phase) {
			continue
		}
		// A leaf is a node that has no children (i.e., no other node lists it as a child)
		if _, hasChildren := childIDs[nodeID]; hasChildren {
			continue
		}
		// Skip DAG/Steps orchestrator nodes — we only want actual task/Pod nodes
		if node.Type == "DAG" || node.Type == "Steps" || node.Type == "StepGroup" {
			continue
		}

		fn := models.FailedNode{
			WorkflowName: wf.Metadata.Name,
			Namespace:    wf.Metadata.Namespace,
			NodeID:       nodeID,
			NodeName:     node.Name,
			TemplateName: node.TemplateName,
			Phase:        node.Phase,
			Message:      node.Message,
		}

		fn.StartedAt, _ = parseTime(node.StartedAt)
		fn.FinishedAt, _ = parseTime(node.FinishedAt)
		if !fn.StartedAt.IsZero() && !fn.FinishedAt.IsZero() {
			fn.Duration = fn.FinishedAt.Sub(fn.StartedAt)
		}

		if node.Outputs != nil {
			fn.ExitCode = node.Outputs.ExitCode
		}

		// If node name is empty fall back to full name
		if fn.NodeName == "" {
			fn.NodeName = node.FullName
		}

		result.FailedNodes = append(result.FailedNodes, fn)
	}

	// If no leaf failures were found but the workflow is failed, surface the
	// workflow-level message as a synthetic failed node so it still appears in output
	if len(result.FailedNodes) == 0 && isFailedPhase(wf.Status.Phase) {
		result.FailedNodes = append(result.FailedNodes, models.FailedNode{
			WorkflowName: wf.Metadata.Name,
			Namespace:    wf.Metadata.Namespace,
			NodeID:       "(workflow)",
			NodeName:     wf.Metadata.Name,
			TemplateName: wf.Spec.Entrypoint,
			Phase:        wf.Status.Phase,
			Message:      wf.Status.Message,
			StartedAt:    result.StartedAt,
			FinishedAt:   result.FinishedAt,
			Duration:     result.Duration,
		})
	}

	return result
}

// buildChildSet returns a set of node IDs that are referenced as children.
// Argo nodes store children references via their "children" field; however the
// archived API flattens the graph — so we derive parenthood by looking at which
// nodes are referenced in the "boundaryID" / by checking the prefix convention.
// The most reliable approach with the archived API is to treat a node as a
// non-leaf if its ID appears as the prefix (parent) of another node's ID.
func buildChildSet(nodes map[string]models.NodeStatus) map[string]struct{} {
	parents := make(map[string]struct{})
	for _, node := range nodes {
		// Argo node IDs follow the pattern: workflowName[.stepName...], where
		// parent IDs are prefixes of child IDs separated by a bracket or dot.
		// We flag any node that is a proper prefix of another node as a parent.
		_ = node
	}
	// Re-iterate to actually build the parent set
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	for i, id := range ids {
		for j, other := range ids {
			if i == j {
				continue
			}
			// If `id` is a prefix of `other` (and other is longer), id is a parent
			if len(id) < len(other) && other[:len(id)] == id {
				parents[id] = struct{}{}
			}
		}
	}
	return parents
}

func isFailedPhase(phase string) bool {
	return phase == PhaseFailed || phase == PhaseError
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	formats := []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05Z"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable: %s", s)
}
