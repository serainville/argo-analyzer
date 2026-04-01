// Package patterns detects recurring failure signatures across a set of
// AnalyzedWorkflows and returns them as a ranked slice of FailurePattern.
//
// Grouping key
// ────────────
// Failures are grouped by the tuple (TemplateName, Subtype, normalised-message).
// Message normalisation strips run-specific tokens (UUIDs, pod names, IPs,
// timestamps, line numbers) so that the same logical failure from different
// workflow runs lands in the same bucket.
package patterns

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"argo-analyzer/internal/models"
)

// Detect analyses all failed nodes across workflows and returns recurring
// patterns sorted by occurrence count descending.
func Detect(workflows []models.AnalyzedWorkflow) []models.FailurePattern {
	type bucket struct {
		pattern   models.FailurePattern
		msgCounts map[string]int // raw message → count, for picking representative
		wfSet     map[string]struct{}
		nsSet     map[string]struct{}
		codeSet   map[string]struct{}
	}

	buckets := map[string]*bucket{}

	for _, wf := range workflows {
		for _, node := range wf.FailedNodes {
			key := makeKey(node)

			b, ok := buckets[key]
			if !ok {
				b = &bucket{
					pattern: models.FailurePattern{
						PatternKey:   key,
						Category:     node.Classification.Category,
						Subtype:      node.Classification.Subtype,
						TemplateName: node.TemplateName,
						FirstSeen:    node.StartedAt,
						LastSeen:     node.StartedAt,
					},
					msgCounts: map[string]int{},
					wfSet:     map[string]struct{}{},
					nsSet:     map[string]struct{}{},
					codeSet:   map[string]struct{}{},
				}
				buckets[key] = b
			}

			b.pattern.OccurrenceCount++
			b.wfSet[wf.Name] = struct{}{}
			b.nsSet[wf.Namespace] = struct{}{}
			if node.ExitCode != "" {
				b.codeSet[node.ExitCode] = struct{}{}
			}
			b.msgCounts[node.Message]++

			if !node.StartedAt.IsZero() {
				if b.pattern.FirstSeen.IsZero() || node.StartedAt.Before(b.pattern.FirstSeen) {
					b.pattern.FirstSeen = node.StartedAt
				}
				if node.StartedAt.After(b.pattern.LastSeen) {
					b.pattern.LastSeen = node.StartedAt
				}
			}
		}
	}

	// Detect flakiness: a template that sometimes fails AND sometimes succeeds
	// within the same workflow run (step retried and recovered).
	flakyTemplates := detectFlakyTemplates(workflows)

	// Materialise buckets → patterns
	result := make([]models.FailurePattern, 0, len(buckets))
	for _, b := range buckets {
		p := b.pattern

		// Sorted, deduplicated workflow / namespace lists
		p.AffectedWorkflows = sortedKeys(b.wfSet)
		p.AffectedNamespaces = sortedKeys(b.nsSet)
		p.TypicalExitCodes = sortedKeys(b.codeSet)
		p.RepresentativeMessage = mostCommon(b.msgCounts)

		if _, flaky := flakyTemplates[p.TemplateName]; flaky {
			p.IsFlaky = true
		}

		result = append(result, p)
	}

	// Sort: highest occurrence first; ties broken by most recent LastSeen
	sort.Slice(result, func(i, j int) bool {
		if result[i].OccurrenceCount != result[j].OccurrenceCount {
			return result[i].OccurrenceCount > result[j].OccurrenceCount
		}
		return result[i].LastSeen.After(result[j].LastSeen)
	})

	return result
}

// ── Key construction ──────────────────────────────────────────────────────────

// makeKey produces a stable grouping key for a failed node.
// We use (template, subtype, normalised-message) so that identical failures
// from different workflow runs collapse into one pattern.
func makeKey(node models.FailedNode) string {
	norm := normalise(node.Message)
	return fmt.Sprintf("%s|%s|%s", node.TemplateName, string(node.Classification.Subtype), norm)
}

// tokensToClear are patterns that vary between runs and should be removed
// before comparing messages.
var tokensToClear = []*regexp.Regexp{
	// UUIDs / k8s suffixes
	regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
	// Kubernetes pod/resource name suffixes (alphanumeric hash tail)
	regexp.MustCompile(`-[a-z0-9]{5,10}\b`),
	// IPv4 addresses
	regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?\b`),
	// Port numbers standalone
	regexp.MustCompile(`:\d{4,5}\b`),
	// Timestamps inside messages
	regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`),
	// Line/column numbers
	regexp.MustCompile(`\bline \d+\b|\bcol \d+\b|\bcolumn \d+\b`),
	// File paths with run-specific components
	regexp.MustCompile(`/tmp/[^\s]+`),
	// Numbers that look like counts or sizes (but keep exit codes by not stripping single digits)
	regexp.MustCompile(`\b\d{3,}\b`),
}

func normalise(msg string) string {
	s := strings.ToLower(strings.TrimSpace(msg))
	for _, re := range tokensToClear {
		s = re.ReplaceAllString(s, "<X>")
	}
	// Collapse multiple spaces / <X><X> runs
	s = regexp.MustCompile(`(<X>\s*)+`).ReplaceAllString(s, "<X> ")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ── Flakiness detection ───────────────────────────────────────────────────────

// detectFlakyTemplates returns a set of template names that appear as both
// Failed and Succeeded nodes within the same workflow (i.e. a retry succeeded).
func detectFlakyTemplates(workflows []models.AnalyzedWorkflow) map[string]struct{} {
	flaky := map[string]struct{}{}

	for _, wf := range workflows {
		failedTemplates := map[string]struct{}{}
		for _, node := range wf.FailedNodes {
			if node.TemplateName != "" {
				failedTemplates[node.TemplateName] = struct{}{}
			}
		}
		// We don't have direct access to succeeded nodes here, but if the
		// overall workflow Succeeded while containing failed nodes (which can
		// happen when retries are configured), those templates are flaky.
		if wf.Phase == "Succeeded" {
			for tmpl := range failedTemplates {
				flaky[tmpl] = struct{}{}
			}
		}
	}

	return flaky
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mostCommon(counts map[string]int) string {
	best, bestN := "", 0
	for msg, n := range counts {
		if n > bestN || (n == bestN && msg < best) {
			best, bestN = msg, n
		}
	}
	return best
}
