# AI Integration

kube-assist can enhance health check suggestions using AI providers to deliver context-aware remediation guidance. AI can be configured at runtime from the dashboard without restarting the operator.

## Overview

When enabled, the AI integration:

1. Collects detected issues from health checks
2. Sanitizes data to remove sensitive information (secrets, tokens, credentials, internal IPs)
3. Sends context to the configured AI provider
4. Returns enhanced suggestions with root cause analysis and remediation steps

AI enhancement runs after standard health checks complete. Results include both static suggestions (with kubectl commands) and AI-generated insights.

## Configuration

There are four ways to configure AI, from simplest to most flexible:

### 1. Dashboard UI (Recommended for Quick Setup)

The dashboard includes a built-in AI settings panel. No restart required.

1. Open the dashboard at `http://<host>:9090`
2. Locate the AI Settings panel
3. Toggle AI on, select provider/model, and save
4. Click Save

Changes take effect immediately. The dashboard uses the `POST /api/settings/ai` endpoint, which calls `Manager.Reconfigure()` to swap the active provider at runtime. Both the dashboard and the controllers share the same `ai.Manager` instance, so the change is reflected everywhere.

Important: `POST /api/settings/ai` rejects direct API key submission. Configure API keys through Secret/env (`KUBE_ASSIST_AI_API_KEY`) and use dashboard settings only for runtime toggles/provider/model.

### 2. CLI Flags (Operator)

```bash
./manager \
  --enable-ai \
  --ai-provider=anthropic \
  --ai-model=claude-haiku-4-5-20251001
```

| Flag | Description | Default |
|------|-------------|---------|
| `--enable-ai` | Enable AI-powered suggestions | `false` |
| `--ai-provider` | Provider: `anthropic`, `openai`, `noop` | `noop` |
| `--ai-model` | Model name (uses provider default if empty) | - |

### 3. Environment Variables

```bash
export KUBE_ASSIST_AI_PROVIDER=anthropic
export KUBE_ASSIST_AI_API_KEY=your-api-key-here
export KUBE_ASSIST_AI_MODEL=claude-haiku-4-5-20251001
export KUBE_ASSIST_AI_ENDPOINT=https://api.anthropic.com/v1/messages  # optional
```

The operator reads `KUBE_ASSIST_AI_API_KEY` from environment or Kubernetes Secret-backed env vars.

### 4. Helm Values

```yaml
ai:
  enabled: true
  provider: "anthropic"  # anthropic, openai, or noop
  model: ""              # optional, uses provider default

  # API key from Kubernetes Secret (recommended)
  apiKeySecretRef:
    name: "kube-assist-ai-secret"
    key: "api-key"
```

## API Key Management

### Using Kubernetes Secrets (Recommended)

Create the secret:

```bash
kubectl create secret generic kube-assist-ai-secret \
  --namespace kube-assist-system \
  --from-literal=api-key=your-api-key-here
```

Reference in Helm values:

```yaml
ai:
  enabled: true
  provider: "anthropic"
  apiKeySecretRef:
    name: "kube-assist-ai-secret"
    key: "api-key"
```

The deployment template mounts the secret as the `KUBE_ASSIST_AI_API_KEY` environment variable.

### Using External Secrets Operator

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: kube-assist-ai-secret
  namespace: kube-assist-system
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: kube-assist-ai-secret
  data:
    - secretKey: api-key
      remoteRef:
        key: secret/data/kube-assist
        property: ai-api-key
```

## Providers

### Anthropic (Claude)

| Setting | Value |
|---------|-------|
| Provider name | `anthropic` |
| Default model | `claude-haiku-4-5-20251001` |
| Default endpoint | `https://api.anthropic.com/v1/messages` |
| API version | `2023-06-01` |

Recommended models:
- `claude-haiku-4-5-20251001` - Cost-efficient (default)
- `claude-sonnet-4-5-20250929` - Balanced performance and cost
- `claude-opus-4-6` - Highest quality analysis

### OpenAI

| Setting | Value |
|---------|-------|
| Provider name | `openai` |
| Default model | `gpt-4o-mini` |
| Default endpoint | `https://api.openai.com/v1/chat/completions` |

Recommended models:
- `gpt-4o-mini` - Cost-optimized (default)
- `gpt-4o` - Best analysis quality
- `gpt-4-turbo` - Faster with larger context

### NoOp (Testing)

| Setting | Value |
|---------|-------|
| Provider name | `noop` |
| API key required | No |

Returns static suggestions without AI enhancement. Use for:
- Testing without API costs
- Environments without external network access
- Development and CI pipelines

## Data Sanitization

Before sending data to AI providers, the sanitizer removes sensitive information.

### Data Sent to AI

- Issue type and severity
- Resource kind and name (partially redacted)
- Namespace name
- Error messages (sanitized)
- Kubernetes version and provider
- Static suggestion text

### Data Redacted

The sanitizer removes patterns matching:

| Category | Examples |
|----------|----------|
| API keys and tokens | `api_key=...`, `Bearer ...` |
| AWS credentials | `AKIA...`, `aws_secret_access_key` |
| Connection strings | `postgres://...`, `mongodb://...` |
| Certificates | `-----BEGIN CERTIFICATE-----` |
| JWT tokens | `eyJ...` |
| Email addresses | `user@example.com` |
| Internal IPs | `10.x.x.x`, `192.168.x.x` |
| Base64 secrets | Long base64 strings in data fields |
| Secret references | `secretRef: ...` |

Resource names containing `secret`, `token`, `credential`, `password`, or `key` are replaced with `[REDACTED]`.

### Custom Patterns

Add custom sanitization patterns programmatically:

```go
sanitizer := ai.NewSanitizer()
sanitizer.AddPattern(`my-custom-pattern`)
```

## AI Manager Architecture

The `ai.Manager` struct (`internal/ai/manager.go`) wraps the `ai.Provider` interface with thread-safe runtime reconfiguration. It is shared between the dashboard server and the controllers.

Key properties:

- **Thread-safe**: All access is protected by `sync.RWMutex`
- **Drop-in replacement**: Manager implements the `Provider` interface, so existing code that accepts a Provider works unchanged
- **Runtime reconfiguration**: `Reconfigure(provider, apiKey, model, explainModel)` swaps providers/models without downtime
- **Enable/disable toggle**: `SetEnabled(bool)` turns AI on or off without changing the provider

When the dashboard receives a `POST /api/settings/ai` request, it calls `Manager.Reconfigure()` with staged settings and atomically swaps the provider/model. Because the Manager is shared, the controllers immediately use the new configuration on their next reconciliation.

## Settings API Endpoints

### GET /api/settings/ai

Returns the current AI configuration. The API key is never exposed -- only a boolean `hasApiKey` field.

```json
{
  "enabled": true,
  "provider": "anthropic",
  "model": "claude-haiku-4-5-20251001",
  "hasApiKey": true,
  "providerReady": true
}
```

### POST /api/settings/ai

Updates AI configuration at runtime. Only provided fields are changed.
When `DASHBOARD_AUTH_TOKEN` is configured, include `Authorization: Bearer <token>`.

```json
{
  "enabled": true,
  "provider": "anthropic",
  "model": "claude-haiku-4-5-20251001",
  "explainModel": "claude-haiku-4-5-20251001"
}
```

Optional key-clear request:

```json
{
  "enabled": true,
  "provider": "anthropic",
  "clearApiKey": true
}
```

Valid providers: `anthropic`, `openai`, `noop`.
Direct `apiKey` in request body is rejected with HTTP 400.

---

## Example Output

### Without AI

Every checker includes specific kubectl commands, common root causes, and links to Kubernetes docs even without AI enabled.

```
workloads (2 healthy, 1 critical)
   [Critical] default/api-server
     Pod api-server-abc123 in CrashLoopBackOff (5 restarts)
     -> The container is repeatedly crashing. Check recent logs with:
        kubectl logs api-server-abc123 --previous.
        Common causes: missing config/secrets, incorrect entrypoint, or application errors.
        If OOM-related, check: kubectl describe pod api-server-abc123 | grep -i oom
```

### With AI Enhancement

```
workloads (2 healthy, 1 critical)
   [Critical] default/api-server
     Pod api-server-abc123 in CrashLoopBackOff (5 restarts)

     AI Analysis:
     Root Cause: Application failing health checks due to missing database connection

     Steps:
     1. Check database connectivity: kubectl exec -it api-server-abc123 -- nc -zv db-host 5432
     2. Verify DATABASE_URL environment variable is set correctly
     3. Ensure database credentials secret exists and is mounted
     4. Review application startup logs for connection errors

     Confidence: 0.85
```

## Response Structure

AI responses include:

```json
{
  "enhancedSuggestions": {
    "default/deployment/api-server": {
      "suggestion": "Database connection failing on startup",
      "rootCause": "Missing or invalid DATABASE_URL environment variable",
      "steps": [
        "Verify the database secret exists",
        "Check network policies allow egress to database",
        "Validate connection string format"
      ],
      "references": [
        "https://kubernetes.io/docs/concepts/configuration/secret/"
      ],
      "confidence": 0.85
    }
  },
  "summary": "1 critical issue detected related to database connectivity",
  "tokensUsed": 847
}
```

## Cost Considerations

AI API calls incur costs based on token usage. To manage costs:

1. Use `noop` provider in development and CI pipelines
2. Choose smaller models for routine checks (`gpt-4o-mini`, `claude-haiku-4-5-20251001`)
3. Limit max tokens via provider configuration (default: 2000)
4. Run AI-enhanced checks on-demand rather than continuously
5. Use the dashboard toggle to enable AI only when you need deeper analysis -- the static suggestions already include kubectl commands and common root causes
