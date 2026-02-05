# KubeAssist

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.25+-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Kubernetes operator for workload diagnostics and cluster health monitoring.**

One command to diagnose your entire cluster. KubeAssist provides instant visibility into workload issues, certificate expiration, resource quotas, Flux GitOps status, and more — with actionable suggestions to fix problems.

![Dashboard Screenshot](docs/dashboard-screenshot.png)

---

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [CLI Reference](#cli-reference)
- [Health Checkers](#health-checkers)
- [Custom Resources](#custom-resources)
- [Dashboard](#dashboard)
- [Helm Installation](#helm-installation)
- [Architecture](#architecture)
- [Development](#development)
- [License](#license)

---

## Features

### Instant Diagnostics
- **8 built-in checkers** covering workloads, secrets, storage, quotas, network policies, and Flux GitOps
- **Severity levels** (Critical/Warning/Info) with actionable fix suggestions
- **CLI and CRD interfaces** — use whichever fits your workflow

### Real-Time Dashboard
- **Live updates** via Server-Sent Events (SSE)
- **Dark/Light themes** with persistent preference
- **Search & filter** by namespace, checker, severity
- **Export** reports as JSON or CSV
- **Keyboard shortcuts** for power users
- **Health score** visualization with animated progress ring

### GitOps Native
- **Flux integration** for HelmReleases, Kustomizations, GitRepositories
- **Graceful degradation** — Flux checkers automatically skip if Flux isn't installed
- **Stale reconciliation detection** — catch GitOps pipelines that stopped syncing

### AI-Powered Suggestions
- **Enhanced diagnostics** with AI-generated root cause analysis
- **Provider options** — Anthropic Claude, OpenAI, or NoOp for testing
- **Data sanitization** — sensitive data redacted before AI calls
- **Configurable** via CLI flags, env vars, or Helm values

### Production Ready
- **Leader election** for HA deployments
- **Prometheus metrics** for observability
- **Minimal RBAC** — read-only access to cluster resources
- **Distroless container** — secure, minimal attack surface
- **Network policy** template for restricted egress

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
make deploy IMG=ghcr.io/osagberg/kube-assist-operator:v1.2.0
```

---

## CLI Reference

### `kubeassist` — Workload Diagnostics

Scans Deployments, StatefulSets, DaemonSets, and Pods for issues.

```bash
kubeassist [namespace] [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--all-namespaces` | `-A` | `true` | Scan all namespaces |
| `--selector` | `-l` | — | Label selector (e.g., `app=api`) |
| `--output` | `-o` | `text` | Output format: `text` or `json` |
| `--watch` | `-w` | `false` | Continuous monitoring mode |
| `--workers` | — | `5` | Parallel diagnostic workers |
| `--timeout` | — | `60s` | Timeout per diagnostic |
| `--cleanup` | — | `true` | Delete CRs after displaying results |

**Examples:**

```bash
# Diagnose a specific namespace
kubeassist production

# Filter by label
kubeassist -l app=frontend

# Watch mode — continuous monitoring
kubeassist -w

# JSON output for scripting
kubeassist -o json | jq '.issues[] | select(.severity == "Critical")'
```

### `kubeassist health` — Comprehensive Health Checks

Runs all 8 checkers across specified namespaces.

```bash
kubeassist health [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespaces` | `-n` | current | Comma-separated namespace list |
| `--namespace-selector` | — | — | Label selector for namespaces |
| `--checks` | — | all | Comma-separated checker names |
| `--output` | `-o` | `text` | Output format: `text` or `json` |
| `--timeout` | — | `120s` | Total check timeout |
| `--cleanup` | — | `true` | Delete CR after displaying results |

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
```

**Status fields:**

| Field | Description |
|-------|-------------|
| `phase` | Pending, Running, Completed, or Failed |
| `issues` | List of detected issues with severity and suggestions |
| `logsConfigMap` | ConfigMap containing collected logs |
| `eventsConfigMap` | ConfigMap containing related events |

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

---

## Dashboard

The operator includes a real-time web dashboard with Server-Sent Events (SSE) for live updates.

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

### Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /` | Dashboard UI |
| `GET /api/health` | Current health data (JSON) |
| `GET /api/events` | Real-time SSE stream |
| `POST /api/check` | Trigger immediate health check |

### Features

- **Theme Toggle** — Dark/light mode with localStorage persistence
- **Search** — Filter issues by resource name, namespace, or message
- **Filters** — Namespace dropdown, checker dropdown, severity tabs
- **Export** — Download reports as JSON or CSV
- **Collapsible Cards** — Click checker headers to collapse/expand
- **Health Score** — Animated progress ring showing overall health
- **Timeline** — Visual history of last 5 health checks
- **Keyboard Shortcuts**:

| Key | Action |
|-----|--------|
| `/` | Focus search |
| `j` / `k` | Navigate issues |
| `f` | Focus namespace filter |
| `t` | Toggle theme |
| `p` | Pause/resume updates |
| `r` | Refresh data |
| `1-4` | Filter by severity (All/Critical/Warning/Info) |
| `?` | Show keyboard shortcuts |
| `Esc` | Close modal / blur input |

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
| `operator.leaderElection.enabled` | `true` | Enable leader election |
| `operator.metricsBindAddress` | `:8080` | Metrics endpoint |
| `resources.requests.cpu` | `10m` | CPU request |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `128Mi` | Memory limit |
| `ai.enabled` | `false` | Enable AI-powered suggestions |
| `ai.provider` | `noop` | AI provider: anthropic, openai, noop |
| `ai.apiKeySecretRef.name` | — | Secret containing API key |
| `networkPolicy.enabled` | `false` | Enable network policy |

### Full Configuration

See [charts/kube-assist/values.yaml](charts/kube-assist/values.yaml) for all options.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              User Interface                              │
├─────────────────────────────────┬───────────────────────────────────────┤
│         CLI (kubeassist)        │         Dashboard (:9090)             │
│  • kubeassist [namespace]       │  • Real-time SSE updates              │
│  • kubeassist health            │  • Dark/light themes                  │
│  • JSON/text output             │  • Search, filter, export             │
└─────────────────────────────────┴───────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          Custom Resources                                │
├─────────────────────────────────┬───────────────────────────────────────┤
│       TroubleshootRequest       │        TeamHealthRequest              │
│  • Target: Deployment/Pod/etc   │  • Scope: namespaces/selector         │
│  • Actions: diagnose/logs/etc   │  • Checks: configurable list          │
│  • Output: issues + ConfigMaps  │  • Config: per-checker thresholds     │
└─────────────────────────────────┴───────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                            Controllers                                   │
├─────────────────────────────────┬───────────────────────────────────────┤
│  TroubleshootRequestReconciler  │    TeamHealthRequestReconciler        │
│  • Validates target exists      │    • Resolves namespace scope         │
│  • Collects pod diagnostics     │    • Runs selected checkers           │
│  • Stores logs/events           │    • Aggregates results               │
└─────────────────────────────────┴───────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          Checker Registry                                │
├──────────┬──────────┬──────────┬──────────┬──────────┬─────────────────┤
│workloads │ secrets  │   pvcs   │  quotas  │  netpol  │   Flux (3)      │
│          │          │          │          │          │ • helmreleases  │
│Deployment│ TLS cert │ Pending  │ Usage %  │ Coverage │ • kustomizations│
│StatefulS │ expiry   │ Lost     │ exceeded │ rules    │ • gitrepos      │
│DaemonSet │ empty    │ capacity │          │          │                 │
└──────────┴──────────┴──────────┴──────────┴──────────┴─────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes API                                   │
│  Deployments • StatefulSets • DaemonSets • Pods • Events • Secrets      │
│  PVCs • ResourceQuotas • NetworkPolicies • HelmReleases • Kustomizations│
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Metrics

Prometheus metrics available at `:8080/metrics`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubeassist_reconcile_total` | Counter | name, namespace, result | Total reconciliations |
| `kubeassist_reconcile_duration_seconds` | Histogram | name, namespace | Reconciliation duration |
| `kubeassist_issues_total` | Gauge | namespace, severity | Issues by severity |

---

## Development

```bash
# Run tests
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

- [AI Integration Guide](docs/ai-integration.md) — Configure AI-powered suggestions
- [Troubleshooting Guide](docs/troubleshooting.md) — Common issues and solutions

---

## Roadmap

- [x] AI-powered suggestions
- [ ] Slack/PagerDuty alerting integration
- [ ] Historical trend analysis
- [ ] Custom checker plugins

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
