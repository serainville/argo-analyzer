# argo-analyzer

A CLI tool for pulling, classifying, and analyzing failed archived workflows from an **Argo Workflows** server.

---

## What it does

Beyond basic failure counting, `argo-analyzer` answers the questions platform engineers actually care about:

| Question | How it's answered |
|---|---|
| Is this a platform problem or an application bug? | Rules-based classifier assigns every failed node a **category** and **subtype** |
| Which failure modes are recurring? | **Pattern engine** groups failures across runs by template + normalised message |
| Where should we invest in guardrails? | **Insights engine** derives actionable DevEx recommendations from the patterns |
| Which templates are the biggest pain points? | **Top failing templates** ranked by failure count + dominant category |
| Are any steps flaky? | Pattern engine detects steps that both fail and succeed across runs |

---

## Failure taxonomy

### Categories

| Category | Meaning |
|---|---|
| `platform` | Infrastructure is responsible — OOM, eviction, image pull, scheduling, Argo controller |
| `application` | User workload is responsible — bad code, missing secrets, test failures, permissions |
| `devex` | The **platform experience** has a gap — missing retry policy, no input validation, opaque errors, timeouts |
| `unknown` | Could not be classified — manual review recommended |

### Platform subtypes
`oom_kill` · `pod_eviction` · `image_pull` · `storage_failure` · `network_timeout` · `resource_quota` · `node_pressure` · `argo_internal` · `pod_scheduling`

### Application subtypes
`exit_nonzero` · `assertion_error` · `dependency_failure` · `missing_config_or_secret` · `test_failure` · `permission_denied` · `invalid_input`

### DevEx subtypes
`timeout_too_short` · `missing_retry_policy` · `no_resource_limits_set` · `flaky_step` · `unclear_error_message` · `no_input_validation`

---

## Build

```bash
# Requires Go 1.22+
go build -o argo-analyzer ./cmd
```

---

## Usage

```
argo-analyzer <command> [flags]
```

### Global flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--server` | `-s` | *(required)* | Argo Workflows server URL |
| `--namespace` | `-n` | *(all)* | Kubernetes namespace |
| `--token` | `-t` | `$ARGO_TOKEN` | Bearer token |
| `--insecure` | | `false` | Skip TLS verification |
| `--timeout` | | `30` | HTTP timeout (seconds) |
| `--csv` | | | Path for CSV output |
| `--json` | | | Path for JSON output |
| `--verbose` | `-v` | `false` | Show classifier reasoning per node |

### `count` — analyze N most recent workflows

```bash
argo-analyzer count \
  --server https://argo.example.com \
  --token $ARGO_TOKEN \
  --count 200 \
  --csv report.csv \
  --json report.json
```

### `window` — analyze a time range

```bash
argo-analyzer window \
  --server https://argo.example.com \
  --from 2024-06-01T00:00:00Z \
  --to   2024-06-30T23:59:59Z
```

---

## Console output

```
  ── METRICS ──────────────────────────────────────────────────────────
  Total / Successful / Failed

  ── FAILURE BREAKDOWN BY CATEGORY ────────────────────────────────────
  Platform / Application / DevEx / Unknown  (counts + % + meaning)

  ── TOP FAILING TEMPLATES ────────────────────────────────────────────

  ── RECURRING FAILURE PATTERNS ───────────────────────────────────────
  Category · Subtype · Template · Hits · Workflows · Flaky · Message

  ── DEVEX INSIGHTS & RECOMMENDATIONS ─────────────────────────────────
  ● [HIGH] Add retry policy to process-data
     Template process-data is failing on transient errors across 18
     workflows with no retry policy configured.
     → Add retryStrategy.limit: 3, retryPolicy: OnTransientError

  ── FAILED WORKFLOWS ─────────────────────────────────────────────────
  ── FAILED NODE DETAILS ──────────────────────────────────────────────
  Category · Subtype · Confidence · Exit code · Duration · Reason
  (--verbose adds classifier reasoning)
```

---

## Output schemas

### CSV columns
`workflow_name`, `namespace`, `workflow_phase`, `workflow_started/finished_at`, `workflow_duration_sec`, `node_id`, `node_name`, `template_name`, `node_phase`, `node_exit_code`, `node_started/finished_at`, `node_duration_sec`, `failure_message`, **`category`**, **`subtype`**, **`confidence`**, **`classified_by`**, **`reasoning`**

### JSON top-level keys
`generated_at` · `query_type` · `query_value` · `metrics` (with category breakdown + top templates) · `patterns` · `insights` · `failed_workflows`

---

## Project layout

```
argo-analyzer/
├── cmd/main.go                      # Cobra CLI
├── internal/
│   ├── models/models.go             # All shared types
│   ├── client/client.go             # Argo REST client + pagination
│   ├── classifier/classifier.go     # Rules-based failure classifier
│   ├── patterns/patterns.go         # Cross-workflow pattern detection
│   ├── insights/insights.go         # DevEx insight rules
│   ├── analyzer/analyzer.go         # Pipeline orchestrator
│   └── reporter/reporter.go         # Console / CSV / JSON
└── go.mod
```

## Adding LLM classification later

`classifier.Classify()` returns a `Classification` with `ClassifiedBy: "rules"`.
To add an LLM fallback for low-confidence results:

1. After the rules pass, check `c.Confidence == ConfidenceLow`
2. Call the LLM with the raw message + exit code + template context
3. Overwrite the `Classification` fields; set `ClassifiedBy: "llm"`

No other code changes needed — the rest of the pipeline is already wired to carry
`Classification` through to patterns, insights, CSV, and JSON.
