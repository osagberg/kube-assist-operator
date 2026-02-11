# Checker Registry & Causal Engine

> Checker interface, registry execution model, causal correlation pipeline, and AI batch enhancement.

---

## A. Checker Interface & Registry

All health checkers implement the `Checker` interface and register with the `Registry` at startup.

```mermaid
classDiagram
    class Checker {
        <<interface>>
        +Name() string
        +Check(ctx, checkCtx) *CheckResult, error
        +Supports(ctx, ds) bool
    }

    class Registry {
        -mu sync.RWMutex
        -checkers map[string]Checker
        +Register(c Checker) error
        +MustRegister(c Checker)
        +Unregister(name string)
        +Replace(name string, c Checker)
        +RunAll(ctx, checkCtx, config) map[string]*CheckResult
        +List() []string
    }

    class WorkloadChecker {
        +Name() "workloads"
    }
    class SecretChecker {
        +Name() "secrets"
    }
    class PVCChecker {
        +Name() "pvcs"
    }
    class QuotaChecker {
        +Name() "quotas"
    }
    class NetworkPolicyChecker {
        +Name() "networkpolicies"
    }
    class HelmReleaseChecker {
        +Name() "helmreleases"
    }
    class KustomizationChecker {
        +Name() "kustomizations"
    }
    class GitRepositoryChecker {
        +Name() "gitrepositories"
    }
    class PluginChecker {
        +Name() "plugin:name"
        -compiled []compiledCheck
    }

    Checker <|.. WorkloadChecker
    Checker <|.. SecretChecker
    Checker <|.. PVCChecker
    Checker <|.. QuotaChecker
    Checker <|.. NetworkPolicyChecker
    Checker <|.. HelmReleaseChecker
    Checker <|.. KustomizationChecker
    Checker <|.. GitRepositoryChecker
    Checker <|.. PluginChecker

    Registry o-- Checker : contains
```

> **Source anchors:** `internal/checker/checker.go` (Checker interface, CheckResult, Issue types), `internal/checker/registry.go` (Registry struct), `internal/checker/workload/`, `internal/checker/resource/` (secrets, pvcs, quotas, networkpolicies), `internal/checker/flux/` (helmreleases, kustomizations, gitrepositories), `internal/checker/plugin/checker.go`

---

## B. Registry.RunAll Execution

`RunAll` spawns one goroutine per checker and collects results via a channel.

```mermaid
sequenceDiagram
    participant Caller
    participant Registry
    participant Ch1 as workloads
    participant Ch2 as secrets
    participant ChN as ...checker N

    Caller->>Registry: RunAll(ctx, checkCtx, config)
    Registry->>Registry: Filter by Supports() + requested checks

    par Concurrent execution
        Registry->>Ch1: go Check(ctx, checkCtx)
        Registry->>Ch2: go Check(ctx, checkCtx)
        Registry->>ChN: go Check(ctx, checkCtx)
    end

    Ch1-->>Registry: result via channel
    Ch2-->>Registry: result via channel
    ChN-->>Registry: result via channel

    Registry->>Registry: Collect all results into map
    Registry-->>Caller: map[string]*CheckResult
```

> **Source anchors:** `internal/checker/registry.go` (RunAll — goroutine fan-out, channel collection, Supports filtering)

---

## C. Causal Correlation Pipeline

Three strategies run in parallel, then results are merged, deduplicated, and sorted by confidence.

```mermaid
flowchart TD
    Input["CheckResults<br/>(all checkers)"]

    Input --> Rules["CrossCheckerRules<br/>4 predefined rules"]
    Input --> Graph["ResourceGraph<br/>owner-reference chains"]
    Input --> Temporal["TemporalCorrelation<br/>time-window grouping"]

    Rules --> Merge["Merge all CausalGroups"]
    Graph --> Merge
    Temporal --> Merge

    Merge --> Dedup["Deduplicate<br/>(by issue key set)"]
    Dedup --> Sort["Sort by confidence<br/>(descending)"]
    Sort --> Output["CausalContext<br/>Timeline + Groups"]
```

> **Source anchors:** `internal/causal/correlator.go` (Analyze method), `internal/causal/rules.go` (CrossCheckerRules strategy), `internal/causal/graph.go` (ResourceGraph strategy), `internal/causal/temporal.go` (TemporalCorrelation strategy)

---

## D. Cross-Checker Rules

Four predefined rules match issue tags across different checkers to surface root causes.

```mermaid
flowchart LR
    subgraph Rule1["OOM + Quota (0.85)"]
        oom["workloads:<br/>OOMKilled"] --> r1{match}
        quota["quotas:<br/>near limit"] --> r1
        r1 --> c1["Quota constraining<br/>memory allocation"]
    end

    subgraph Rule2["Crash + ImagePull (0.80)"]
        crash["workloads:<br/>CrashLoopBackOff"] --> r2{match}
        pull["workloads:<br/>ImagePullBackOff"] --> r2
        r2 --> c2["Registry or image<br/>configuration issue"]
    end

    subgraph Rule3["Flux Chain (0.90)"]
        git["gitrepositories:<br/>failed"] --> r3{match}
        kust["kustomizations:<br/>failed"] --> r3
        helm["helmreleases:<br/>failed"] --> r3
        r3 --> c3["Upstream source<br/>failure cascading"]
    end

    subgraph Rule4["PVC + Workload (0.75)"]
        pvc["pvcs:<br/>pending"] --> r4{match}
        pod["workloads:<br/>pending"] --> r4
        r4 --> c4["Storage not bound,<br/>pods cannot start"]
    end
```

> **Source anchors:** `internal/causal/rules.go` (rule definitions with tag matchers and confidence scores)

---

## E. AI Batch Enhancement

`EnhanceAllWithAI` collects issues from all checkers, applies optimizations, then makes a single batched AI call.

```mermaid
sequenceDiagram
    participant Caller as Controller / Dashboard
    participant Enhance as EnhanceAllWithAI
    participant AI as AI Manager

    Caller->>Enhance: EnhanceAllWithAI(results, causalCtx)

    Enhance->>Enhance: Collect all issues from results
    Enhance->>Enhance: Filter out Info severity
    Enhance->>Enhance: Cap by severity priority
    Enhance->>Enhance: Deduplicate by signature hash

    Enhance->>AI: Analyze(batchRequest)
    Note over AI: Cache check → Budget check →<br/>Provider call → Cache store

    AI-->>Enhance: AnalysisResponse

    Enhance->>Enhance: Fan-out responses to all<br/>duplicates sharing same signature
    Enhance->>Enhance: Apply EnhancedSuggestion to each issue
    Enhance-->>Caller: Updated results with AI fields
```

> **Source anchors:** `internal/checker/checker.go` (EnhanceAllWithAI — severity gating, deduplication, signature hashing, fan-out), `internal/ai/manager.go` (Manager.Analyze — cache, budget, provider dispatch)
