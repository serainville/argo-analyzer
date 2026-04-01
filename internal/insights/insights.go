// Package insights derives actionable DevEx recommendations from the pattern
// and metrics data produced earlier in the pipeline.
//
// Each rule inspects the full pattern list and metrics; rules are additive —
// multiple rules can fire and produce separate insights.  Insights are
// deduplicated by (Type, primary affected template) and sorted by priority.
package insights

import (
	"fmt"
	"sort"
	"strings"

	"argo-analyzer/internal/models"
)

// Generate returns a prioritised list of DevExInsights derived from the
// detected patterns and overall metrics.
func Generate(patterns []models.FailurePattern, metrics models.Metrics) []models.DevExInsight {
	var insights []models.DevExInsight

	insights = append(insights, ruleFrequentPlatformOutages(patterns, metrics)...)
	insights = append(insights, ruleMissingRetryPolicies(patterns)...)
	insights = append(insights, ruleFlakySteps(patterns)...)
	insights = append(insights, ruleNoInputValidation(patterns)...)
	insights = append(insights, ruleOOMKills(patterns)...)
	insights = append(insights, ruleTimeoutTooShort(patterns)...)
	insights = append(insights, ruleUnclearErrors(patterns)...)
	insights = append(insights, ruleMissingConfig(patterns)...)
	insights = append(insights, ruleHighFailureRate(metrics)...)
	insights = append(insights, ruleDominantCategory(patterns, metrics)...)

	insights = dedup(insights)
	sortInsights(insights)
	return insights
}

// ── Individual insight rules ──────────────────────────────────────────────────

// ruleFrequentPlatformOutages: if platform failures account for >30% of all
// failures, flag it as a systemic infrastructure concern.
func ruleFrequentPlatformOutages(patterns []models.FailurePattern, metrics models.Metrics) []models.DevExInsight {
	if metrics.FailedCount == 0 {
		return nil
	}
	platPct := float64(metrics.PlatformCount) / float64(metrics.FailedCount) * 100
	if platPct < 30 {
		return nil
	}

	// Collect the dominant platform subtypes
	subtypeCounts := map[models.FailureSubtype]int{}
	for _, p := range patterns {
		if p.Category == models.CategoryPlatform {
			subtypeCounts[p.Subtype] += p.OccurrenceCount
		}
	}
	topSubtypes := topN(subtypeCounts, 3)

	return []models.DevExInsight{{
		Priority: models.PriorityHigh,
		Type:     models.InsightInvestigatePlat,
		Title:    "High rate of platform-level failures",
		Description: fmt.Sprintf(
			"%.0f%% of failed workflows (%d/%d) are caused by platform issues (%s). "+
				"This suggests infrastructure instability rather than application bugs.",
			platPct, metrics.PlatformCount, metrics.FailedCount,
			strings.Join(topSubtypes, ", "),
		),
		Recommendation: "Review cluster resource headroom, node autoscaler settings, and recent infrastructure change history. " +
			"Consider adding a platform SLO dashboard to track these failure modes over time.",
		SupportingData: fmt.Sprintf("%d platform failures, %.0f%% of total failures", metrics.PlatformCount, platPct),
	}}
}

// ruleMissingRetryPolicies: any pattern classified as missing_retry_policy
// that appears in ≥2 workflows is a clear guardrail gap.
func ruleMissingRetryPolicies(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	for _, p := range patterns {
		if p.Subtype != models.SubtypeMissingRetry {
			continue
		}
		if len(p.AffectedWorkflows) < 2 {
			continue
		}
		tmpl := templateLabel(p.TemplateName)
		insights = append(insights, models.DevExInsight{
			Priority: models.PriorityHigh,
			Type:     models.InsightAddRetry,
			Title:    fmt.Sprintf("Add retry policy to %s", tmpl),
			Description: fmt.Sprintf(
				"The template %s is failing on transient errors (%q) across %d workflows with no retry policy. "+
					"These failures would likely self-heal if the step were retried.",
				tmpl, truncate(p.RepresentativeMessage, 80), len(p.AffectedWorkflows),
			),
			Recommendation: "Add a retryStrategy to the template (e.g. retryStrategy.limit: 3, " +
				"retryStrategy.retryPolicy: OnTransientError). Consider also adding exponential back-off.",
			AffectedTemplates: []string{p.TemplateName},
			SupportingData:    fmt.Sprintf("%d occurrences across %d workflows", p.OccurrenceCount, len(p.AffectedWorkflows)),
		})
	}
	return insights
}

// ruleFlakySteps: steps that both fail and succeed across runs need hardening.
func ruleFlakySteps(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	seen := map[string]struct{}{}
	for _, p := range patterns {
		if !p.IsFlaky || p.TemplateName == "" {
			continue
		}
		if _, ok := seen[p.TemplateName]; ok {
			continue
		}
		seen[p.TemplateName] = struct{}{}
		insights = append(insights, models.DevExInsight{
			Priority: models.PriorityMedium,
			Type:     models.InsightReduceFlakiness,
			Title:    fmt.Sprintf("Flaky step detected: %s", templateLabel(p.TemplateName)),
			Description: fmt.Sprintf(
				"Template %s fails inconsistently — it has both failed and succeeded across workflow runs. "+
					"Flaky steps erode developer trust and hide real failures.",
				p.TemplateName,
			),
			Recommendation: "Investigate whether the step has an external dependency that is intermittently unavailable. " +
				"Add retries, improve idempotency, or add a readiness check before the step runs. " +
				"Consider alerting on flakiness rate separately from hard failures.",
			AffectedTemplates: []string{p.TemplateName},
			SupportingData:    fmt.Sprintf("%d failures observed, step also succeeded in some runs", p.OccurrenceCount),
		})
	}
	return insights
}

// ruleNoInputValidation: patterns where bad inputs reached execution.
func ruleNoInputValidation(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	collected := collectTemplates(patterns, models.SubtypeNoInputValidation, 2)
	if len(collected) == 0 {
		return nil
	}
	for tmpl, count := range collected {
		insights = append(insights, models.DevExInsight{
			Priority: models.PriorityHigh,
			Type:     models.InsightAddValidation,
			Title:    fmt.Sprintf("Add input validation to %s", templateLabel(tmpl)),
			Description: fmt.Sprintf(
				"Template %s has failed %d times because invalid or missing parameters reached execution. "+
					"These failures could be caught earlier with parameter validation.",
				templateLabel(tmpl), count,
			),
			Recommendation: "Add parameter validation at the workflow entrypoint using Argo's built-in enum/pattern " +
				"constraints on inputs.parameters, or add an explicit validation step as the first node in the DAG. " +
				"Document required parameter formats clearly in the workflow annotation.",
			AffectedTemplates: []string{tmpl},
			SupportingData:    fmt.Sprintf("%d occurrences", count),
		})
	}
	return insights
}

// ruleOOMKills: OOM kills are almost always a sign that resource limits are
// either unset or set too low.
func ruleOOMKills(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	for _, p := range patterns {
		if p.Subtype != models.SubtypeOOMKill {
			continue
		}
		tmpl := templateLabel(p.TemplateName)
		insights = append(insights, models.DevExInsight{
			Priority: models.PriorityHigh,
			Type:     models.InsightAddResourceLimits,
			Title:    fmt.Sprintf("OOM kills in %s — review memory limits", tmpl),
			Description: fmt.Sprintf(
				"Template %s has been OOM-killed %d times across %d workflows. "+
					"This means either no memory limit is set (so Kubernetes defaults apply) "+
					"or the limit is lower than the workload's actual peak usage.",
				tmpl, p.OccurrenceCount, len(p.AffectedWorkflows),
			),
			Recommendation: "Profile the step's peak memory usage and set resources.limits.memory to 1.5× that value. " +
				"Also set resources.requests.memory to ensure the scheduler places the pod on a node with sufficient headroom. " +
				"If the memory growth is unbounded, investigate for memory leaks in the application code.",
			AffectedTemplates: []string{p.TemplateName},
			SupportingData:    fmt.Sprintf("%d OOM kills across %d workflows", p.OccurrenceCount, len(p.AffectedWorkflows)),
		})
	}
	return insights
}

// ruleTimeoutTooShort: recurring timeout failures on the same template.
func ruleTimeoutTooShort(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	for _, p := range patterns {
		if p.Subtype != models.SubtypeTimeoutTooShort {
			continue
		}
		if p.OccurrenceCount < 2 {
			continue
		}
		tmpl := templateLabel(p.TemplateName)
		insights = append(insights, models.DevExInsight{
			Priority: models.PriorityMedium,
			Type:     models.InsightReviewTimeout,
			Title:    fmt.Sprintf("Timeout configuration needs review for %s", tmpl),
			Description: fmt.Sprintf(
				"Template %s has timed out %d times. "+
					"If the workload legitimately needs more time, the timeout is misconfigured. "+
					"If not, the step may be hanging due to a bug or an unresponsive dependency.",
				tmpl, p.OccurrenceCount,
			),
			Recommendation: "Check the step's activeDeadlineSeconds. If runs consistently exceed it by a small margin, " +
				"increase the limit. If runs hang indefinitely, add a liveness probe or a timeout in the application code itself. " +
				"Consider adding progress heartbeats so operators can tell whether the step is making progress.",
			AffectedTemplates: []string{p.TemplateName},
			SupportingData:    fmt.Sprintf("%d timeout occurrences", p.OccurrenceCount),
		})
	}
	return insights
}

// ruleUnclearErrors: templates that repeatedly emit generic error messages.
func ruleUnclearErrors(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	for _, p := range patterns {
		if p.Subtype != models.SubtypeUnclearError {
			continue
		}
		if p.OccurrenceCount < 3 {
			continue
		}
		tmpl := templateLabel(p.TemplateName)
		insights = append(insights, models.DevExInsight{
			Priority: models.PriorityMedium,
			Type:     models.InsightImproveError,
			Title:    fmt.Sprintf("Improve error messages in %s", tmpl),
			Description: fmt.Sprintf(
				"Template %s has emitted generic, non-actionable error messages %d times (%q). "+
					"When failures are opaque, developers spend more time diagnosing than fixing.",
				tmpl, p.OccurrenceCount, truncate(p.RepresentativeMessage, 60),
			),
			Recommendation: "Update the template to output structured error messages that include: " +
				"(1) what operation was attempted, (2) what went wrong, (3) what the developer should check. " +
				"Consider adding a standardised error schema across all templates in this workflow.",
			AffectedTemplates: []string{p.TemplateName},
			SupportingData:    fmt.Sprintf("%d occurrences of generic errors", p.OccurrenceCount),
		})
	}
	return insights
}

// ruleMissingConfig: repeated missing-secret/configmap failures suggest
// documentation or onboarding gaps.
func ruleMissingConfig(patterns []models.FailurePattern) []models.DevExInsight {
	var insights []models.DevExInsight
	total := 0
	var templates []string
	seen := map[string]struct{}{}
	for _, p := range patterns {
		if p.Subtype != models.SubtypeMissingConfig {
			continue
		}
		total += p.OccurrenceCount
		if p.TemplateName != "" {
			if _, ok := seen[p.TemplateName]; !ok {
				templates = append(templates, p.TemplateName)
				seen[p.TemplateName] = struct{}{}
			}
		}
	}
	if total == 0 {
		return nil
	}
	insights = append(insights, models.DevExInsight{
		Priority: models.PriorityMedium,
		Type:     models.InsightImproveDoc,
		Title:    "Recurring missing-secret/config failures suggest documentation gaps",
		Description: fmt.Sprintf(
			"%d failures across %d template(s) were caused by missing secrets or configmaps. "+
				"This is often a sign that prerequisite setup steps are not documented clearly, "+
				"or that the workflow does not validate dependencies before starting.",
			total, len(seen),
		),
		Recommendation: "1) Add a preflight check step that validates all required secrets/configmaps exist before the DAG proceeds. " +
			"2) Update the workflow's README/annotation with an explicit prerequisites section listing every required secret. " +
			"3) Consider a self-service secret provisioning guide in your internal developer portal.",
		AffectedTemplates: templates,
		SupportingData:    fmt.Sprintf("%d missing-config failures", total),
	})
	return insights
}

// ruleHighFailureRate: if the overall failure rate is very high, surface it.
func ruleHighFailureRate(metrics models.Metrics) []models.DevExInsight {
	if metrics.FailedCount == 0 {
		return nil
	}
	if metrics.FailurePercentage < 40 {
		return nil
	}
	return []models.DevExInsight{{
		Priority: models.PriorityHigh,
		Type:     models.InsightGeneral,
		Title:    fmt.Sprintf("Very high failure rate: %.0f%%", metrics.FailurePercentage),
		Description: fmt.Sprintf(
			"%.0f%% of workflows in this query window failed (%d/%d). "+
				"This is significantly above a healthy baseline and suggests a systemic problem.",
			metrics.FailurePercentage, metrics.FailedCount, metrics.TotalWorkflows,
		),
		Recommendation: "Cross-reference the failure timeline with recent platform changes, deployments, or infrastructure events. " +
			"Use the patterns table to identify whether failures are concentrated in a small number of templates (targeted fix) " +
			"or spread across many templates (systemic/infrastructure issue).",
		SupportingData: fmt.Sprintf("%d failed / %d total", metrics.FailedCount, metrics.TotalWorkflows),
	}}
}

// ruleDominantCategory: if one category accounts for >60% of failures, call it out.
func ruleDominantCategory(patterns []models.FailurePattern, metrics models.Metrics) []models.DevExInsight {
	if metrics.FailedCount < 5 {
		return nil
	}
	type entry struct {
		cat   models.FailureCategory
		count int
		pct   float64
	}
	candidates := []entry{
		{models.CategoryPlatform, metrics.PlatformCount, float64(metrics.PlatformCount) / float64(metrics.FailedCount) * 100},
		{models.CategoryApplication, metrics.ApplicationCount, float64(metrics.ApplicationCount) / float64(metrics.FailedCount) * 100},
		{models.CategoryDevEx, metrics.DevExCount, float64(metrics.DevExCount) / float64(metrics.FailedCount) * 100},
	}

	for _, e := range candidates {
		if e.pct < 60 || e.count == 0 {
			continue
		}
		switch e.cat {
		case models.CategoryDevEx:
			return []models.DevExInsight{{
				Priority: models.PriorityHigh,
				Type:     models.InsightAddGuardrail,
				Title:    fmt.Sprintf("%.0f%% of failures are DevEx gaps — platform guardrails needed", e.pct),
				Description: fmt.Sprintf(
					"%d of %d failures (%0.f%%) are classified as DevEx issues: missing retries, "+
						"missing validation, unclear errors, timeouts, or flaky steps. "+
						"These are platform-fixable problems that developers are repeatedly hitting.",
					e.count, metrics.FailedCount, e.pct,
				),
				Recommendation: "Prioritise guardrail work: (1) audit templates for missing retryStrategy, " +
					"(2) add parameter validation schemas, (3) improve error message quality standards, " +
					"(4) create a template linting step in your CI pipeline that enforces these standards.",
				SupportingData: fmt.Sprintf("%d DevEx failures", e.count),
			}}
		case models.CategoryPlatform:
			// Covered by ruleFrequentPlatformOutages already — skip to avoid duplication.
		}
	}
	return nil
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// collectTemplates returns a map[templateName]totalOccurrences for patterns
// matching the given subtype, filtered to those with >= minOccurrences.
func collectTemplates(patterns []models.FailurePattern, subtype models.FailureSubtype, minOccurrences int) map[string]int {
	result := map[string]int{}
	for _, p := range patterns {
		if p.Subtype != subtype {
			continue
		}
		if p.OccurrenceCount < minOccurrences {
			continue
		}
		result[p.TemplateName] += p.OccurrenceCount
	}
	return result
}

// topN returns the N subtypes with highest counts as formatted strings.
func topN(counts map[models.FailureSubtype]int, n int) []string {
	type kv struct {
		k models.FailureSubtype
		v int
	}
	var sorted []kv
	for k, v := range counts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })
	var result []string
	for i, kv := range sorted {
		if i >= n {
			break
		}
		result = append(result, fmt.Sprintf("%s (%d)", kv.k, kv.v))
	}
	return result
}

// dedup removes insights with the same (Type, first AffectedTemplate) key.
func dedup(insights []models.DevExInsight) []models.DevExInsight {
	seen := map[string]struct{}{}
	var out []models.DevExInsight
	for _, ins := range insights {
		tmpl := ""
		if len(ins.AffectedTemplates) > 0 {
			tmpl = ins.AffectedTemplates[0]
		}
		key := string(ins.Type) + "|" + tmpl
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ins)
	}
	return out
}

func sortInsights(insights []models.DevExInsight) {
	order := map[models.InsightPriority]int{
		models.PriorityHigh:   0,
		models.PriorityMedium: 1,
		models.PriorityLow:    2,
	}
	sort.SliceStable(insights, func(i, j int) bool {
		oi, oj := order[insights[i].Priority], order[insights[j].Priority]
		if oi != oj {
			return oi < oj
		}
		return insights[i].Title < insights[j].Title
	})
}

func templateLabel(t string) string {
	if t == "" {
		return "(unknown template)"
	}
	return t
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// unique deduplicates a string slice while preserving order.
func unique(ss []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

var _ = unique // silence unused warning; used externally if needed
var _ = strings.Join
