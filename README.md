# KubeAssist

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.25+-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![Chart Version](https://img.shields.io/badge/Helm_Chart-v1.8.1-0F1689?style=flat&logo=helm)](charts/kube-assist)
[![Tests](https://img.shields.io/badge/Tests-400+_passing-success?style=flat)](https://github.com/osagberg/kube-assist-operator/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/osagberg/kube-assist-operator/actions/workflows/test.yml/badge.svg)](https://github.com/osagberg/kube-assist-operator/actions/workflows/test.yml)

**Kubernetes diagnostics that tell you *why* things break and *how* to fix them.**

Deploy the operator, get instant visibility into workload failures, certificate expiration, resource quotas, Flux GitOps status, and more. Every issue comes with copy-able `kubectl` commands, root cause analysis, and optional AI-enhanced suggestions -- all from a single binary with zero external dependencies.

![KubeAssist Dashboard](docs/dashboard-overview.png)
*Full dashboard showing health ring, severity pills, health history chart, collapsible causal analysis with AI-enhanced groups, and pipeline progress indicator.*

---

## Why KubeAssist?

Most monitoring tools tell you *what* is broken. KubeAssist tells you *why* and *how to fix it*.

- **Zero-config value** -- deploy the operator and get immediate health insights with no setup required
- **8 built-in + custom plugin checkers** -- full-stack coverage across workloads, secrets, storage, quotas, network policies, and Flux GitOps, plus user-defined CEL-based checks via `CheckPlugin` CRD
- **Actionable remediation** -- every issue includes copy-able `kubectl` commands, root causes, and links to upstream documentation
- **Causal analysis engine** -- 4 cross-checker rules with confidence scoring, temporal correlation, and resource graph ownership chains to surface the *real* root cause
- **AI-enhanced diagnostics** -- optional Anthropic (Claude) or OpenAI integration for context-aware root cause analysis, configurable at runtime from the dashboard, with "Explain This Cluster" narrative mode
- **Frosted glass dashboard** -- modern React 19 SPA with dark/light themes, severity pills for colorblind accessibility, collapsible sections, and a pipeline progress indicator
- **GitOps-native** -- first-class Flux CD integration with graceful degradation when Flux is not installed
- **Predictive health** -- linear regression trend analysis on health history with projected scores, velocity detection, and risky checker identification
- **Enterprise patterns** -- DataSource abstraction, pluggable notifiers, webhook validation, TTL cleanup, leader election
- **Single binary** -- dashboard, API, operator, and CLI all compile into one Go binary (~22K lines of code: 20K Go + 2K TypeScript)

---

## Screenshots

<table>
<tr>
<td width="50%">

**Dashboard Overview**

![Dashboard Overview](docs/dashboard-overview.png)

Health ring at 19%, severity pills (OK 15, CR 6, WR 28, IN 28), health history chart, collapsible causal analysis with AI-enhanced groups, and the pipeline progress indicator showing checker/causal/AI stages.

</td>
<td width="50%">

**AI Settings**

![AI Settings Modal](docs/dashboard-ai-settings.png)

AI Settings modal with provider selection (Anthropic, OpenAI, NoOp), API key input, model picker, and "Provider ready" indicator. Saving settings immediately triggers a new analysis cycle.

</td>
</tr>
<tr>
<td colspan="2">

**Issue List**

![Issue List](docs/dashboard-issues.png)

Sticky filter bar with severity tabs, workload checker cards with individual issues, severity pills (CR/WR/IN), AI badges, root causes, suggestions, and copy-able kubectl commands. Line-clamped suggestions expand on click.

</td>
</tr>
</table>

---

## Quick Start

### Install the CLI

```bash
# Clone and build
git clone https://github.com/osagberg/kube-assist-operator.git
cd kube-assist-operator
make install-cli

# Or build manually
go build -o /usr/local/bin/kubeassist ./cmd/kubeassist
```

### Run Diagnostics

```bash
# Diagnose all workloads across all namespaces
kubeassist

# Run comprehensive health checks
kubeassist health

# Check specific namespaces
kubeassist health -n production,staging

# Output as JSON for CI/CD pipelines
kubeassist health -o json
```

### Deploy the Operator

```bash
# Using Helm (recommended)
helm install kube-assist charts/kube-assist \
  --namespace kube-assist-system \
  --create-namespace \
  --set dashboard.enabled=true

# Using Kustomize
make deploy IMG=ghcr.io/osagberg/kube-assist-operator:v1.8.1
```

---

## CLI Reference

### `kubeassist` -- Workload Diagnostics

Scans Deployments, StatefulSets, DaemonSets, and Pods for issues.

```bash
kubeassist [namespace] [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--all-namespaces` | `-A` | `true` | Scan all namespaces |
| `--selector` | `-l` | -- | Label selector (e.g., `app=api`) |
| `--output` | `-o` | `text` | Output format: `text` or `json` |
| `--watch` | `-w` | `false` | Continuous monitoring mode |
| `--workers` | -- | `5` | Parallel diagnostic workers |
| `--timeout` | -- | `60s` | Timeout per diagnostic |
| `--cleanup` | -- | `true` | Delete CRs after displaying results |

**Examples:**

```bash
# Diagnose a specific namespace
kubeassist production

# Filter by label
kubeassist -l app=frontend

# Watch mode -- continuous monitoring
kubeassist -w

# JSON output for scripting
kubeassist -o json | jq '.issues[] | select(.severity == "Critical")'
```

### `kubeassist health` -- Comprehensive Health Checks

Runs all 8 checkers across specified namespaces.

```bash
kubeassist health [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespaces` | `-n` | current | Comma-separated namespace list |
| `--namespace-selector` | -- | -- | Label selector for namespaces |
| `--checks` | -- | all | Comma-separated checker names |
| `--output` | `-o` | `text` | Output format: `text` or `json` |
| `--timeout` | -- | `120s` | Total check timeout |
| `--cleanup` | -- | `true` | Delete CR after displaying results |

**Examples:**

```bash
# Check current namespace
kubeassist health

# Check multiple namespaces
kubeassist health -n frontend,backend,database

# Check namespaces by label
kubeassist health --namespace-selector team=platform

# Run specific checkers only
kubeassist health --checks workloads,secrets,helmreleases

# JSON output
kubeassist health -o json > health-report.json
```

---

## Health Checkers

KubeAssist includes 8 built-in checkers that detect common issues:

### Workloads (`workloads`)

Checks Deployments, StatefulSets, DaemonSets, and Pods.

| Issue | Severity | Description |
|-------|----------|-------------|
| CrashLoopBackOff | Critical | Container repeatedly crashing |
| ImagePullBackOff | Critical | Cannot pull container image |
| OOMKilled | Critical | Container killed due to memory limit |
| CreateContainerConfigError | Critical | Invalid container configuration |
| Pending (Unschedulable) | Critical | Pod cannot be scheduled |
| High Restart Count | Warning | Container restarted >3 times (configurable) |
| No Resource Limits | Warning | Missing CPU/memory limits |
| No Liveness Probe | Info | Missing liveness probe |
| No Readiness Probe | Info | Missing readiness probe |

### Secrets (`secrets`)

Checks TLS certificates in Kubernetes Secrets.

| Issue | Severity | Description |
|-------|----------|-------------|
| Certificate Expired | Critical | TLS cert has expired |
| Certificate Expiring | Warning | Cert expires within 30 days (configurable) |
| Empty Secret | Warning | Secret has no data |

### PVCs (`pvcs`)

Checks PersistentVolumeClaims.

| Issue | Severity | Description |
|-------|----------|-------------|
| PVC Lost | Critical | PVC in Lost state |
| PVC Pending | Warning | PVC waiting to be bound |
| High Capacity Usage | Warning | >85% capacity used (configurable) |

### Quotas (`quotas`)

Checks ResourceQuotas.

| Issue | Severity | Description |
|-------|----------|-------------|
| Quota Exceeded | Critical | Resource usage over quota |
| Quota Near Limit | Warning | >80% of quota used (configurable) |

### Network Policies (`networkpolicies`)

Checks NetworkPolicy coverage.

| Issue | Severity | Description |
|-------|----------|-------------|
| No NetworkPolicy | Info | Namespace has no network policies |
| Overly Permissive | Info | Policy allows all ingress/egress |

### HelmReleases (`helmreleases`)

Checks Flux HelmRelease resources.

| Issue | Severity | Description |
|-------|----------|-------------|
| Release Failed | Critical | Helm upgrade/install failed |
| Stale Reconciliation | Warning | Not reconciled in >1 hour |
| Suspended | Info | Release is suspended |

### Kustomizations (`kustomizations`)

Checks Flux Kustomization resources.

| Issue | Severity | Description |
|-------|----------|-------------|
| Build Failed | Critical | Kustomize build failed |
| Stale Reconciliation | Warning | Not reconciled in >1 hour |
| Suspended | Info | Kustomization is suspended |

### GitRepositories (`gitrepositories`)

Checks Flux GitRepository resources.

| Issue | Severity | Description |
|-------|----------|-------------|
| Clone Failed | Critical | Cannot clone repository |
| Auth Failed | Critical | Authentication failure |
| Stale Fetch | Warning | Not fetched in >1 hour |
| Suspended | Info | Repository is suspended |

---

## Custom Resources

### TroubleshootRequest

On-demand diagnostics for a specific workload.

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TroubleshootRequest
metadata:
  name: diagnose-my-app
  namespace: production
spec:
  target:
    kind: Deployment          # Deployment, StatefulSet, DaemonSet, Pod, ReplicaSet
    name: my-app
  actions:
    - diagnose                # Analyze pod status and errors
    - logs                    # Collect container logs
    - events                  # Collect related events
    - all                     # All of the above
  tailLines: 100              # Log lines to collect (default: 100)
  ttlSecondsAfterFinished: 300  # Auto-delete after 5 min (optional)
```

**Status fields:**

| Field | Description |
|-------|-------------|
| `phase` | Pending, Running, Completed, or Failed |
| `issues` | List of detected issues with severity and suggestions |
| `logsConfigMap` | ConfigMap containing collected logs |
| `eventsConfigMap` | ConfigMap containing related events |
| `completedAt` | Timestamp when the request completed or failed |

### TeamHealthRequest

Comprehensive health check across namespaces.

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TeamHealthRequest
metadata:
  name: platform-health
spec:
  scope:
    # Option 1: Explicit namespace list
    namespaces:
      - frontend
      - backend
      - database

    # Option 2: Label selector
    # namespaceSelector:
    #   matchLabels:
    #     team: platform

    # Option 3: Current namespace only
    # currentNamespaceOnly: true

  checks:                     # Empty = all checkers
    - workloads
    - secrets
    - helmreleases

  ttlSecondsAfterFinished: 600  # Auto-delete after 10 min (optional)

  config:                     # Per-checker configuration
    workloads:
      restartThreshold: 3
      includeJobs: false
    secrets:
      checkCertExpiry: true
      certExpiryWarningDays: 30
    quotas:
      usageWarningPercent: 80
    pvcs:
      capacityWarningPercent: 85
```

**Status fields:**

| Field | Description |
|-------|-------------|
| `phase` | Pending, Running, Completed, or Failed |
| `results` | Per-checker results with healthy count and issues |
| `namespacesChecked` | List of namespaces that were checked |
| `lastCheckTime` | Timestamp of last check |
| `completedAt` | Timestamp when the request completed or failed |

---

## Dashboard

The operator includes a real-time dashboard built with React 19, Vite, TypeScript, and Tailwind CSS, embedded in the Go binary via `go:embed`. No separate frontend deployment needed.

### Frosted Glass UI

The dashboard uses a frosted glass design language with translucent panels, blur effects, and smooth transitions across dark and light themes.

![Dashboard Overview](docs/dashboard-overview.png)

**Key interface elements:**

- **Health ring** -- animated SVG ring showing overall cluster health percentage
- **Severity pills** -- color-coded labels (CR/WR/IN/OK) replacing color-only indicators, designed for colorblind accessibility
- **Health history chart** -- line chart visualization powered by recharts showing health score over time
- **Pipeline progress indicator** -- visual `[Checkers] -> [Causal] -> [AI]` display showing which stage of analysis is currently running
- **Collapsible sections** -- Causal Timeline, Causal Groups, and Checker Cards all collapse to keep the view manageable
- **Sticky filter bar** -- stays pinned below the header during scroll with severity tabs, namespace/checker dropdowns, and text search
- **Connection indicator** -- green/yellow/red dot showing SSE connection status
- **Mobile hamburger menu** -- responsive layout with collapsed navigation at <768px viewport width
- **Line-clamp suggestions** -- issue suggestions are truncated in list view; click to expand

### Enable Dashboard

```bash
# Local development
make run ARGS="--enable-dashboard"

# Helm installation
helm install kube-assist charts/kube-assist \
  --set dashboard.enabled=true

# Access (OrbStack users)
open http://kube-assist-dashboard.kube-assist-system:9090

# Access (standard Kubernetes)
kubectl port-forward -n kube-assist-system svc/kube-assist-dashboard 9090:9090
open http://localhost:9090
```

If `dashboard.authToken` is configured, TLS must also be configured (`dashboard.tls.enabled=true` + `dashboard.tls.secretName`) unless you explicitly set `dashboard.allowInsecureHttp=true` for local development.

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Dashboard UI |
| `/api/health` | GET | Current health data (JSON) |
| `/api/events` | GET | Real-time SSE stream |
| `/api/check` | POST | Trigger immediate health check (Bearer token required when configured) |
| `/api/settings/ai` | GET | Current AI configuration (API key masked) |
| `/api/settings/ai` | POST | Update AI provider/model/key at runtime (Bearer token required when configured) |
| `/api/health/history` | GET | Health score history (`?last=N`, `?since=RFC3339`) |
| `/api/causal/groups` | GET | Causal correlation analysis (correlated issue groups) |
| `/api/explain` | GET | AI-generated cluster health narrative with risk level and top issues (Bearer token required when configured) |
| `/api/prediction/trend` | GET | Predictive health trend analysis (direction, velocity, projection) |

### Live Updates

The dashboard uses Server-Sent Events (SSE) for real-time updates with automatic reconnection and pause/resume support. Toast notifications provide visual feedback for user actions and background events. Export reports as JSON or CSV at any time.

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `/` | Focus search |
| `f` | Focus namespace filter |
| `t` | Toggle theme |
| `p` | Pause/resume updates |
| `r` | Refresh data |
| `1-4` | Filter by severity (All/Critical/Warning/Info) |
| `?` | Show keyboard shortcuts |
| `Esc` | Close modal / blur input |

---

## AI Integration

KubeAssist enhances health check results with AI-generated root cause analysis and remediation guidance. Three providers are supported:

| Provider | Default Model | Description |
|----------|---------------|-------------|
| **Anthropic** | Claude Haiku 4.5 | Context-aware diagnostics with structured output and prompt caching |
| **OpenAI** | GPT-4o-mini | Cost-optimized provider (~97% cheaper than GPT-4o) with broad model selection |
| **NoOp** | -- | Returns empty suggestions; useful for testing and development |

### Quick Setup (Dashboard)

The fastest way to enable AI is through the dashboard:

1. Click the gear icon to open AI Settings
2. Toggle AI on, select a provider, enter your API key, and pick a model
3. Click Save -- the operator immediately triggers a new analysis cycle with AI enabled (no waiting for the next scheduled check)

![AI Settings](docs/dashboard-ai-settings.png)
*AI Settings modal showing Anthropic provider with "Provider ready" indicator.*

### Quick Setup (CLI)

```bash
make run ARGS="--enable-dashboard --enable-ai --ai-provider=anthropic --ai-api-key=sk-ant-..."
```

### Quick Setup (Helm)

```yaml
ai:
  enabled: true
  provider: "anthropic"
  apiKeySecretRef:
    name: "kube-assist-ai-secret"
    key: "api-key"
```

### How AI Analysis Works

1. **Batched calls** -- Issues are grouped and sent to the AI provider in a single batched call to minimize API overhead
2. **LRU response cache** -- 100-entry cache with 5-minute TTL keyed by issue signature hash; repeated checks for the same issue pattern cost nothing
3. **Severity gating** -- Info-level issues are skipped by AI (keep static suggestions), reducing token usage ~30%
4. **Cross-issue deduplication** -- Identical issue patterns (after normalizing pod hashes) are grouped; only one representative is sent to AI, results fanned out to all duplicates
5. **Token budget** -- Configurable daily and monthly token limits with automatic window reset; `ErrBudgetExceeded` returned gracefully when caps are hit
6. **Tiered model routing** -- Separate models for analyze vs explain mode (e.g., fast model for issue analysis, powerful model for cluster explanations)
7. **Anthropic prompt caching** -- System prompts sent with `cache_control: {type: "ephemeral"}` for ~90% cost reduction on repeated calls
8. **Data sanitization** -- Sensitive data (secrets, environment variables, API keys) is redacted before being sent to any AI provider
9. **Thread-safe AI Manager** -- `Reconfigure()` swaps providers at runtime without downtime; the same manager instance is shared across the dashboard and controllers
10. **Pipeline indicator** -- The dashboard shows a `[Checkers] -> [Causal] -> [AI]` progress bar so you can see exactly when AI analysis is running

For full details on providers, data sanitization, API key management, and cost considerations, see the [AI Integration Guide](docs/ai-integration.md).

---

## Causal Analysis

The causal analysis engine correlates issues across checkers to identify root causes that no single checker can detect on its own. It combines temporal correlation, resource graph ownership chains, and predefined cross-checker rules to surface the *real* reason your workloads are failing.

### Cross-Checker Rules

| Rule | Confidence | Description |
|------|------------|-------------|
| **OOM + Quota** | 0.85 | An OOMKilled container in a namespace with quota near its limit suggests the quota is the underlying constraint |
| **Crash + ImagePull** | 0.80 | CrashLoopBackOff alongside ImagePullBackOff often indicates a registry or image configuration issue rather than an application bug |
| **Flux Chain** | 0.90 | A failed GitRepository causes downstream Kustomization and HelmRelease failures; the engine traces the chain back to the source |
| **PVC + Workload** | 0.75 | A pending PVC bound to a workload explains why pods are stuck in Pending state |

### How It Works

1. **Temporal correlation** -- Issues that appear within the same time window are grouped as potentially related
2. **Resource graph** -- Kubernetes owner references are walked to connect pods, deployments, replica sets, and other resources into ownership chains
3. **Rule matching** -- The 4 cross-checker rules above are evaluated against grouped issues to identify known causal patterns
4. **AI enhancement** -- When AI is enabled, causal groups are enriched with AI-generated explanations that tie the correlated issues together into a coherent narrative
5. **Confidence scoring** -- Each causal group carries a confidence score so you can prioritize investigation

Causal groups appear in the dashboard as collapsible cards in the Causal Analysis section, showing the rule that matched, the confidence score, and the root cause. The `/api/causal/groups` endpoint returns the same data as JSON.

---

## Helm Installation

### Basic Installation

```bash
helm install kube-assist charts/kube-assist \
  --namespace kube-assist-system \
  --create-namespace
```

### With Dashboard

```bash
helm install kube-assist charts/kube-assist \
  --namespace kube-assist-system \
  --create-namespace \
  --set dashboard.enabled=true
```

### Configuration Values

| Parameter | Default | Description |
|-----------|---------|-------------|
| `replicaCount` | `1` | Number of operator replicas |
| `image.repository` | `ghcr.io/osagberg/kube-assist-operator` | Image repository |
| `image.tag` | Chart appVersion | Image tag |
| `dashboard.enabled` | `false` | Enable web dashboard |
| `dashboard.bindAddress` | `:9090` | Dashboard listen address |
| `dashboard.service.type` | `ClusterIP` | Dashboard service type |
| `dashboard.service.port` | `9090` | Dashboard service port |
| `operator.leaderElection.enabled` | `true` | Enable leader election |
| `operator.metricsBindAddress` | `:8080` | Metrics endpoint |
| `resources.requests.cpu` | `10m` | CPU request |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `128Mi` | Memory limit |
| `ai.enabled` | `false` | Enable AI-powered suggestions |
| `ai.provider` | `noop` | AI provider: anthropic, openai, noop |
| `ai.model` | (provider default) | AI model to use |
| `ai.explainModel` | `""` | Separate model for "Explain This Cluster" mode (uses primary model if empty) |
| `ai.apiKeySecretRef.name` | -- | Secret containing API key |
| `ai.budget.dailyTokenLimit` | `0` | Daily token budget (0 = unlimited) |
| `ai.budget.monthlyTokenLimit` | `0` | Monthly token budget (0 = unlimited) |
| `dashboard.authToken` | `""` | Bearer token for authenticating mutating API requests |
| `dashboard.authTokenSecretRef.name` | `""` | Secret containing auth token |
| `dashboard.allowInsecureHttp` | `false` | Allow auth over HTTP without TLS (local/dev only) |
| `dashboard.tls.enabled` | `false` | Enable TLS for dashboard HTTPS |
| `dashboard.tls.secretName` | `""` | Secret with tls.crt and tls.key |
| `networkPolicy.enabled` | `true` | Enable network policy |
| `networkPolicy.ingressMode` | `permissive` | Ingress mode: permissive or strict |
| `networkPolicy.dnsMode` | `kube-system` | DNS egress: all or kube-system |

### Full Configuration

See [charts/kube-assist/values.yaml](charts/kube-assist/values.yaml) for all options.

---

## Architecture

```
+---------------------------------------------------------------------------+
|                              User Interface                               |
+---------------------------------+-----------------------------------------+
|         CLI (kubeassist)        |         Dashboard (:9090)               |
|  - kubeassist [namespace]       |  - Frosted glass UI, dark/light themes  |
|  - kubeassist health            |  - Real-time SSE updates                |
|  - JSON/text output             |  - AI settings panel (runtime config)   |
|                                 |  - Severity pills + pipeline indicator  |
+---------------------------------+-----------------------------------------+
                                    |
                        +-----------+----------+
                        v                      v
+------------------------------+ +------------------------------------------+
|       AI Manager             | |           Custom Resources                |
|  - Thread-safe Reconfigure() | +--------------------+---------------------+
|  - Shared across dashboard   | | TroubleshootRequest | TeamHealthRequest   |
|    and controllers           | | - Target: any kind  | - Scope: ns/sel     |
|  - POST /api/settings/ai     | | - diagnose/logs/... | - Configurable      |
+------------------------------+ | - issues+ConfigMaps | - Per-checker cfg   |
                        |        +--------------------+---------------------+
                        |                      |
                        +-----------+----------+
                                    v
+---------------------------------------------------------------------------+
|                            Controllers                                    |
+------------------+---------------------+---------------------------------+
| TroubleshootReq  | TeamHealthReq       | CheckPluginReconciler           |
| - Validates tgt  | - Resolves ns scope | - Watches CheckPlugin CRs       |
| - Pod diagnostics| - Runs checkers     | - Compiles CEL expressions      |
| - Stores logs    | - Aggregates results| - Hot-reload registry           |
+------------------+---------------------+---------------------------------+
                                    |
                                    v
+---------------------------------------------------------------------------+
|                          Checker Registry                                 |
+----------+----------+----------+----------+----------+---------+----------+
|workloads | secrets  |   pvcs   |  quotas  |  netpol  | Flux(3) | Plugins  |
|          |          |          |          |          | helmrel | CEL CRD  |
|Deployment| TLS cert | Pending  | Usage %  | Coverage | kustom  | hot-     |
|StatefulS | expiry   | Lost     | exceeded | rules    | gitrepo | reload   |
+----------+----------+----------+----------+----------+---------+----------+
                                    |
                                    v
+---------------------------------------------------------------------------+
|                       Causal Analysis Engine                              |
|  - Temporal correlation across checker results                            |
|  - Resource graph ownership chains (owner references)                     |
|  - 4 cross-checker rules with confidence scoring                          |
|  - AI-enhanced group explanations when AI is enabled                      |
+---------------------------------------------------------------------------+
                                    |
                                    v
+---------------------------------------------------------------------------+
|                       Predictive Health Engine                             |
|  - OLS linear regression on health score history                          |
|  - Velocity detection (score change per hour)                             |
|  - Projected score with 95% confidence interval                           |
|  - Risky checker + severity trajectory identification                     |
+---------------------------------------------------------------------------+
                                    |
                                    v
+---------------------------------------------------------------------------+
|                      DataSource Abstraction                               |
|  - KubernetesDataSource (default) -- direct K8s API calls                 |
|  - Pluggable interface -- swap in enterprise cache or multi-cluster       |
|  - Scope resolver -- namespace filtering, label selectors, 50-ns cap     |
+---------------------------------------------------------------------------+
|                         Kubernetes API                                    |
|  Deployments - StatefulSets - DaemonSets - Pods - Events - Secrets       |
|  PVCs - ResourceQuotas - NetworkPolicies - HelmReleases - Kustomizations |
+---------------------------------------------------------------------------+
```

---

## Metrics

Prometheus metrics available at `:8080/metrics`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubeassist_reconcile_total` | Counter | name, namespace, result | Total reconciliations |
| `kubeassist_reconcile_duration_seconds` | Histogram | name, namespace | Reconciliation duration |
| `kubeassist_issues_total` | Gauge | namespace, severity | Issues by severity |
| `kubeassist_ai_calls_total` | Counter | provider, mode, result | AI API calls |
| `kubeassist_ai_tokens_used_total` | Counter | provider, mode | Tokens consumed |
| `kubeassist_ai_call_duration_seconds` | Histogram | provider, mode | AI call latency |
| `kubeassist_ai_cache_hits_total` | Counter | -- | Response cache hits |
| `kubeassist_ai_cache_misses_total` | Counter | -- | Response cache misses |
| `kubeassist_ai_cache_size` | Gauge | -- | Current cache entries |
| `kubeassist_ai_budget_exceeded_total` | Counter | window | Budget limit rejections |
| `kubeassist_ai_budget_tokens_used` | Gauge | window | Current token usage per window |
| `kubeassist_ai_budget_tokens_limit` | Gauge | window | Token limit per window |
| `kubeassist_ai_issues_filtered_total` | Counter | reason | Issues skipped (severity_info, duplicate) |

---

## CI/CD Pipeline

Four GitHub Actions workflows enforce quality at every stage:

| Workflow | File | Trigger | What It Does |
|----------|------|---------|--------------|
| **Lint** | `lint.yml` | Push/PR to `main` | golangci-lint with 21 linters (staticcheck, gosec, errcheck, govet, ineffassign, misspell, and more) |
| **Tests** | `test.yml` | Push/PR to `main` | Unit and integration tests, govulncheck, build verification |
| **E2E Tests** | `test-e2e.yml` | Push/PR to `main` | End-to-end tests against a real cluster environment |
| **Release** | `release.yml` | Tag `v*` | Multi-arch Docker build, GHCR push, Trivy container scan, GitHub Release |

**Container security**: Release images are built distroless (`gcr.io/distroless/static:nonroot`), scanned with Trivy, and published to GHCR with OCI labels. Dependabot keeps Go modules and GitHub Actions dependencies up to date.

---

## Development

```bash
# Run tests (400+ test cases)
make test

# Run operator locally
make run

# Run with dashboard
make run ARGS="--enable-dashboard"

# Build container image
make docker-build IMG=ghcr.io/osagberg/kube-assist-operator:dev

# Generate CRD manifests
make manifests

# Install CRDs
make install

# Build CLI
make install-cli
```

---

## Documentation

- [AI Integration Guide](docs/ai-integration.md) -- Configure AI-powered suggestions (including runtime dashboard config)
- [Troubleshooting Guide](docs/troubleshooting.md) -- Common issues and solutions

---

## Roadmap

- [x] AI-powered suggestions (v1.3.0)
- [x] Runtime AI configuration via dashboard (v1.4.0)
- [x] Copy-able kubectl remediation commands (v1.4.0)
- [x] Full E2E test coverage for controllers (v1.4.0)
- [x] TTL auto-cleanup for completed CRs (v1.5.0)
- [x] Validating admission webhooks (v1.5.0)
- [x] Test helper utilities and reduced boilerplate (v1.5.0)
- [x] DataSource abstraction for pluggable backends (v1.5.0)
- [x] Webhook notification interface (`spec.notify` on TeamHealthRequest) (v1.5.1)
- [x] Health score history with ring buffer (`/api/health/history`) (v1.5.1)
- [x] React dashboard -- React 19 + Vite + TypeScript + Tailwind SPA (v1.6.0)
- [x] Causal analysis engine -- temporal correlation, resource graph, cross-checker rules, AI-enhanced context (v1.7.0)
- [x] Frosted glass dashboard redesign -- dark/light themes, severity pills, pipeline indicator, collapsible causal timeline, instant AI trigger (v1.7.1)
- [x] Custom checker plugins — CRD-driven health checks with CEL expressions (`CheckPlugin` CR, hot-reload registry) (v1.8.0)
- [x] "Explain this cluster" AI mode — narrative summary of cluster health with risk level, top issues, and trend direction (v1.8.0)
- [x] Predictive health — trend analysis via linear regression on health history, score projection, risky checker detection (v1.8.0)
- [x] AI cost optimization — 9 optimizations (severity gating, change-only AI, gpt-4o-mini, prompt caching, token budget, batching, dedup, tiered routing, LRU cache) + 11 Prometheus metrics (v1.8.1-dev)
- [ ] Cross-cluster via ConsoleDataSource — multi-cluster aggregation through pluggable `DataSource` interface

---

## License

Apache License 2.0 -- see [LICENSE](LICENSE) for details.
