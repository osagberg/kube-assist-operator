# AI Integration

kube-assist can enhance health check suggestions using AI providers to deliver context-aware remediation guidance.

## Overview

When enabled, the AI integration:

1. Collects detected issues from health checks
2. Sanitizes data to remove sensitive information
3. Sends context to the configured AI provider
4. Returns enhanced suggestions with root cause analysis and remediation steps

AI enhancement runs after standard health checks complete. Results include both static suggestions and AI-generated insights.

## Configuration

### CLI Flags (Operator)

```bash
/manager \
  --enable-ai \
  --ai-provider=anthropic \
  --ai-model=claude-3-sonnet-20240229 \
  --ai-api-key=sk-ant-...
```

| Flag | Description | Default |
|------|-------------|---------|
| `--enable-ai` | Enable AI-powered suggestions | `false` |
| `--ai-provider` | Provider: `anthropic`, `openai`, `noop` | `noop` |
| `--ai-api-key` | API key (prefer env var or secret) | - |
| `--ai-model` | Model name (uses provider default if empty) | - |

### Environment Variables

```bash
export KUBE_ASSIST_AI_PROVIDER=anthropic
export KUBE_ASSIST_AI_API_KEY=sk-ant-api03-...
export KUBE_ASSIST_AI_MODEL=claude-3-sonnet-20240229
export KUBE_ASSIST_AI_ENDPOINT=https://api.anthropic.com/v1/messages  # optional
```

The operator checks `KUBE_ASSIST_AI_API_KEY` if no API key is provided via flag.

### Helm Values

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
  --from-literal=api-key=sk-ant-api03-...
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
| Default model | `claude-3-sonnet-20240229` |
| Default endpoint | `https://api.anthropic.com/v1/messages` |
| API version | `2023-06-01` |

Recommended models:
- `claude-3-sonnet-20240229` - Balanced performance and cost
- `claude-3-opus-20240229` - Highest quality analysis

### OpenAI

| Setting | Value |
|---------|-------|
| Provider name | `openai` |
| Default model | `gpt-4` |
| Default endpoint | `https://api.openai.com/v1/chat/completions` |

Recommended models:
- `gpt-4` - Best analysis quality
- `gpt-4-turbo` - Faster with larger context
- `gpt-3.5-turbo` - Lower cost option

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

## Example Output

### Without AI

```
workloads (2 healthy, 1 critical)
   [Critical] default/api-server
     Pod api-server-abc123 in CrashLoopBackOff (5 restarts)
     -> Check pod logs and events for crash reason
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

1. Use `noop` provider in development
2. Choose smaller models for routine checks (`gpt-3.5-turbo`, `claude-3-sonnet`)
3. Limit max tokens via provider configuration (default: 2000)
4. Run AI-enhanced checks on-demand rather than continuously
