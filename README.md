# KubeAssist Operator

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-326CE5?style=flat&logo=kubernetes)](https://kubernetes.io/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**One command to diagnose your entire Kubernetes cluster.**

KubeAssist is a Kubernetes operator that simplifies workload troubleshooting. Instead of running multiple `kubectl` commands and interpreting cryptic error messages, just run `kubeassist` and get instant, actionable diagnostics.

```
╔══════════════════════════════════════════════════════════════╗
║              🔍 KubeAssist Workload Diagnostics              ║
╚══════════════════════════════════════════════════════════════╝

✗ production/api-server
   ● [Critical] Container app is waiting: CrashLoopBackOff
     → Application is crashing. Check logs for error messages.
   ○ [Warning] Container app has restarted 12 times
     → Check logs for crash reasons.

✗ production/worker
   ● [Critical] Container worker is waiting: ImagePullBackOff
     → Check if the image exists and credentials are configured correctly.

✓ production/redis
   All 1 pod(s) healthy - no issues found

────────────────────────────────────────────────────────────────
Summary: 1 healthy, 2 unhealthy (2 critical, 1 warnings)
```

## Why KubeAssist?

In enterprise Kubernetes environments, developers often:
- **Lack direct cluster access** - Security policies restrict kubectl usage
- **Don't have K8s expertise** - They just want to know why their app isn't working
- **Waste time on troubleshooting** - Jumping between pods, logs, events, and descriptions

KubeAssist solves this by providing:
- **One-command diagnostics** - `kubeassist` scans your entire cluster
- **Actionable suggestions** - Not just "CrashLoopBackOff" but "Application is crashing. Check logs."
- **Collected evidence** - Logs and events stored in ConfigMaps for later analysis

## Quick Start

### Option 1: CLI Tool (Recommended)

```sh
# Clone and build
git clone https://github.com/osagberg/kube-assist-operator.git
cd kube-assist-operator
make install-cli

# Run diagnostics on entire cluster
kubeassist
```

### Option 2: Deploy as Operator

```sh
# Install CRDs and deploy operator
make deploy IMG=ghcr.io/osagberg/kube-assist-operator:latest

# Create a TroubleshootRequest
kubectl apply -f - <<EOF
apiVersion: assist.cluster.local/v1alpha1
kind: TroubleshootRequest
metadata:
  name: diagnose-my-app
  namespace: default
spec:
  target:
    name: my-app
  actions: [all]
EOF

# Check results
kubectl get troubleshootrequest diagnose-my-app -o yaml
```

## Detected Issues

KubeAssist automatically detects and explains these common problems:

| Issue | Severity | Example Suggestion |
|-------|----------|-------------------|
| CrashLoopBackOff | Critical | "Application is crashing. Check logs for error messages." |
| ImagePullBackOff | Critical | "Check if the image exists and credentials are configured correctly." |
| OOMKilled | Critical | "Container exceeded memory limit. Increase memory limit or optimize usage." |
| CreateContainerConfigError | Critical | "Check ConfigMaps and Secrets referenced by the pod." |
| Pending (Unschedulable) | Critical | "Check node resources, taints/tolerations, and affinity rules." |
| High Restart Count | Warning | "Check logs for crash reasons. Consider increasing resource limits." |
| No Memory Limit | Warning | "Set memory limits to prevent OOM issues and ensure fair resource sharing." |

## CLI Usage

```sh
# Scan entire cluster (default)
kubeassist

# Scan specific namespace
kubeassist production

# Filter by label
kubeassist -l app=api

# Keep TroubleshootRequests after scan (default: auto-cleanup)
kubeassist --cleanup=false
```

## Architecture

```
┌─────────────────┐     ┌──────────────────────┐     ┌─────────────────┐
│   kubeassist    │────▶│  TroubleshootRequest │────▶│    Operator     │
│     (CLI)       │     │        (CR)          │     │   Controller    │
└─────────────────┘     └──────────────────────┘     └────────┬────────┘
                                                              │
                        ┌─────────────────────────────────────┼─────────────────────────────────────┐
                        │                                     ▼                                     │
                        │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
                        │  │    Pods      │  │    Logs      │  │   Events     │  │  ConfigMaps  │  │
                        │  │  (diagnose)  │  │  (collect)   │  │  (collect)   │  │   (store)    │  │
                        │  └──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘  │
                        │                                Kubernetes API                             │
                        └───────────────────────────────────────────────────────────────────────────┘
```

## Supported Workload Types

- Deployment
- StatefulSet
- DaemonSet
- ReplicaSet
- Pod

## TroubleshootRequest Spec

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TroubleshootRequest
metadata:
  name: diagnose-my-app
  namespace: default
spec:
  target:
    kind: Deployment    # Deployment, StatefulSet, DaemonSet, Pod, ReplicaSet
    name: my-app        # Name of the workload
  actions:
    - diagnose          # Analyze pod status and conditions
    - logs              # Collect container logs
    - events            # Collect related events
    # Or use "all" for everything
  tailLines: 100        # Number of log lines to collect (default: 100)
```

## Status Output

```yaml
status:
  phase: Completed
  summary: "2 critical, 1 warning issue(s) found"
  issues:
    - type: ContainerNotReady
      severity: Critical
      message: "Container app is waiting: CrashLoopBackOff"
      suggestion: "Application is crashing. Check logs for error messages."
    - type: HighRestartCount
      severity: Warning
      message: "Container app has restarted 12 times"
      suggestion: "Check logs for crash reasons."
  logsConfigMap: diagnose-my-app-logs
  eventsConfigMap: diagnose-my-app-events
  startedAt: "2024-01-15T10:30:00Z"
  completedAt: "2024-01-15T10:30:02Z"
```

## Development

```sh
# Run tests
make test

# Run locally against current kubeconfig
make run

# Build container image
make docker-build IMG=my-registry/kube-assist-operator:dev
```

## License

Apache License 2.0
