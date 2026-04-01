package models

import "time"

// ArgoWorkflowList represents the list response from Argo Workflows API
type ArgoWorkflowList struct {
	Metadata ListMetadata `json:"metadata"`
	Items    []Workflow   `json:"items"`
}

// ListMetadata holds pagination info
type ListMetadata struct {
	Continue           string `json:"continue"`
	ResourceVersion    string `json:"resourceVersion"`
	RemainingItemCount *int64 `json:"remainingItemCount,omitempty"`
}

// Workflow represents a single Argo Workflow
type Workflow struct {
	Metadata WorkflowMetadata `json:"metadata"`
	Spec     WorkflowSpec     `json:"spec"`
	Status   WorkflowStatus   `json:"status"`
}

// WorkflowMetadata holds k8s metadata
type WorkflowMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

// WorkflowSpec holds the workflow specification
type WorkflowSpec struct {
	Entrypoint string `json:"entrypoint"`
}

// WorkflowStatus holds the workflow execution status
type WorkflowStatus struct {
	Phase      string                `json:"phase"`
	StartedAt  string                `json:"startedAt"`
	FinishedAt string                `json:"finishedAt"`
	Message    string                `json:"message,omitempty"`
	Nodes      map[string]NodeStatus `json:"nodes,omitempty"`
	Progress   string                `json:"progress,omitempty"`
}

// NodeStatus represents the status of a single node/step in the workflow DAG
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
	Inputs       *NodeInputs  `json:"inputs,omitempty"`
	Outputs      *NodeOutputs `json:"outputs,omitempty"`
}

// NodeInputs holds input parameters/artifacts for a node
type NodeInputs struct {
	Parameters []Parameter `json:"parameters,omitempty"`
}

// NodeOutputs holds output parameters/artifacts for a node
type NodeOutputs struct {
	Parameters []Parameter `json:"parameters,omitempty"`
	ExitCode   string      `json:"exitCode,omitempty"`
}

// Parameter is a key-value pair
type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// FailedNode represents a failed leaf node extracted from a workflow
type FailedNode struct {
	WorkflowName string
	Namespace    string
	NodeID       string
	NodeName     string
	TemplateName string
	Phase        string
	Message      string
	StartedAt    time.Time
	FinishedAt   time.Time
	Duration     time.Duration
	ExitCode     string
}

// AnalyzedWorkflow is the result of analyzing a single workflow
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

// Report is the full analysis report
type Report struct {
	GeneratedAt       time.Time
	TotalWorkflows    int
	SuccessfulCount   int
	FailedCount       int
	SuccessPercentage float64
	FailurePercentage float64
	FailedWorkflows   []AnalyzedWorkflow
	QueryType         string
	QueryValue        string
}
