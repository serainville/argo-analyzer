# Argo Workflows — Failure Analysis Report

**Generated:** Tue, 31 Mar 2026 23:54:46 EDT  
**Query:** count = 50 most recent  

## Summary

| Metric | Count | % |
|--------|------:|---:|
| Total workflows | 50 | — |
| ✅ Successful | 5 | 10.0% |
| ❌ Failed | 45 | 90.0% |

## Failure Breakdown by Category

| Category | Count | % of Failures | Meaning |
|----------|------:|--------------:|---------|
| 🔧 Platform | 0 | — | Infrastructure, scheduler, Argo controller |
| 💻 Application | 52 | 115.6% | User workload code or configuration |
| 🛠 DevEx gap | 0 | — | Missing guardrails, retries, validation, docs |
| ❓ Unknown | 11 | 24.4% | Could not be classified |

## Duration Metrics

| Scope | Count | Min | Max | Mean | Median |
|-------|------:|----:|----:|-----:|-------:|
| All (terminal) | 50 | — | 15m40s | 1m15s | 40s |
| Successful | 5 | 38s | 1m05s | 44s | 40s |
| Failed | 45 | — | 15m40s | 1m19s | 40s |

## Metrics by Workflow Template

### Run Counts

| WF Template | Total | Successful | Failed | Fail % | PLT | APP | DEV | UNK |
|-------------|------:|-----------:|-------:|-------:|----:|----:|----:|----:|
| (unknown) | 39 | 0 | 39 | 100.0% | 0 | 52 | 0 | 11 |
| ci-java | 11 | 5 | 6 | 54.5% | 0 | 0 | 0 | 0 |

### Durations

| WF Template | Scope | Count | Min | Max | Mean | Median |
|-------------|-------|------:|----:|----:|-----:|-------:|
| (unknown) | All | 39 | — | 15m40s | 1m24s | 10s |
|  |   Failed | 39 | — | 15m40s | 1m24s | 10s |
| ci-java | All | 11 | 38s | 1m05s | 45s | 40s |
|  |   Successful | 5 | 38s | 1m05s | 44s | 40s |
|  |   Failed | 6 | 40s | 52s | 45s | 45s |

## Slowest Workflow Templates (Top 10, by median duration)

| # | WF Template | Runs | Median Duration |
|---|------------|-----:|----------------:|
| 1 | ci-java | 11 runs | 40s |
| 2 | (unknown) | 39 runs | 10s |

## Slowest Workflows (Top 10)

| # | Workflow | WF Template | Phase | Duration |
|---|---------|-------------|-------|----------|
| 1 | `app-ci-push-wqkx5` | — | Failed | 15m40s |
| 2 | `app-ci-push-mk968` | — | Failed | 7m09s |
| 3 | `app-ci-push-4bh4d` | — | Failed | 2m53s |
| 4 | `app-ci-push-dqsd2` | — | Failed | 1m59s |
| 5 | `app-ci-push-qwm67` | — | Failed | 1m56s |
| 6 | `app-ci-push-248m8` | — | Failed | 1m55s |
| 7 | `app-ci-push-cbdvf` | — | Failed | 1m54s |
| 8 | `app-ci-push-f8mwb` | — | Failed | 1m54s |
| 9 | `app-ci-push-bxzb6` | — | Failed | 1m52s |
| 10 | `app-ci-push-dflgn` | — | Failed | 1m50s |

## Slowest Failed Template Steps (Top 10)

| # | Node | Template | Workflow | Duration |
|---|------|----------|----------|----------|
| 1 | `sonar-scan` | sonar-scan | `app-ci-push-wqkx5` | 14m08s |
| 2 | `sonar-scan` | sonar-scan | `app-ci-push-mk968` | 5m19s |
| 3 | `sonar-scan` | sonar-scan | `app-ci-push-4bh4d` | 1m09s |
| 4 | `vuln-scan` | repo-vuln-scan | `app-ci-push-4bh4d` | 31s |
| 5 | `vuln-scan` | repo-vuln-scan | `app-ci-push-m868l` | 29s |
| 6 | `vuln-scan` | repo-vuln-scan | `app-ci-push-dqsd2` | 29s |
| 7 | `vuln-scan` | repo-vuln-scan | `app-ci-push-qwm67` | 28s |
| 8 | `vuln-scan` | repo-vuln-scan | `app-ci-push-r6g8j` | 28s |
| 9 | `vuln-scan` | repo-vuln-scan | `app-ci-push-ppf7v` | 27s |
| 10 | `vuln-scan` | repo-vuln-scan | `app-ci-push-7hh4b` | 27s |

## Top Failing Templates

| # | Template | Failures | Dominant Category |
|---|----------|---------|------------------|
| 1 | `repo-vuln-scan` | 18 | application |
| 2 | `sonar-scan` | 18 | application |
| 3 | `checkout` | 16 | application |
| 4 | `code-quality` | 6 | application |
| 5 | `(unknown)` | 5 | unknown |

## Recurring Failure Patterns

| # | WF Template | Category | Subtype | Template | Occurrences | Workflows | Flaky | Representative Message |
|---|------------|----------|---------|----------|------------:|----------:|:-----:|------------------------|
| 1 | — | application | `exit_nonzero` | `repo-vuln-scan` | 18 | 18 |  | main: Error (exit code 1) |
| 2 | — | application | `exit_nonzero` | `checkout` | 14 | 14 |  | main: Error (exit code 128) |
| 3 | — | application | `exit_nonzero` | `sonar-scan` | 7 | 7 |  | main: Error (exit code 1) |
| 4 | — | application | `exit_nonzero` | `sonar-scan` | 6 | 6 |  | main: Error (exit code 2) |
| 5 | — | application | `exit_nonzero` | `code-quality` | 3 | 3 |  | main: Error (exit code 2) |
| 6 | — | unknown | `unclassified` | `sonar-scan` | 2 | 2 |  | build-sonar-truststore: Error (exit code 2) |
| 7 | — | unknown | `unclassified` | `sonar-scan` | 2 | 2 |  | workflow shutdown with strategy:  Stop |
| 8 | — | unknown | `unclassified` | — | 2 | 2 |  | invalid spec: templates.app-ci.tasks.checkout templates.checkout inputs.param... |
| 9 | — | application | `permission_denied` | `checkout` | 2 | 2 |  | task 'app-ci-push-hd4pw.checkout' errored: pods "app-ci-push-hd4pw-checkout-3... |
| 10 | — | unknown | `unclassified` | — | 2 | 2 |  | invalid spec: templates.app-ci.tasks.go-pipeline templates.go-pipeline.inputs... |
| 11 | — | application | `exit_nonzero` | `code-quality` | 2 | 2 |  | main: Error (exit code 1) |
| 12 | — | unknown | `unclassified` | `sonar-scan` | 1 | 1 |  | workflow shutdown with strategy:  Terminate |
| 13 | — | unknown | `unclassified` | — | 1 | 1 |  | invalid spec: templates.app-ci.tasks.go-pipeline templates.go-pipeline.tasks.... |
| 14 | — | unknown | `unclassified` | `code-quality` | 1 | 1 |  | build-truststore: Error (exit code 2) |

## DevEx Insights & Recommendations

### 1. 🔴 Very high failure rate: 90%

**Priority:** HIGH  
**Supporting data:** 45 failed / 50 total  

90% of workflows in this query window failed (45/50). This is significantly above a healthy baseline and suggests a systemic problem.

**Recommendation:** Cross-reference the failure timeline with recent platform changes, deployments, or infrastructure events. Use the patterns table to identify whether failures are concentrated in a small number of templates (targeted fix) or spread across many templates (systemic/infrastructure issue).

## Failed Workflows

| Workflow | WF Template | Namespace | Duration | Failed Nodes | Categories |
|----------|-------------|-----------|----------|-------------:|------------|
| `app-ci-push-dqsd2` | — | argo-workflows-system | 1m59s | 2 | APP×2 |
| `app-ci-push-97frk` | — | argo-workflows-system | 1m41s | 2 | APP×2 |
| `app-ci-push-248m8` | — | argo-workflows-system | 1m55s | 2 | APP×2 |
| `app-ci-push-cbdvf` | — | argo-workflows-system | 1m54s | 2 | APP×2 |
| `app-ci-push-qwm67` | — | argo-workflows-system | 1m56s | 2 | APP×2 |
| `app-ci-push-dflgn` | — | argo-workflows-system | 1m50s | 2 | APP×2 |
| `app-ci-push-f8mwb` | — | argo-workflows-system | 1m54s | 2 | APP×2 |
| `app-ci-push-bxzb6` | — | argo-workflows-system | 1m52s | 2 | APP×2 |
| `app-ci-push-ppf7v` | — | argo-workflows-system | 1m43s | 2 | APP×2 |
| `app-ci-push-r6g8j` | — | argo-workflows-system | 1m41s | 2 | APP×2 |
| `app-ci-push-nhch5` | — | argo-workflows-system | 1m43s | 2 | APP×2 |
| `app-ci-push-wqkx5` | — | argo-workflows-system | 15m40s | 2 | APP×1 UNK×1 |
| `app-ci-push-7hh4b` | — | argo-workflows-system | 1m45s | 2 | APP×1 UNK×1 |
| `app-ci-push-f4g95` | — | argo-workflows-system | 1m42s | 2 | APP×1 UNK×1 |
| `app-ci-push-m868l` | — | argo-workflows-system | 1m42s | 2 | APP×2 |
| `app-ci-push-4xnfm` | — | argo-workflows-system | 1m41s | 2 | APP×2 |
| `app-ci-push-4bh4d` | — | argo-workflows-system | 2m53s | 2 | APP×1 UNK×1 |
| `app-ci-push-mk968` | — | argo-workflows-system | 7m09s | 2 | APP×1 UNK×1 |
| `app-ci-push-zrbq7` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-xb45w` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-cxhlt` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-59gl2` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-zs26t` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-t5lxt` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-q8644` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-q8vvm` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-4dmtd` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-jtth2` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-r9rkr` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-q8r5b` | — | argo-workflows-system | — | 1 | UNK×1 |
| `app-ci-push-lkn6j` | — | argo-workflows-system | — | 1 | UNK×1 |
| `app-ci-push-gg2hd` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-hkzvv` | — | argo-workflows-system | 11s | 1 | APP×1 |
| `app-ci-push-j76ff` | — | argo-workflows-system | 10s | 1 | APP×1 |
| `app-ci-push-hgz5b` | — | argo-workflows-system | — | 1 | APP×1 |
| `app-ci-push-hd4pw` | — | argo-workflows-system | — | 1 | APP×1 |
| `app-ci-push-sjmqk` | — | argo-workflows-system | — | 1 | UNK×1 |
| `app-ci-push-xqdjf` | — | argo-workflows-system | — | 1 | UNK×1 |
| `app-ci-push-w8crb` | — | argo-workflows-system | — | 1 | UNK×1 |
| `ci-java-t7mmz` | ci-java | argo-workflows-system | 51s | 1 | APP×1 |
| `ci-java-jwc5q` | ci-java | argo-workflows-system | 40s | 1 | APP×1 |
| `ci-java-9s5gw` | ci-java | argo-workflows-system | 40s | 1 | APP×1 |
| `ci-java-44bcj` | ci-java | argo-workflows-system | 52s | 1 | APP×1 |
| `ci-java-4xk9n` | ci-java | argo-workflows-system | 51s | 1 | UNK×1 |
| `ci-java-pxz9t` | ci-java | argo-workflows-system | 40s | 1 | APP×1 |

## Failed Node Details

| WF Template | Node | Template | Path | Category | Subtype | Confidence | Exit | Failure Reason |
|-------------|------|----------|------|----------|---------|:----------:|:----:|----------------|
| — | `sonar-scan` | `sonar-scan` | app-ci-push-dqsd2 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-dqsd2 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-97frk → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-97frk → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-248m8 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-248m8 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-cbdvf → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-cbdvf → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-qwm67 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-qwm67 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-dflgn → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-dflgn → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-f8mwb → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-f8mwb → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-bxzb6 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-bxzb6 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-ppf7v → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-ppf7v → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-r6g8j → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-r6g8j → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-nhch5 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-nhch5 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-wqkx5 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-wqkx5 → checkout → detect-language → ja... | unknown | `unclassified` | low | — | workflow shutdown with strategy:  Terminate |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-7hh4b → checkout → detect-language → ja... | unknown | `unclassified` | low | — | build-sonar-truststore: Error (exit code 2) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-7hh4b → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-f4g95 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-f4g95 → checkout → detect-language → ja... | unknown | `unclassified` | low | — | build-sonar-truststore: Error (exit code 2) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-m868l → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-m868l → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-4xnfm → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-4xnfm → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-4bh4d → checkout → detect-language → ja... | unknown | `unclassified` | low | — | workflow shutdown with strategy:  Stop |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-4bh4d → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `vuln-scan` | `repo-vuln-scan` | app-ci-push-mk968 → checkout → detect-language → ja... | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| — | `sonar-scan` | `sonar-scan` | app-ci-push-mk968 → checkout → detect-language → ja... | unknown | `unclassified` | low | — | workflow shutdown with strategy:  Stop |
| — | `checkout` | `checkout` | app-ci-push-zrbq7 → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-xb45w → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-cxhlt → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-59gl2 → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-zs26t → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-t5lxt → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-q8644 → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-q8vvm → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-4dmtd → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-jtth2 → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-r9rkr → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `app-ci-push-q8r5b` | — | app-ci-push-q8r5b | unknown | `unclassified` | low | — | invalid spec: templates.app-ci.tasks.checkout templates.checkout inputs.param... |
| — | `app-ci-push-lkn6j` | — | app-ci-push-lkn6j | unknown | `unclassified` | low | — | invalid spec: templates.app-ci.tasks.checkout templates.checkout inputs.param... |
| — | `checkout` | `checkout` | app-ci-push-gg2hd → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-hkzvv → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-j76ff → checkout | application | `exit_nonzero` | medium | 128 | main: Error (exit code 128) |
| — | `checkout` | `checkout` | app-ci-push-hgz5b → checkout | application | `permission_denied` | high | — | task 'app-ci-push-hgz5b.checkout' errored: pods "app-ci-push-hgz5b-checkout-1... |
| — | `checkout` | `checkout` | app-ci-push-hd4pw → checkout | application | `permission_denied` | high | — | task 'app-ci-push-hd4pw.checkout' errored: pods "app-ci-push-hd4pw-checkout-3... |
| — | `app-ci-push-sjmqk` | — | app-ci-push-sjmqk | unknown | `unclassified` | low | — | invalid spec: templates.app-ci.tasks.go-pipeline templates.go-pipeline.tasks.... |
| — | `app-ci-push-xqdjf` | — | app-ci-push-xqdjf | unknown | `unclassified` | low | — | invalid spec: templates.app-ci.tasks.go-pipeline templates.go-pipeline.inputs... |
| — | `app-ci-push-w8crb` | — | app-ci-push-w8crb | unknown | `unclassified` | low | — | invalid spec: templates.app-ci.tasks.go-pipeline templates.go-pipeline.inputs... |
| `ci-java` | `code-quality` | `code-quality` | ci-java-t7mmz → build → clone-repo → code-quality | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| `ci-java` | `code-quality` | `code-quality` | ci-java-jwc5q → build → clone-repo → code-quality | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| `ci-java` | `code-quality` | `code-quality` | ci-java-9s5gw → build → clone-repo → code-quality | application | `exit_nonzero` | medium | 2 | main: Error (exit code 2) |
| `ci-java` | `code-quality` | `code-quality` | ci-java-44bcj → build → clone-repo → code-quality | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |
| `ci-java` | `code-quality` | `code-quality` | ci-java-4xk9n → build → clone-repo → code-quality | unknown | `unclassified` | low | — | build-truststore: Error (exit code 2) |
| `ci-java` | `code-quality` | `code-quality` | ci-java-pxz9t → build → clone-repo → code-quality | application | `exit_nonzero` | medium | 1 | main: Error (exit code 1) |

---
*Report generated by argo-analyzer on Tue, 31 Mar 2026 23:54:46 EDT*
