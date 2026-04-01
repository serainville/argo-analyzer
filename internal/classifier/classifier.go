// Package classifier categorises a failed Argo node into a FailureCategory and
// FailureSubtype using a deterministic, rules-based approach.
//
// Design contract
// ───────────────
//   - No external dependencies — pure string matching + exit-code logic.
//   - ClassifiedBy is always "rules". An LLM stage can be bolted on later by
//     wrapping Classify() and re-classifying nodes where Confidence == Low.
//   - Rules are evaluated in priority order; the first match wins.
//   - If no rule fires the result is CategoryUnknown / SubtypeUnclassified / Low.
package classifier

import (
	"fmt"
	"regexp"
	"strings"

	"argo-analyzer/internal/models"
)

// Classify returns a Classification for a single failed node.
// message is the raw failure message from Argo; exitCode may be empty.
func Classify(message, exitCode, templateName string) models.Classification {
	msg := strings.ToLower(strings.TrimSpace(message))

	for _, rule := range orderedRules {
		if rule.matches(msg, exitCode, templateName) {
			return models.Classification{
				Category:     rule.category,
				Subtype:      rule.subtype,
				Confidence:   rule.confidence,
				Reasoning:    rule.reasoning(message, exitCode),
				ClassifiedBy: "rules",
			}
		}
	}

	// Final heuristic: any non-zero exit code that didn't match a specific rule
	// is an application failure (the process itself exited badly).
	if exitCode != "" && exitCode != "0" {
		return models.Classification{
			Category:     models.CategoryApplication,
			Subtype:      models.SubtypeExitNonZero,
			Confidence:   models.ConfidenceMedium,
			Reasoning:    fmt.Sprintf("process exited with code %s; no specific rule matched the message", exitCode),
			ClassifiedBy: "rules",
		}
	}

	return models.Classification{
		Category:     models.CategoryUnknown,
		Subtype:      models.SubtypeUnclassified,
		Confidence:   models.ConfidenceLow,
		Reasoning:    "no rule matched; manual review recommended",
		ClassifiedBy: "rules",
	}
}

// ── Rule engine ───────────────────────────────────────────────────────────────

type rule struct {
	category   models.FailureCategory
	subtype    models.FailureSubtype
	confidence models.ClassifierConfidence
	// matches is called with the lower-cased message, raw exit code, and template name.
	matches func(msg, exitCode, template string) bool
	// reasoning produces a human-readable explanation given the original (raw) message.
	reasoning func(rawMsg, exitCode string) string
}

// orderedRules are evaluated top-to-bottom; first match wins.
// More-specific rules must come before more-general ones.
var orderedRules = []rule{

	// ── Platform: OOM kill ────────────────────────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeOOMKill,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"oom", "out of memory", "memory limit exceeded",
				"killed", "oomkilled", "signal: killed",
				"exit code 137",
			) || exitCodeIs("", "137")
		},
		reasoning: func(raw, code string) string {
			return fmt.Sprintf("OOM kill detected (exit code %s / message: %q) — pod exceeded its memory limit", code, truncate(raw, 120))
		},
	},

	// ── Platform: Pod eviction ────────────────────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypePodEviction,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg, "evicted", "eviction", "the node was low on resource")
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("pod was evicted by the kubelet (node resource pressure): %q", truncate(raw, 120))
		},
	},

	// ── Platform: Node pressure / not-ready ───────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeNodePressure,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"node is not ready", "node not ready", "nodepressure",
				"diskpressure", "memorypressure", "pidpressure",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("node was under resource pressure or not ready: %q", truncate(raw, 120))
		},
	},

	// ── Platform: Pod scheduling ──────────────────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypePodSchedule,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"unschedulable", "insufficient cpu", "insufficient memory",
				"no nodes available", "failed to schedule",
				"0/", "nodes are available",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("pod could not be scheduled (insufficient cluster capacity): %q", truncate(raw, 120))
		},
	},

	// ── Platform: Image pull ──────────────────────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeImagePull,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"errimagepull", "imagepullbackoff", "back-off pulling image",
				"failed to pull image", "image pull", "no such image",
				"manifest unknown", "unauthorized: authentication required",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("container image could not be pulled: %q", truncate(raw, 120))
		},
	},

	// ── Platform: Storage / PVC ───────────────────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeStorageFail,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"persistentvolumeclaim", "pvc", "volume mount",
				"failed to mount", "unable to mount", "no such file or directory",
				"read-only file system", "input/output error",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("storage/volume failure: %q", truncate(raw, 120))
		},
	},

	// ── Platform: Resource quota ──────────────────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeResourceQuota,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"exceeded quota", "resource quota", "forbidden: exceeded",
				"limitrange", "quota exceeded",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("namespace resource quota exceeded: %q", truncate(raw, 120))
		},
	},

	// ── Platform: Argo internal / controller ─────────────────────────────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeArgoInternal,
		confidence: models.ConfidenceMedium,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"workflow-controller", "argo-server", "deadline exceeded",
				"failed to get workflow", "failed to update workflow",
				"container state unknown", "pod failed to start",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("Argo controller or internal error: %q", truncate(raw, 120))
		},
	},

	// ── Platform: Network timeout (infra-level) ───────────────────────────────
	// Note: we classify as platform only when it's clearly infrastructure DNS/LB.
	// Generic "connection refused" is left for later rules (could be app).
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypeNetworkTimeout,
		confidence: models.ConfidenceMedium,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"i/o timeout", "context deadline exceeded",
				"dial tcp: lookup", "no such host",
				"dns resolution failed", "dns lookup failed",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("infrastructure-level network/DNS failure: %q", truncate(raw, 120))
		},
	},

	// ── DevEx: Workflow-level deadline / activeDeadlineSeconds ───────────────
	// Must come before generic timeout rule.
	{
		category:   models.CategoryDevEx,
		subtype:    models.SubtypeTimeoutTooShort,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"activedeadlineseconds", "workflow timeout", "timed out",
				"execution timeout", "step timed out",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("workflow or step timeout fired — activeDeadlineSeconds may be too short: %q", truncate(raw, 120))
		},
	},

	// ── DevEx: Missing retry — transient error pattern ────────────────────────
	{
		category:   models.CategoryDevEx,
		subtype:    models.SubtypeMissingRetry,
		confidence: models.ConfidenceMedium,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"connection refused", "connection reset by peer",
				"503 service unavailable", "502 bad gateway",
				"temporary failure", "transient", "try again",
				"too many requests", "rate limit",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("transient/retryable error with no retry policy configured: %q", truncate(raw, 120))
		},
	},

	// ── DevEx: No input validation ────────────────────────────────────────────
	{
		category:   models.CategoryDevEx,
		subtype:    models.SubtypeNoInputValidation,
		confidence: models.ConfidenceMedium,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"invalid argument", "missing required", "required field",
				"cannot be empty", "must not be empty", "is required",
				"value is required", "parameter is required",
				"no such argument", "unexpected argument",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("input validation gap — bad parameters reached execution without being caught earlier: %q", truncate(raw, 120))
		},
	},

	// ── DevEx: Opaque / unclear error ─────────────────────────────────────────
	{
		category:   models.CategoryDevEx,
		subtype:    models.SubtypeUnclearError,
		confidence: models.ConfidenceLow,
		matches:    matchesUnclearError,
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("error message is too generic to diagnose without context (%q) — consider improving error surfacing in this template", truncate(raw, 120))
		},
	},

	// ── Application: Missing secret / config ─────────────────────────────────
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypeMissingConfig,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"secret not found", "configmap not found",
				"environment variable not set", "env var",
				"missing env", "no such variable",
				"failed to find secret", "secret does not exist",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("missing secret or configmap — workflow has a configuration dependency that was not satisfied: %q", truncate(raw, 120))
		},
	},

	// ── Application: Permission denied ───────────────────────────────────────
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypePermissionDenied,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"permission denied", "access denied", "forbidden",
				"403", "unauthorized", "rbac:",
				"not authorized", "insufficient permissions",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("permission/RBAC failure — workload lacks access to a required resource: %q", truncate(raw, 120))
		},
	},

	// ── Application: Test failure ─────────────────────────────────────────────
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypeTestFail,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"tests failed", "test failed", "test suite",
				"--- fail", "failures:", "assertion failed",
				"expected ", "got ", "want ", "actual ",
				"junit", "pytest", "go test", "rspec",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("automated test failure: %q", truncate(raw, 120))
		},
	},

	// ── Application: Assertion / panic ───────────────────────────────────────
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypeAssertion,
		confidence: models.ConfidenceHigh,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"panic:", "unhandled exception", "uncaught exception",
				"fatal error", "sigsegv", "signal: segmentation fault",
				"core dumped", "abort()", "aborted",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("process crashed (panic/segfault): %q", truncate(raw, 120))
		},
	},

	// ── Application: Dependency failure (app-owned) ───────────────────────────
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypeDepFail,
		confidence: models.ConfidenceMedium,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"database connection", "db connection", "sql:",
				"redis:", "kafka:", "rabbitmq",
				"upstream connect error", "upstream request timeout",
				"failed to connect to", "connection timeout",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("downstream dependency failure (database/broker/service): %q", truncate(raw, 120))
		},
	},

	// ── Application: Invalid input (runtime) ─────────────────────────────────
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypeInvalidInput,
		confidence: models.ConfidenceMedium,
		matches: func(msg, _, _ string) bool {
			return containsAny(msg,
				"invalid json", "unmarshal", "parse error",
				"unexpected token", "syntax error",
				"malformed", "invalid format", "failed to parse",
			)
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("invalid or malformed input data at runtime: %q", truncate(raw, 120))
		},
	},

	// ── Application: Generic non-zero exit (exit 1, 2, etc.) ─────────────────
	// This is a catch-all placed deliberately late — more specific rules above
	// should have matched common non-zero cases already.
	{
		category:   models.CategoryApplication,
		subtype:    models.SubtypeExitNonZero,
		confidence: models.ConfidenceMedium,
		matches: func(msg, exitCode, _ string) bool {
			if exitCode == "" || exitCode == "0" {
				return false
			}
			// Exit 137 = OOM (already handled above); 143 = SIGTERM (often eviction).
			return exitCode != "137" && exitCode != "143"
		},
		reasoning: func(raw, code string) string {
			return fmt.Sprintf("process exited with non-zero code %s: %q", code, truncate(raw, 120))
		},
	},

	// ── Application: SIGTERM (exit 143) — often platform-initiated ───────────
	{
		category:   models.CategoryPlatform,
		subtype:    models.SubtypePodEviction,
		confidence: models.ConfidenceMedium,
		matches: func(_, exitCode, _ string) bool {
			return exitCode == "143"
		},
		reasoning: func(raw, _ string) string {
			return fmt.Sprintf("process received SIGTERM (exit 143) — likely pod preemption or graceful shutdown: %q", truncate(raw, 120))
		},
	},
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func exitCodeIs(exitCode, target string) bool {
	return strings.TrimSpace(exitCode) == target
}

// matchesUnclearError catches messages that are so generic they give the
// platform engineer no actionable information.
var unclearPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^error$`),
	regexp.MustCompile(`^failed$`),
	regexp.MustCompile(`^exit status \d+$`),
	regexp.MustCompile(`^(step|task|job) failed$`),
	regexp.MustCompile(`^unknown error$`),
	regexp.MustCompile(`^internal (server )?error$`),
}

func matchesUnclearError(msg, _, _ string) bool {
	trimmed := strings.TrimSpace(msg)
	for _, re := range unclearPatterns {
		if re.MatchString(trimmed) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
