# AI, Dashboard & DataSource

> AI provider abstraction, optimization pipeline, dashboard HTTP routing, DataSource abstraction, and scope resolver.

---

## A. AI Provider Abstraction

Thread-safe `Manager` wraps a `Provider` interface with caching, budgeting, and runtime reconfiguration.

```mermaid
classDiagram
    class Provider {
        <<interface>>
        +Name() string
        +Analyze(ctx, request) *AnalysisResponse, error
        +Available() bool
    }

    class Manager {
        -mu sync.RWMutex
        -provider Provider
        -explainProvider Provider
        -enabled bool
        -budget *Budget
        -cache *Cache
        +Analyze(ctx, request) *AnalysisResponse, error
        +Reconfigure(provider, apiKey, model, explainModel)
        +SetEnabled(bool)
    }

    class AnthropicProvider {
        +Name() "anthropic"
        -apiKey string
        -model string
        -httpClient *http.Client
    }

    class OpenAIProvider {
        +Name() "openai"
        -apiKey string
        -model string
        -httpClient *http.Client
    }

    class NoOpProvider {
        +Name() "noop"
    }

    class Cache {
        -capacity int
        -ttl time.Duration
        -items map LRU
        +Get(request) *AnalysisResponse, bool
        +Put(request, response)
    }

    class Budget {
        -windows []BudgetWindow
        -usage []windowUsage
        +CheckAllowance(tokens) error
        +RecordUsage(tokens)
    }

    class Sanitizer {
        +Sanitize(text) string
    }

    Provider <|.. AnthropicProvider
    Provider <|.. OpenAIProvider
    Provider <|.. NoOpProvider

    Manager *-- Provider : primary
    Manager *-- Provider : explain
    Manager *-- Cache
    Manager *-- Budget
    Manager ..> Sanitizer : uses
```

> **Source anchors:** `internal/ai/provider.go` (Provider interface, AnalysisRequest/Response types), `internal/ai/manager.go` (Manager struct), `internal/ai/anthropic.go`, `internal/ai/openai.go`, `internal/ai/noop.go`, `internal/ai/cache.go` (LRU + TTL, SHA256 key), `internal/ai/budget.go` (multi-window token tracking), `internal/ai/sanitize.go` (regex-based redaction)

---

## B. AI Pipeline with 9 Optimizations

Issues flow through a pipeline of cost-reduction optimizations before reaching the AI provider.

```mermaid
flowchart LR
    Issues["Raw<br/>Issues"] --> SevGate["1. Severity<br/>Gate"]
    SevGate -->|skip Info| ChangeOnly["2. Change-Only<br/>AI"]
    ChangeOnly -->|skip unchanged| Dedup["3. Cross-Issue<br/>Dedup"]
    Dedup --> Batch["4. Batch<br/>Grouping"]
    Batch --> Provider["5. Provider<br/>Call"]
    Provider --> PromptCache["6. Prompt<br/>Cache"]
    PromptCache --> TokenBudget["7. Token<br/>Budget"]
    TokenBudget --> LRUCache["8. LRU<br/>Response Cache"]
    LRUCache --> TieredRoute["9. Tiered<br/>Model Routing"]
    TieredRoute --> Response["Enhanced<br/>Response"]

    style SevGate fill:#f9f,stroke:#333
    style Dedup fill:#f9f,stroke:#333
    style LRUCache fill:#9f9,stroke:#333
    style TokenBudget fill:#ff9,stroke:#333
```

**Optimization summary:**

| # | Optimization | Where | Effect |
|---|-------------|-------|--------|
| 1 | Severity gate | `EnhanceAllWithAI` | Skip Info-level issues (~30% reduction) |
| 2 | Change-only AI | Dashboard `runAIAnalysisForCluster` | Skip AI when issue hash unchanged |
| 3 | Cross-issue dedup | `EnhanceAllWithAI` | Group identical patterns, send one representative |
| 4 | Batch grouping | `EnhanceAllWithAI` | Single API call for all issues |
| 5 | Provider call | `Manager.Analyze` | Dispatch to selected provider |
| 6 | Prompt cache | `AnthropicProvider` | `cache_control: ephemeral` header (~90% cost reduction) |
| 7 | Token budget | `Budget.CheckAllowance` | Daily/monthly caps with graceful `ErrBudgetExceeded` |
| 8 | LRU response cache | `Cache` | 100-entry, 5-min TTL, SHA256-keyed |
| 9 | Tiered routing | `Manager` | Separate models for analyze vs explain mode |

> **Source anchors:** `internal/ai/` package (all optimization files), `internal/checker/checker.go` (EnhanceAllWithAI), `internal/dashboard/server.go` (runAIAnalysisForCluster issue hash check)

---

## C. Dashboard HTTP Routing

The dashboard server exposes REST endpoints and SSE streaming, protected by auth middleware.

```mermaid
flowchart TD
    subgraph Middleware["Middleware Chain"]
        Security["securityHeaders<br/>CSP, HSTS, X-Frame"]
        RateLimit["rateLimiter<br/>(POST/PUT/DELETE only)"]
        Auth["authMiddleware<br/>Bearer token or<br/>session cookie"]
    end

    subgraph Health["Health & Streaming"]
        H1["GET /api/health"]
        H2["GET /api/health/history"]
        H3["GET /api/events (SSE)"]
    end

    subgraph Settings["Settings"]
        S1["GET /api/settings/ai"]
        S2["POST /api/settings/ai"]
        S3["GET /api/settings/ai/catalog"]
    end

    subgraph Actions["Actions"]
        A1["POST /api/check"]
        A2["POST /api/troubleshoot"]
        A3["GET /api/troubleshoot"]
    end

    subgraph Analysis["Analysis"]
        AN1["GET /api/causal/groups"]
        AN2["GET /api/prediction/trend"]
        AN3["GET /api/explain"]
    end

    subgraph Chat["NLQ Chat"]
        C1["POST /api/chat (SSE stream)"]
    end

    subgraph Fleet["Fleet / Multi-Cluster"]
        F1["GET /api/clusters"]
        F2["GET /api/fleet/summary"]
    end

    subgraph Issues["Issue Management"]
        I1["POST/DELETE /api/issues/acknowledge"]
        I2["POST/DELETE /api/issues/snooze"]
        I3["GET /api/issue-states"]
    end

    subgraph UI["Frontend"]
        SPA["GET / → SPA index.html"]
        Assets["GET /* → static assets"]
    end

    Security --> Auth
    Auth --> Health
    Auth --> Settings
    RateLimit --> Actions
    Auth --> Analysis
    Auth --> Chat
    RateLimit --> Chat
    Auth --> Fleet
    Auth --> Issues
    RateLimit --> Issues
    SPA --> Assets
```

> **Source anchors:** `internal/dashboard/server.go` (buildHandler method — route registration, securityHeaders, authMiddleware, sseAuthMiddleware, rateLimiter)

---

## D. DataSource Abstraction

The `DataSource` interface abstracts Kubernetes API access, enabling both direct and cross-cluster modes.

```mermaid
classDiagram
    class DataSource {
        <<interface>>
        +Get(ctx, key, obj, opts...) error
        +List(ctx, list, opts...) error
    }

    class KubernetesDataSource {
        +client.Reader (embedded)
    }

    class ConsoleDataSource {
        -baseURL string
        -clusterID string
        -httpClient *http.Client
        -bearerToken string
        +ForCluster(id) *ConsoleDataSource
        +GetClusters(ctx) []string, error
    }

    DataSource <|.. KubernetesDataSource
    DataSource <|.. ConsoleDataSource
```

```mermaid
flowchart LR
    subgraph Direct["Kubernetes Mode (default)"]
        KDS["KubernetesDataSource"] -->|"client.Reader"| K8sAPI["Kubernetes API"]
    end

    subgraph Console["Console Mode (multi-cluster)"]
        CDS["ConsoleDataSource"] -->|"HTTP GET"| Backend["console-backend<br/>:8085"]
        Backend -->|"per-cluster<br/>kubeconfig"| K8sA["Cluster A API"]
        Backend -->|"per-cluster<br/>kubeconfig"| K8sB["Cluster B API"]
    end
```

In console mode, `cmd/console-backend` enforces auth/TLS by default (`--allow-insecure` is explicit dev-only bypass).

> **Source anchors:** `internal/datasource/datasource.go` (DataSource interface), `internal/datasource/kubernetes.go` (KubernetesDataSource), `internal/datasource/console.go` (ConsoleDataSource, GVK→resource mapping, URL building, ForCluster), `cmd/console-backend/main.go` (auth/TLS requirements)

---

## E. Scope Resolver

Resolves a `TeamHealthRequest.Spec.Scope` into a concrete list of namespaces to check.

```mermaid
flowchart TD
    Input["TeamHealthRequest<br/>Spec.Scope"]

    Input --> Check{"Scope type?"}

    Check -->|"namespaces: [a, b, c]"| Explicit["validateNamespaces<br/>verify each exists via API"]
    Check -->|"namespaceSelector:<br/>matchLabels: ..."| Selector["resolveBySelector<br/>List namespaces matching labels"]
    Check -->|"no namespaces and no selector"| Default["Use default<br/>namespace"]

    Explicit --> Filter["FilterSystemNamespaces<br/>remove kube-system, kube-public,<br/>kube-node-lease, flux-system"]
    Selector --> Filter
    Default --> Filter

    Filter --> Output["[]string<br/>sorted namespace list"]
```

Namespace capping is applied by the TeamHealthRequest controller (`MaxNamespaces = 50`), not in the resolver package.

> **Source anchors:** `internal/scope/resolver.go` (Resolver struct, ResolveNamespaces, validateNamespaces, resolveBySelector, FilterSystemNamespaces), `internal/controller/teamhealthrequest_controller.go` (`MaxNamespaces` truncation)
