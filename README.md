# KubeAssist Operator

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Kubernetes operator for workload diagnostics and cluster health monitoring. One command to diagnose your entire cluster.

## Features

- **Two CRDs**: TroubleshootRequest for workload diagnostics, TeamHealthRequest for comprehensive health checks
- **8 Checkers**: Workloads, Secrets, PVCs, Quotas, NetworkPolicies, HelmReleases, Kustomizations, GitRepositories
- **CLI Tool**: `kubeassist` for instant diagnostics, `kubeassist health` for full health reports
- **Web Dashboard**: Real-time updates via Server-Sent Events (SSE) on port 9090
- **Flux Integration**: Native support for HelmRelease, Kustomization, and GitRepository resources
- **Actionable Output**: Issues include severity levels and fix suggestions

## Quick Start

### Install CLI

```sh
git clone https://github.com/osagberg/kube-assist-operator.git
cd kube-assist-operator
make install-cli
```

### Run Diagnostics

```sh
kubeassist                    # Diagnose all workloads
kubeassist health             # Run all health checkers
```

### Deploy Operator

```sh
# Option 1: Helm
helm install kube-assist charts/kube-assist -n kube-assist --create-namespace

# Option 2: Kustomize
make deploy IMG=ghcr.io/osagberg/kube-assist-operator:latest
```

## CLI Usage

### Workload Diagnostics

```sh
kubeassist                    # All namespaces (default)
kubeassist production         # Specific namespace
kubeassist -l app=api         # Filter by label
kubeassist -o json            # JSON output
kubeassist -w                 # Watch mode
```

| Flag | Description | Default |
|------|-------------|---------|
| `-A`, `--all-namespaces` | Scan all namespaces | `true` |
| `-l`, `--selector` | Label selector | - |
| `-o`, `--output` | Format: `text`, `json` | `text` |
| `-w`, `--watch` | Continuous monitoring | `false` |
| `--workers` | Parallel workers | `5` |
| `--timeout` | Diagnostic timeout | `60s` |
| `--cleanup` | Delete requests after scan | `true` |

### Health Checks

```sh
kubeassist health                              # Current namespace
kubeassist health -n frontend,backend          # Specific namespaces
kubeassist health --namespace-selector team=x  # By label
kubeassist health --checks workloads,secrets   # Specific checkers
kubeassist health -o json                      # JSON output
```

| Flag | Description | Default |
|------|-------------|---------|
| `-n`, `--namespaces` | Comma-separated namespaces | current |
| `--namespace-selector` | Label selector for namespaces | - |
| `--checks` | Comma-separated checker names | all |
| `-o`, `--output` | Format: `text`, `json` | `text` |
| `--timeout` | Check timeout | `120s` |
| `--cleanup` | Delete request after display | `true` |

## Checkers

| Checker | Resources | Issues Detected |
|---------|-----------|-----------------|
| `workloads` | Deployments, StatefulSets, DaemonSets | CrashLoopBackOff, OOMKilled, ImagePullBackOff, missing limits/probes |
| `secrets` | Secrets (TLS) | Expired/expiring certificates, empty secrets |
| `pvcs` | PersistentVolumeClaims | Pending, Lost state |
| `quotas` | ResourceQuotas | Usage exceeding thresholds |
| `networkpolicies` | NetworkPolicies | Missing policies, overly permissive rules |
| `helmreleases` | Flux HelmReleases | Failed upgrades, stale reconciliation |
| `kustomizations` | Flux Kustomizations | Build failures, stale reconciliation |
| `gitrepositories` | Flux GitRepositories | Clone/auth failures, stale fetch |

## CRDs

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
    kind: Deployment    # Deployment, StatefulSet, DaemonSet, Pod, ReplicaSet
    name: my-app
  actions:
    - diagnose          # Analyze pod status
    - logs              # Collect container logs
    - events            # Collect related events
    - all               # Everything above
  tailLines: 100        # Log lines to collect (default: 100)
```

### TeamHealthRequest

Comprehensive health check across namespaces.

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TeamHealthRequest
metadata:
  name: platform-health
spec:
  scope:
    namespaces:         # Option 1: Explicit list
      - frontend
      - backend
    # namespaceSelector:  # Option 2: Label selector
    #   matchLabels:
    #     team: platform
    # currentNamespaceOnly: true  # Option 3: Current namespace
  checks:               # Empty = all checkers
    - workloads
    - secrets
    - helmreleases
  config:
    workloads:
      restartThreshold: 3
    secrets:
      certExpiryWarningDays: 30
    quotas:
      usageWarningPercent: 80
```

## Dashboard

The operator includes a web dashboard with real-time SSE updates.

**Enable:**
```sh
# Local development
make run ARGS="--enable-dashboard"

# Helm
helm install kube-assist charts/kube-assist --set dashboard.enabled=true
```

**Endpoints:**
- `http://localhost:9090/` - Dashboard UI
- `http://localhost:9090/api/health` - Health data (JSON)
- `http://localhost:9090/api/events` - SSE stream

## Architecture

```
                              +-----------------------+
                              |      CLI / API        |
                              | kubeassist [health]   |
                              +-----------+-----------+
                                          |
                    +---------------------+---------------------+
                    |                                           |
          +---------v---------+                     +-----------v-----------+
          | TroubleshootRequest|                     |  TeamHealthRequest    |
          +--------+----------+                     +-----------+-----------+
                   |                                            |
          +--------v----------+                     +-----------v-----------+
          |    Controller     |                     |      Controller       |
          +--------+----------+                     +-----------+-----------+
                   |                                            |
          +--------v----------+                     +-----------v-----------+
          |  Pod Diagnostics  |                     |   Checker Registry    |
          | - Status          |                     +-----------+-----------+
          | - Logs            |                                 |
          | - Events          |         +----------+------------+----------+
          +-------------------+         |          |            |          |
                                        v          v            v          v
                                   workloads   secrets      flux/*    resource/*

                              +---------------------------------------------+
                              |               Dashboard (9090)              |
                              |          Real-time SSE Updates              |
                              +---------------------------------------------+
```

## Detected Issues

| Issue | Severity | Checker |
|-------|----------|---------|
| CrashLoopBackOff | Critical | workloads |
| ImagePullBackOff | Critical | workloads |
| OOMKilled | Critical | workloads |
| CreateContainerConfigError | Critical | workloads |
| Pending (Unschedulable) | Critical | workloads |
| High Restart Count | Warning | workloads |
| No Memory/CPU Limits | Warning | workloads |
| No Liveness/Readiness Probe | Info | workloads |
| Certificate Expired | Critical | secrets |
| Certificate Expiring Soon | Warning | secrets |
| PVC Pending/Lost | Warning/Critical | pvcs |
| Quota Exceeded/Near Limit | Critical/Warning | quotas |
| No NetworkPolicy | Info | networkpolicies |
| HelmRelease Failed | Critical | helmreleases |
| Kustomization Build Failed | Critical | kustomizations |
| GitRepository Auth Failed | Critical | gitrepositories |

## Metrics

Prometheus metrics available at `:8080/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `kubeassist_reconcile_total` | Counter | Reconciliations by name, namespace, result |
| `kubeassist_reconcile_duration_seconds` | Histogram | Reconciliation duration |
| `kubeassist_issues_total` | Gauge | Issues by namespace and severity |

## Development

```sh
make test                    # Run tests
make run                     # Run locally
make run ARGS="--enable-dashboard"  # With dashboard
make docker-build IMG=...    # Build image
make demo-up                 # Deploy demo workloads
make demo-down               # Clean up demo
```

## License

Apache License 2.0
