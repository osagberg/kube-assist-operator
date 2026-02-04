# KubeAssist Operator

A Kubernetes operator that simplifies workload troubleshooting for developers who may not have deep Kubernetes expertise. Create a simple `TroubleshootRequest` CR and get automated diagnostics, logs, and events without needing direct cluster access.

## Why KubeAssist?

In enterprise environments with strict AKS/EKS/GKE clusters, developers often:
- Lack direct kubectl access for security reasons
- Don't have time to learn Kubernetes troubleshooting
- Need quick answers: "Why isn't my deployment working?"

KubeAssist bridges this gap by providing a declarative way to request diagnostics.

## Features

- **Pod Diagnostics**: Detects common issues like CrashLoopBackOff, OOMKilled, ImagePullBackOff
- **Log Collection**: Gathers logs from target pods into ConfigMaps
- **Event Collection**: Captures relevant Kubernetes events
- **Actionable Suggestions**: Provides fix recommendations for detected issues

## Quick Start

### Install CRDs

```sh
make install
```

### Run Locally (development)

```sh
make run
```

### Deploy to Cluster

```sh
make deploy IMG=ghcr.io/osagberg/kube-assist-operator:latest
```

## Usage

Create a `TroubleshootRequest` to diagnose a workload:

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TroubleshootRequest
metadata:
  name: troubleshoot-my-app
  namespace: my-namespace
spec:
  target:
    kind: Deployment  # Deployment, StatefulSet, DaemonSet, or Pod
    name: my-app
  actions:
    - all            # diagnose, logs, events, or all
  tailLines: 200     # Number of log lines to collect
```

Check results:

```sh
kubectl get troubleshootrequest troubleshoot-my-app -o yaml
```

The status will contain:
- `phase`: Pending -> Running -> Completed/Failed
- `summary`: Brief overview (e.g., "2 critical, 1 warning issue(s) found")
- `issues`: List of detected problems with severity and suggestions
- `logsConfigMap`: Name of ConfigMap containing collected logs
- `eventsConfigMap`: Name of ConfigMap containing events

## Example Output

```yaml
status:
  phase: Completed
  summary: "1 critical, 1 warning issue(s) found"
  issues:
    - type: ContainerNotReady
      severity: Critical
      message: "Container app is waiting: CrashLoopBackOff - back-off 5m0s restarting failed container"
      suggestion: "Application is crashing. Check logs for error messages."
    - type: HighRestartCount
      severity: Warning
      message: "Container app has restarted 12 times"
      suggestion: "Check logs for crash reasons. Consider increasing resource limits or fixing application bugs."
  logsConfigMap: troubleshoot-my-app-logs
  eventsConfigMap: troubleshoot-my-app-events
```

## Supported Workload Types

- Deployment
- StatefulSet
- DaemonSet
- Pod
- ReplicaSet

## License

Apache License 2.0
