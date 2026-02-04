# KubeAssist Operator

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**One command to diagnose your entire Kubernetes cluster.**

KubeAssist is a Kubernetes operator that simplifies workload troubleshooting. Instead of running multiple `kubectl` commands and interpreting cryptic error messages, just run `kubeassist` and get instant, actionable diagnostics.

```
$ kubeassist

Summary: 1 healthy, 2 unhealthy (2 critical, 1 warnings)

api-server [production]
  * [Critical] Container app is waiting: CrashLoopBackOff
    -> Application is crashing. Check logs for error messages.
  - [Warning] Container app has restarted 12 times

worker [production]
  * [Critical] Container worker is waiting: ImagePullBackOff
    -> Check if the image exists and credentials are configured.

redis [production]
  All 1 pod(s) healthy - no issues found
```

## Quick Start

### Option 1: CLI Tool (Recommended)

```sh
git clone https://github.com/osagberg/kube-assist-operator.git
cd kube-assist-operator
make install-cli

kubeassist                    # Scan all namespaces
kubeassist production         # Scan specific namespace
kubeassist -l app=api         # Filter by label
kubeassist -o json            # JSON output
```

### Option 2: Helm Chart

```sh
helm install kube-assist charts/kube-assist -n kube-assist --create-namespace
```

### Option 3: Deploy with Kustomize

```sh
make deploy IMG=ghcr.io/osagberg/kube-assist-operator:latest
```

## CLI Reference

| Flag | Description | Default |
|------|-------------|---------|
| `-A`, `--all-namespaces` | Scan all namespaces | `true` |
| `-l`, `--selector` | Label selector to filter workloads | - |
| `-o`, `--output` | Output format: `text` or `json` | `text` |
| `--workers` | Parallel workers for processing | `5` |
| `--cleanup` | Delete TroubleshootRequests after scan | `true` |
| `--timeout` | Timeout for diagnostics | `60s` |
| `-w`, `--watch` | Continuous monitoring mode | `false` |

## Detected Issues

| Issue | Severity | Suggestion |
|-------|----------|------------|
| CrashLoopBackOff | Critical | Check logs for error messages |
| ImagePullBackOff | Critical | Check image exists and credentials configured |
| OOMKilled | Critical | Increase memory limit or optimize usage |
| CreateContainerConfigError | Critical | Check ConfigMaps and Secrets |
| Pending (Unschedulable) | Critical | Check node resources, taints, affinity |
| High Restart Count | Warning | Check logs, consider increasing limits |
| No Memory Limit | Warning | Set limits for fair resource sharing |
| No Resource Requests | Warning | Set requests for proper scheduling |
| No CPU Limit | Info | Consider for predictable performance |
| No Liveness Probe | Info | Add for automatic restart on failure |
| No Readiness Probe | Info | Add to prevent traffic to unready pods |

## Observability

The operator exposes Prometheus metrics on `:8080/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `kubeassist_reconcile_total` | Counter | Reconciliations by name, namespace, result |
| `kubeassist_reconcile_duration_seconds` | Histogram | Reconciliation duration |
| `kubeassist_issues_total` | Gauge | Issues found by namespace and severity |

## TroubleshootRequest CRD

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TroubleshootRequest
metadata:
  name: diagnose-my-app
  namespace: default
spec:
  target:
    kind: Deployment    # Deployment, StatefulSet, DaemonSet, Pod, ReplicaSet
    name: my-app
  actions:
    - diagnose          # Analyze pod status and conditions
    - logs              # Collect container logs
    - events            # Collect related events
    # Or use "all" for everything
  tailLines: 100        # Log lines to collect (default: 100)
```

Status output includes issues, suggestions, and references to ConfigMaps containing collected logs and events. ConfigMaps are garbage collected automatically via OwnerReferences when the TroubleshootRequest is deleted.

## Architecture

```
CLI (kubeassist)
    |
    v
TroubleshootRequest CR  -->  Operator Controller
                                   |
                    +--------------+--------------+
                    v              v              v
                  Pods           Logs          Events
                (diagnose)     (collect)      (collect)
                    |              |              |
                    v              v              v
                 Status       ConfigMap       ConfigMap
```

## Roadmap: Team Health Dashboard

KubeAssist is evolving into a **complete team health dashboard** - one command to see everything your team cares about.

### Vision

```
┌────────────────────────────────────────────────────────────────────────────┐
│                    KubeAssist Team Health Dashboard                        │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  Namespace: [team-frontend ▼]              Last check: 2 minutes ago  [⟳] │
│                                                                            │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐      │
│  │  Workloads   │ │    Helm      │ │   GitOps     │ │   Secrets    │      │
│  │    12 ✓      │ │  3 ✓   1 ✗   │ │    5 ✓       │ │  8 ✓   1 ⚠   │      │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘      │
│                                                                            │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐                       │
│  │   Storage    │ │    Quotas    │ │   Network    │                       │
│  │    4 ✓       │ │   78% used   │ │  2 policies  │                       │
│  └──────────────┘ └──────────────┘ └──────────────┘                       │
│                                                                            │
│  ══════════════════════════════════════════════════════════════════════   │
│                                                                            │
│  ✗ CRITICAL  helmrelease/redis                                            │
│    Helm upgrade failed: values validation error                           │
│    ┌────────────────────────────────────────────────────────────────┐     │
│    │ 🤖 AI: The 'persistence.size' value changed from 10Gi to 5Gi. │     │
│    │    Helm won't shrink PVCs. Keep at 10Gi or delete the PVC.    │     │
│    └────────────────────────────────────────────────────────────────┘     │
│                                                                            │
│  ⚠ WARNING  secret/tls-cert                                               │
│    TLS certificate expires in 14 days                                     │
│    → Renew certificate before expiry                                      │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

### Planned Features

| Feature | Status | Description |
|---------|--------|-------------|
| **Workload Diagnostics** | ✅ Done | Pod health, crashes, resource issues |
| **Flux HelmReleases** | 🔜 Planned | Failed upgrades, suspended, stale |
| **Flux Kustomizations** | 🔜 Planned | Sync failures, drift detection |
| **Flux GitRepositories** | 🔜 Planned | Fetch errors, auth failures |
| **Secret Health** | 🔜 Planned | Cert expiry, missing references |
| **PVC Status** | 🔜 Planned | Pending, capacity warnings |
| **ResourceQuotas** | 🔜 Planned | Usage warnings, exceeded limits |
| **NetworkPolicies** | 🔜 Planned | Missing policy detection |
| **Web Dashboard** | 🔜 Planned | Real-time UI with SSE updates |
| **AI Analysis** | 🔜 Planned | Context-aware suggestions |

### Namespace Scoping

Support for multi-tenant clusters - scope checks to your team's namespaces:

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TeamHealthRequest
metadata:
  name: frontend-team
spec:
  scope:
    namespaces: [team-frontend, team-shared]
    # Or use label selector:
    # namespaceSelector:
    #   matchLabels:
    #     team: frontend
  checks: [workloads, helmreleases, secrets, quotas]
```

### AI-Powered Analysis

Optional integration with OpenAI or Anthropic for intelligent suggestions:

```
Static:  "Container exceeded memory limit. Increase limit."

AI:      "The api-server container is OOMKilled at 256Mi. Based on the
         logs, it loads a 180MB ML model at startup. Increase memory
         to 512Mi in deploy/production/api-server/deployment.yaml"
```

**Providers:** OpenAI (gpt-4o) | Anthropic (Claude) | None (static fallback)

### Architecture Evolution

```
Current:                              Future:
────────                              ──────

TroubleshootRequest                   TeamHealthRequest
       │                                     │
       v                                     v
  ┌─────────┐                         ┌─────────────┐
  │Reconcile│                         │  Checkers   │
  │  (pods) │                         ├─────────────┤
  └─────────┘                         │ Workloads   │
                                      │ HelmRelease │
                                      │ Kustomize   │
                                      │ Secrets     │
                                      │ Quotas      │
                                      │ PVCs        │
                                      └──────┬──────┘
                                             │
                                      ┌──────▼──────┐
                                      │ AI Provider │ (optional)
                                      │ ┌─────────┐ │
                                      │ │ OpenAI  │ │
                                      │ │ Claude  │ │
                                      │ │  None   │ │
                                      │ └─────────┘ │
                                      └──────┬──────┘
                                             │
                                      ┌──────▼──────┐
                                      │  Dashboard  │
                                      │  (Web UI)   │
                                      └─────────────┘
```

---

## Development

```sh
make test                    # Run tests
make run                     # Run locally against kubeconfig
make docker-build IMG=...    # Build container image
make demo-up                 # Deploy demo workloads
make demo-down               # Clean up demo
```

## License

Apache License 2.0
