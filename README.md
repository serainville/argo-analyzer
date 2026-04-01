# argo-analyzer

A CLI tool for pulling and analyzing failed archived workflows from an **Argo Workflows** server via its REST API.

---

## Features

- **Two query modes:**
  - `count` — pull the N most recent archived workflows
  - `window` — pull workflows started within a time range
- **Failure analysis** — identifies all failed *leaf* nodes in the workflow DAG (not just the top-level status)
- **Metrics** — total, successful, failed, and percentages
- **Three output formats:**
  - Console table (rendered with `tablewriter`)
  - CSV file
  - JSON file
- Bearer token auth, TLS skip, configurable timeout, multi-namespace support

---

## Build

```bash
# Requires Go 1.22+
git clone https://github.com/your-org/argo-analyzer
cd argo-analyzer
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
| `--csv` | | *(none)* | Path to write CSV report |
| `--json` | | *(none)* | Path to write JSON report |
| `--verbose` | `-v` | `false` | Show extra detail (node IDs) |

### `count` — analyze N most recent workflows

```bash
argo-analyzer count \
  --server https://argo.example.com \
  --token eyJhbGci... \
  --count 100 \
  --csv report.csv \
  --json report.json
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--count` | `-c` | `50` | Number of workflows to fetch |

### `window` — analyze workflows in a time range

```bash
argo-analyzer window \
  --server https://argo.example.com \
  --from 2024-06-01T00:00:00Z \
  --to   2024-06-30T23:59:59Z \
  --csv june-report.csv \
  --json june-report.json
```

| Flag | Default | Description |
|---|---|---|
| `--from` | *(required)* | Window start (RFC3339) |
| `--to` | now | Window end (RFC3339) |

---

## Authentication

Pass the bearer token via `--token` or the environment variable:

```bash
export ARGO_TOKEN="$(kubectl get secret -n argo argo-server-sa-token -o=jsonpath='{.data.token}' | base64 --decode)"
argo-analyzer count --server https://argo.example.com --count 50
```

---

## Console output example

```
════════════════════════════════════════════════════════════════
                ARGO WORKFLOWS FAILURE REPORT
════════════════════════════════════════════════════════════════
  Generated at : Tue, 18 Jun 2024 14:32:01 UTC
  Query        : count = 100

  METRICS
  ───────────────────────────────────────────
  Metric            │ Count │ Percentage
  ──────────────────┼───────┼──────────────
  Total Workflows   │ 100   │ —
  Successful        │ 87    │ 87.0%
  Failed            │ 13    │ 13.0%

  FAILED WORKFLOWS
  ...

  FAILED NODE DETAILS
  ───────────────────────────────────────────
  Workflow │ Node │ Template │ Phase │ Exit Code │ Duration │ Failure Reason
  ...
```

---

## CSV columns

`workflow_name`, `namespace`, `workflow_phase`, `workflow_started_at`, `workflow_finished_at`,
`workflow_duration_sec`, `workflow_message`, `node_id`, `node_name`, `template_name`,
`node_phase`, `node_exit_code`, `node_started_at`, `node_finished_at`,
`node_duration_sec`, `node_failure_reason`

---

## JSON structure

```json
{
  "generated_at": "2024-06-18T14:32:01Z",
  "query_type": "count",
  "query_value": "100",
  "metrics": {
    "total_workflows": 100,
    "successful_count": 87,
    "failed_count": 13,
    "success_percentage": 87.0,
    "failure_percentage": 13.0
  },
  "failed_workflows": [
    {
      "name": "my-pipeline-abc12",
      "namespace": "argo",
      "phase": "Failed",
      "started_at": "2024-06-17T10:00:00Z",
      "finished_at": "2024-06-17T10:05:30Z",
      "duration_sec": 330.0,
      "message": "child 'step-3' failed",
      "failed_nodes": [
        {
          "node_id": "my-pipeline-abc12-1234567890",
          "node_name": "step-3",
          "template_name": "process-data",
          "phase": "Failed",
          "exit_code": "1",
          "started_at": "2024-06-17T10:04:00Z",
          "finished_at": "2024-06-17T10:05:30Z",
          "duration_sec": 90.0,
          "message": "Error: connection refused to db-service:5432"
        }
      ]
    }
  ]
}
```

---

## Project layout

```
argo-analyzer/
├── cmd/
│   └── main.go                  # CLI entry point (Cobra commands)
├── internal/
│   ├── client/
│   │   └── client.go            # Argo Workflows REST API client
│   ├── analyzer/
│   │   └── analyzer.go          # Workflow + failed-leaf analysis
│   ├── reporter/
│   │   └── reporter.go          # Console, CSV, JSON output
│   └── models/
│       └── models.go            # Shared types
├── go.mod
├── go.sum
└── README.md
```
