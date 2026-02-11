# Reconciliation Flows

> Controller lifecycles and CR state machines for all three KubeAssist controllers.

---

## A. TeamHealthRequest Full Pipeline

The most complex reconciliation — runs checkers, causal analysis, and AI enhancement across namespaces.

```mermaid
sequenceDiagram
    participant CR as controller-runtime
    participant R as THR Reconciler
    participant S as Scope Resolver
    participant Reg as Checker Registry
    participant C as Causal Correlator
    participant AI as AI Manager
    participant K as Kubernetes API

    CR->>R: Reconcile(req)
    R->>K: Fetch TeamHealthRequest
    R->>R: Check TTL expiry
    R->>K: Set Phase=Running

    R->>S: ResolveNamespaces(scope)
    S->>K: List/validate namespaces
    S-->>R: []string (filtered, 50-ns cap)

    R->>Reg: RunAll(ctx, checkCtx, config)
    Note over Reg: Concurrent goroutines<br/>per checker, results<br/>collected via channel
    Reg-->>R: map[string]*CheckResult

    R->>C: Analyze(results)
    Note over C: 3 strategies in parallel:<br/>Rules, Graph, Temporal
    C-->>R: CausalContext

    R->>R: causal.ToAIContext(causalCtx)
    R->>AI: checker.EnhanceAllWithAI(results, aiCtx)
    Note over AI: Severity gate → dedup →<br/>batch → provider call
    AI-->>R: Enhanced suggestions

    R->>R: aggregateResults → status patch
    R->>K: Update status (Phase=Completed)

    R-)R: launchNotifications (async webhooks)
```

> **Source anchors:** `internal/controller/teamhealthrequest_controller.go` (Reconcile method), `internal/scope/resolver.go` (ResolveNamespaces), `internal/checker/registry.go` (RunAll), `internal/causal/correlator.go` (Analyze), `internal/checker/checker.go` (EnhanceAllWithAI)

---

## B. TroubleshootRequest Lifecycle

On-demand diagnostics for a specific workload target.

```mermaid
sequenceDiagram
    participant CR as controller-runtime
    participant R as TR Reconciler
    participant K as Kubernetes API

    CR->>R: Reconcile(req)
    R->>K: Fetch TroubleshootRequest
    R->>R: Check TTL expiry, skip if Completed/Failed
    R->>K: Set Phase=Running

    R->>K: findTargetPods(target kind/name)
    R->>R: diagnosePods (status, restarts, OOM, probes)

    alt actions include "logs" or "all"
        R->>K: collectLogs (tail N lines per container)
        R->>K: Store in ConfigMap (logsConfigMap)
    end

    alt actions include "events" or "all"
        R->>K: collectEvents (field selector)
        R->>K: Store in ConfigMap (eventsConfigMap)
    end

    R->>R: generateSummary (issue count, severity)
    R->>K: Update status (Phase=Completed, issues, configMaps)
```

> **Source anchors:** `internal/controller/troubleshootrequest_controller.go` (Reconcile, findTargetPods, diagnosePods, collectLogs, collectEvents)

---

## C. CheckPlugin Lifecycle

CRD-driven custom health checks with CEL expressions and hot-reload into the registry.

```mermaid
sequenceDiagram
    participant CR as controller-runtime
    participant R as CP Reconciler
    participant P as plugin.NewPluginChecker
    participant Reg as Checker Registry

    CR->>R: Reconcile(req)
    R->>R: Fetch CheckPlugin CR

    alt Deletion in progress
        R->>Reg: Unregister("plugin:" + name)
        R->>R: RemoveFinalizer
        R-->>CR: done
    else Create / Update
        R->>R: EnsureFinalizer
        R->>P: NewPluginChecker(name, spec)
        Note over P: Compile all CEL rules<br/>(condition + message)
        alt Compilation fails
            R->>R: Set status Ready=False, Error
            R-->>CR: requeue
        else Compilation succeeds
            R->>Reg: Replace("plugin:" + name, checker)
            R->>R: Set status Ready=True
            R-->>CR: done
        end
    end
```

> **Source anchors:** `internal/controller/checkplugin_controller.go` (Reconcile, finalizer handling), `internal/checker/plugin/checker.go` (NewPluginChecker, CEL compilation), `internal/checker/registry.go` (Replace, Unregister)

---

## D. CR State Machines

### TeamHealthRequest

```mermaid
stateDiagram-v2
    [*] --> Pending: CR created
    Pending --> Running: Reconcile starts
    Running --> Completed: All checkers + AI done
    Running --> Failed: Error during reconciliation
    Completed --> [*]: TTL expiry → delete
    Failed --> [*]: TTL expiry → delete
```

> Phase constants: `TeamHealthPhasePending`, `TeamHealthPhaseRunning`, `TeamHealthPhaseCompleted`, `TeamHealthPhaseFailed`

### TroubleshootRequest

```mermaid
stateDiagram-v2
    [*] --> Pending: CR created
    Pending --> Running: Reconcile starts
    Running --> Completed: Diagnostics finished
    Running --> Failed: Target not found / error
    Completed --> [*]: TTL expiry → delete
    Failed --> [*]: TTL expiry → delete
```

> Phase constants: `PhasePending`, `PhaseRunning`, `PhaseCompleted`, `PhaseFailed`

### CheckPlugin

```mermaid
stateDiagram-v2
    [*] --> EnsureFinalizer: CR created
    EnsureFinalizer --> CompileCEL: Finalizer added
    CompileCEL --> Ready: All rules compile
    CompileCEL --> Error: Compilation failure
    Error --> CompileCEL: CR updated (retry)
    Ready --> CompileCEL: CR updated (re-compile)
    Ready --> Unregister: Deletion requested
    Error --> Unregister: Deletion requested
    Unregister --> RemoveFinalizer: Checker removed from registry
    RemoveFinalizer --> [*]
```

> **Source anchors:** Phase constants in `api/v1alpha1/teamhealthrequest_types.go:215-218`, `api/v1alpha1/troubleshootrequest_types.go:83-86`, controller finalizer logic in `internal/controller/checkplugin_controller.go`
