# System Overview

> Architecture diagrams for the KubeAssist operator — system context, package dependencies, and binary composition.

---

## A. System Context

How users interact with KubeAssist and how internal components connect.

```mermaid
flowchart TD
    subgraph User["User Interface"]
        CLI["CLI<br/><code>cmd/kubeassist/</code>"]
        Dashboard["Dashboard :9090<br/><code>internal/dashboard/server.go</code>"]
    end

    subgraph Controllers["Controllers"]
        THR["TeamHealthRequest<br/>Controller"]
        TR["TroubleshootRequest<br/>Controller"]
        CP["CheckPlugin<br/>Controller"]
    end

    subgraph Engine["Analysis Engine"]
        Registry["Checker Registry<br/>8 built-in + plugins"]
        Causal["Causal Correlator<br/>3 strategies"]
        AI["AI Manager<br/>Anthropic / OpenAI / NoOp"]
        Predict["Predictive Health<br/>OLS regression"]
    end

    subgraph Data["Data Layer"]
        DS["DataSource<br/>K8s / Console"]
        Scope["Scope Resolver<br/>ns filtering, 50-ns cap"]
        History["Health History<br/>ring buffer"]
    end

    K8s["Kubernetes API"]

    CLI -->|creates CRs| K8s
    Dashboard -->|SSE, REST| Engine
    Dashboard -->|POST /api/check| Registry
    Dashboard -->|POST /api/troubleshoot| K8s

    THR --> Scope
    THR --> Registry
    THR --> Causal
    THR --> AI
    TR --> DS
    CP -->|CEL compile| Registry

    Registry --> DS
    Causal --> Registry
    AI --> DS
    Predict --> History
    DS --> K8s
    Scope --> DS
```

> **Source anchors:** `cmd/main.go`, `cmd/kubeassist/`, `internal/controller/`, `internal/checker/`, `internal/causal/`, `internal/ai/`, `internal/dashboard/server.go`, `internal/datasource/`, `internal/prediction/`, `internal/scope/`, `internal/history/`

---

## B. Package Dependency Graph

How `internal/` packages import each other. Arrows point from importer to dependency.

```mermaid
flowchart LR
    controller["controller"]
    dashboard["dashboard"]
    checker["checker"]
    causal["causal"]
    ai["ai"]
    datasource["datasource"]
    scope["scope"]
    notifier["notifier"]
    history["history"]
    prediction["prediction"]
    plugin["checker/plugin"]

    controller --> checker
    controller --> causal
    controller --> ai
    controller --> datasource
    controller --> scope
    controller --> notifier

    dashboard --> ai
    dashboard --> checker
    dashboard --> causal
    dashboard --> datasource
    dashboard --> scope
    dashboard --> history
    dashboard --> prediction

    checker --> datasource
    checker --> ai

    causal --> checker
    causal --> ai

    prediction --> history

    plugin --> checker
    plugin --> datasource
```

> **Source anchors:** import statements in each package under `internal/`

---

## C. Binary Composition

KubeAssist ships three entry points compiled from a single Go module.

```mermaid
flowchart TD
    subgraph Module["go module: kube-assist-operator"]
        main["cmd/main.go<br/>Operator entry point"]
        cli["cmd/kubeassist/<br/>CLI tool"]
        console["cmd/console-backend/<br/>Multi-cluster HTTP backend"]
    end

    subgraph MainBin["Single Binary (cmd/main.go)"]
        mgr["controller-runtime<br/>Manager"]
        dash["Dashboard Server<br/>:9090"]
        thr_ctrl["TeamHealthRequest<br/>Reconciler"]
        tr_ctrl["TroubleshootRequest<br/>Reconciler"]
        cp_ctrl["CheckPlugin<br/>Reconciler"]
        webhooks["Admission<br/>Webhooks"]
    end

    subgraph CLIBin["CLI Binary (cmd/kubeassist/)"]
        diag["kubeassist<br/>→ TroubleshootRequest"]
        health["kubeassist health<br/>→ TeamHealthRequest"]
    end

    subgraph ConsoleBin["Console Backend (cmd/console-backend/)"]
        api["HTTP API :8085<br/>Multi-cluster proxy"]
    end

    main --> mgr
    mgr --> thr_ctrl
    mgr --> tr_ctrl
    mgr --> cp_ctrl
    mgr --> webhooks
    main -->|"--enable-dashboard"| dash

    cli --> diag
    cli --> health

    console --> api
    api -->|"per-cluster<br/>kubeconfig"| K8s["Kubernetes APIs"]
```

> **Source anchors:** `cmd/main.go` (manager setup, dashboard start, controller registration), `cmd/kubeassist/main.go` (CLI dispatch), `cmd/console-backend/main.go` (HTTP server with auth/TLS)
