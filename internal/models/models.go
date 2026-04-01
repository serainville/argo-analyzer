package models

import "time"

// ── Argo API wire types ───────────────────────────────────────────────────────

type ArgoWorkflowList struct {
	Metadata ListMetadata `json:"metadata"`
	Items    []Workflow   `json:"items"`
}

type ListMetadata struct {
	Continue           string `json:"continue"`
	ResourceVersion    string `json:"resourceVersion"`
	RemainingItemCount *int64 `json:"remainingItemCount,omitempty"`
}

type Workflow struct {
	Metadata WorkflowMetadata `json:"metadata"`
	Spec     WorkflowSpec     `json:"spec"`
	Status   WorkflowStatus   `json:"status"`
}

type WorkflowMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

type WorkflowSpec struct {
	Entrypoint string `json:"entrypoint"`
}

type WorkflowStatus struct {
	Phase      string                `json:"phase"`
	StartedAt  string                `json:"startedAt"`
	FinishedAt string                `json:"finishedAt"`
	Message    string                `json:"message,omitempty"`
	Nodes      map[string]NodeStatus `json:"nodes,omitempty"`
	Progress   string                `json:"progress,omitempty"`
}

type NodeStatus struct {
	ID           string       `json:"id"`
	Name         string       `json:"displayName"`
	FullName     string       `json:"name"`
	Type         string       `json:"type"`
	Phase        string       `json:"phase"`
	Message      string       `json:"message,omitempty"`
	StartedAt    string       `json:"startedAt"`
	FinishedAt   string       `json:"finishedAt"`
	TemplateName string       `json:"templateName,omitempty"`
	Children     []string     `json:"children,omitempty"`
	Inputs       *NodeInputs  `json:"inputs,omitempty"`
	Outputs      *NodeOutputs `json:"outputs,omitempty"`
}

type NodeInputs struct {
	Parameters []Parameter `json:"parameters,omitempty"`
}

type NodeOutputs struct {
	Parameters []Parameter `json:"parameters,omitempty"`
	ExitCode   string      `json:"exitCode,omitempty"`
}

type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// ── Failure classification ────────────────────────────────────────────────────

// FailureCategory is the top-level responsibility bucket.
type FailureCategory string

const (
	// CategoryPlatform — infrastructure, scheduler, or Argo control plane is at fault.
	CategoryPlatform FailureCategory = "platform"
	// CategoryApplication — user workload code or its configuration is at fault.
	CategoryApplication FailureCategory = "application"
	// CategoryDevEx — a gap in the platform experience is at fault: missing guardrails,
	// absent retry policy, opaque errors, documentation holes, etc.
	CategoryDevEx   FailureCategory = "devex"
	CategoryUnknown FailureCategory = "unknown"
)

// FailureSubtype is the specific failure mode within a category.
type FailureSubtype string

const (
	// Platform subtypes
	SubtypeOOMKill        FailureSubtype = "oom_kill"
	SubtypePodEviction    FailureSubtype = "pod_eviction"
	SubtypeImagePull      FailureSubtype = "image_pull"
	SubtypeStorageFail    FailureSubtype = "storage_failure"
	SubtypeNetworkTimeout FailureSubtype = "network_timeout"
	SubtypeResourceQuota  FailureSubtype = "resource_quota"
	SubtypeNodePressure   FailureSubtype = "node_pressure"
	SubtypeArgoInternal   FailureSubtype = "argo_internal"
	SubtypePodSchedule    FailureSubtype = "pod_scheduling"

	// Application subtypes
	SubtypeExitNonZero      FailureSubtype = "exit_nonzero"
	SubtypeAssertion        FailureSubtype = "assertion_error"
	SubtypeDepFail          FailureSubtype = "dependency_failure"
	SubtypeMissingConfig    FailureSubtype = "missing_config_or_secret"
	SubtypeTestFail         FailureSubtype = "test_failure"
	SubtypePermissionDenied FailureSubtype = "permission_denied"
	SubtypeInvalidInput     FailureSubtype = "invalid_input"

	// DevEx subtypes — gaps in the platform experience
	SubtypeTimeoutTooShort   FailureSubtype = "timeout_too_short"
	SubtypeMissingRetry      FailureSubtype = "missing_retry_policy"
	SubtypeNoResourceLimits  FailureSubtype = "no_resource_limits_set"
	SubtypeFlaky             FailureSubtype = "flaky_step"
	SubtypeUnclearError      FailureSubtype = "unclear_error_message"
	SubtypeNoInputValidation FailureSubtype = "no_input_validation"

	SubtypeUnclassified FailureSubtype = "unclassified"
)

// ClassifierConfidence indicates how certain the rules-based classification is.
type ClassifierConfidence string

const (
	ConfidenceHigh   ClassifierConfidence = "high"
	ConfidenceMedium ClassifierConfidence = "medium"
	ConfidenceLow    ClassifierConfidence = "low"
)

// Classification is the output of classifying a single failed node.
// ClassifiedBy is "rules" now; reserved for "llm" when that stage is added.
type Classification struct {
	Category     FailureCategory      `json:"category"`
	Subtype      FailureSubtype       `json:"subtype"`
	Confidence   ClassifierConfidence `json:"confidence"`
	Reasoning    string               `json:"reasoning"`
	ClassifiedBy string               `json:"classified_by"`
}

// ── Core analysis types ───────────────────────────────────────────────────────

// FailedNode is a failed leaf node with its classification attached.
type FailedNode struct {
	WorkflowName     string
	WorkflowTemplate string // metadata.labels["workflows.argoproj.io/workflow-template"]
	Namespace        string
	NodeID           string
	NodeName         string   // displayName of the failed node itself
	NodePath         []string // displayNames from DAG root down to this node
	TemplateName     string
	Phase            string
	Message          string
	StartedAt        time.Time
	FinishedAt       time.Time
	Duration         time.Duration
	ExitCode         string
	Classification   Classification
}

// AnalyzedWorkflow is the fully analyzed result of a single workflow.
type AnalyzedWorkflow struct {
	Name        string
	Namespace   string
	Phase       string
	StartedAt   time.Time
	FinishedAt  time.Time
	Duration    time.Duration
	Message     string
	FailedNodes []FailedNode
}

// ── Pattern detection ─────────────────────────────────────────────────────────

// FailurePattern is a recurring failure signature detected across multiple workflows.
type FailurePattern struct {
	PatternKey            string
	Category              FailureCategory
	Subtype               FailureSubtype
	TemplateName          string // empty = spans multiple templates
	OccurrenceCount       int
	AffectedWorkflows     []string // deduplicated, sorted
	AffectedNamespaces    []string // deduplicated, sorted
	FirstSeen             time.Time
	LastSeen              time.Time
	IsFlaky               bool     // failed then retried+succeeded in the same workflow
	TypicalExitCodes      []string // deduplicated
	RepresentativeMessage string
}

// ── DevEx insights ────────────────────────────────────────────────────────────

type InsightPriority string

const (
	PriorityHigh   InsightPriority = "high"
	PriorityMedium InsightPriority = "medium"
	PriorityLow    InsightPriority = "low"
)

type InsightType string

const (
	InsightAddRetry          InsightType = "add_retry_policy"
	InsightAddValidation     InsightType = "add_input_validation"
	InsightAddResourceLimits InsightType = "add_resource_limits"
	InsightImproveError      InsightType = "improve_error_message"
	InsightImproveDoc        InsightType = "improve_documentation"
	InsightInvestigatePlat   InsightType = "investigate_platform"
	InsightReviewTimeout     InsightType = "review_timeout_config"
	InsightReduceFlakiness   InsightType = "reduce_flakiness"
	InsightAddGuardrail      InsightType = "add_guardrail"
	InsightGeneral           InsightType = "general"
)

// DevExInsight is a concrete, actionable recommendation derived from pattern analysis.
type DevExInsight struct {
	Priority          InsightPriority `json:"priority"`
	Type              InsightType     `json:"type"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	Recommendation    string          `json:"recommendation"`
	AffectedTemplates []string        `json:"affected_templates,omitempty"`
	SupportingData    string          `json:"supporting_data"`
}

// ── Top-level report ──────────────────────────────────────────────────────────

// DurationStats holds statistical summary of a set of durations.
// All fields are zero if no data points were recorded.
type DurationStats struct {
	Min    time.Duration `json:"min_sec"`
	Max    time.Duration `json:"max_sec"`
	Mean   time.Duration `json:"mean_sec"`
	Median time.Duration `json:"median_sec"`
	Count  int           `json:"count"`
}

// SlowestEntry is one ranked entry in a slowest-N list.
type SlowestEntry struct {
	Name         string // workflow name or node display name
	WorkflowName string // parent workflow (empty for workflow-level entries)
	TemplateName string
	Phase        string
	Duration     time.Duration
}

// TemplateFailureCount is one entry in the top-failing-templates ranking.
type TemplateFailureCount struct {
	TemplateName     string
	Count            int
	DominantCategory FailureCategory
}

// Metrics holds all computed counters and rates for the report.
type Metrics struct {
	TotalWorkflows    int
	SuccessfulCount   int
	FailedCount       int
	SuccessPercentage float64
	FailurePercentage float64

	// Failure counts broken down by responsibility category
	PlatformCount    int
	ApplicationCount int
	DevExCount       int
	UnknownCount     int

	// Top templates by failure frequency (capped at 10)
	TopFailingTemplates []TemplateFailureCount

	// Duration statistics — computed over terminal (non-running) workflows only
	AllWorkflowDuration DurationStats // all terminal workflows
	FailedDuration      DurationStats // failed workflows only
	SuccessfulDuration  DurationStats // successful workflows only

	// Slowest individual workflow runs (top 10, by total duration)
	SlowestWorkflows []SlowestEntry

	// Slowest individual template steps across all failed workflow nodes (top 10)
	SlowestTemplates []SlowestEntry
}

// Report is the complete output of a full analysis run.
type Report struct {
	GeneratedAt time.Time
	QueryType   string
	QueryValue  string

	Metrics         Metrics
	FailedWorkflows []AnalyzedWorkflow
	Patterns        []FailurePattern
	Insights        []DevExInsight
}
