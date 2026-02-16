# Troubleshooting Guide

Common issues and solutions for the kube-assist operator.

## Dashboard Not Loading

### Symptoms
- Dashboard returns 404 or connection refused on port 9090
- No dashboard UI appears

### Solutions

**1. Verify dashboard is enabled**

```bash
# Check if --enable-dashboard flag is set
kubectl get deployment kube-assist-controller-manager -n kube-assist-system -o yaml | grep -A5 "args:"
```

The args should include `--enable-dashboard`. If missing, update the deployment or Helm values:

```yaml
# values.yaml
dashboard:
  enabled: true
```

**2. Check port accessibility**

```bash
# Port-forward to test locally
kubectl port-forward -n kube-assist-system deployment/kube-assist-controller-manager 9090:9090

# Test connection
curl http://localhost:9090/
```

**3. Check pod logs for errors**

```bash
kubectl logs -n kube-assist-system deployment/kube-assist-controller-manager | grep -i dashboard
```

Look for:
- `starting dashboard server` - confirms dashboard is starting
- `dashboard server error` - indicates startup failure

**4. Verify service configuration**

```bash
kubectl get svc -n kube-assist-system | grep dashboard
```

**5. If auth token is set, configure TLS (or explicit local override)**

When `DASHBOARD_AUTH_TOKEN` is configured, the dashboard now requires TLS cert/key by default.

Helm example:

```yaml
dashboard:
  enabled: true
  authTokenSecretRef:
    name: kube-assist-dashboard-auth
    key: auth-token
  tls:
    enabled: true
    secretName: kube-assist-dashboard-tls
```

For local development only, you can explicitly allow insecure HTTP:

```yaml
dashboard:
  allowInsecureHttp: true
```

---

## Checkers Returning Empty Results

### Symptoms
- Health checks complete but show 0 healthy resources
- Issues array is empty when problems exist

### Solutions

**1. Verify namespace scope**

Checkers only scan namespaces specified in the request or all non-system namespaces by default.

```bash
# Check which namespaces are being scanned
kubectl logs -n kube-assist-system deployment/kube-assist-controller-manager | grep -i namespace
```

For TroubleshootRequest, specify the namespace:

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TroubleshootRequest
metadata:
  name: check-myapp
  namespace: my-namespace  # Resources checked here
spec:
  target:
    kind: Deployment
    name: my-app
```

For TeamHealthRequest, specify namespaces explicitly:

```yaml
apiVersion: assist.cluster.local/v1alpha1
kind: TeamHealthRequest
metadata:
  name: health-check
spec:
  scope:
    namespaces:
      - production
      - staging
```

**2. Check RBAC permissions**

The operator needs permissions to read resources it checks.

```bash
# Verify ClusterRole permissions
kubectl get clusterrole kube-assist-manager-role -o yaml
```

Required permissions for core checkers:
- `pods`, `deployments`, `statefulsets`, `daemonsets` - workload checker
- `persistentvolumeclaims` - PVC checker
- `resourcequotas` - quota checker
- `networkpolicies` - network policy checker

Optional checker permissions:
- `secrets` - required only when `checkers.secrets.enabled=true`

For Flux checkers:
- `helmreleases.helm.toolkit.fluxcd.io`
- `kustomizations.kustomize.toolkit.fluxcd.io`
- `gitrepositories.source.toolkit.fluxcd.io`

**3. Enable debug logging**

```bash
# Add to deployment args
--zap-log-level=debug
```

Or run locally:

```bash
make run ARGS="--zap-log-level=debug"
```

---

## Flux Checkers Skipped

### Symptoms
- `helmreleases`, `kustomizations`, or `gitrepositories` checkers show errors
- Log shows "checker is not supported in this environment"

### Solutions

**1. Verify Flux CRDs are installed**

```bash
kubectl get crd | grep -E "(helmreleases|kustomizations|gitrepositories)"
```

Expected output:
```
gitrepositories.source.toolkit.fluxcd.io
helmreleases.helm.toolkit.fluxcd.io
kustomizations.kustomize.toolkit.fluxcd.io
```

If missing, install Flux:

```bash
flux install
```

**2. Verify Flux API is added to scheme**

The operator automatically registers Flux types. Check startup logs:

```bash
kubectl logs -n kube-assist-system deployment/kube-assist-controller-manager | head -50
```

Look for:
```
Registered checkers [...helmreleases kustomizations gitrepositories...]
```

**3. Check API server access**

```bash
# Test if operator can list Flux resources
kubectl auth can-i list helmreleases.helm.toolkit.fluxcd.io --as=system:serviceaccount:kube-assist-system:kube-assist-controller-manager
```

---

## AI Suggestions Not Appearing

### Symptoms
- Health check results show static suggestions only (kubectl commands but no "AI Analysis:" section)
- No "AI Analysis:" prefix in suggestions
- Logs show AI errors

### Solutions

**1. Enable AI via the Dashboard**

Open the dashboard, find the AI Settings panel, toggle AI on, select a provider/model, and save. No restart needed. The dashboard calls `POST /api/settings/ai` to reconfigure the shared AI Manager at runtime.

Important: direct `apiKey` in `POST /api/settings/ai` is rejected. Configure API keys via Secret/env (`KUBE_ASSIST_AI_API_KEY`).

Verify the settings took effect:

```bash
curl http://localhost:9090/api/settings/ai
# Should show: {"enabled":true,"provider":"anthropic","hasApiKey":true,"providerReady":true}
```

**2. Verify AI is enabled (CLI/Helm)**

Check deployment args include `--enable-ai`:

```bash
kubectl get deployment kube-assist-controller-manager -n kube-assist-system -o yaml | grep enable-ai
```

**3. Configure API key**

Environment variable:
```yaml
env:
  - name: KUBE_ASSIST_AI_API_KEY
    valueFrom:
      secretKeyRef:
        name: kube-assist-ai
        key: api-key
  - name: KUBE_ASSIST_AI_PROVIDER
    value: "anthropic"
```

Create the secret:
```bash
kubectl create secret generic kube-assist-ai \
  --from-literal=api-key=your-api-key-here \
  -n kube-assist-system
```

**4. Check API key secret exists**

```bash
kubectl get secret kube-assist-ai -n kube-assist-system
```

**5. Verify network egress**

The operator must reach external AI APIs:
- Anthropic: `api.anthropic.com`
- OpenAI: `api.openai.com`

Check network policies:

```bash
kubectl get networkpolicy -n kube-assist-system
```

If egress is restricted, add an exception:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-ai-egress
  namespace: kube-assist-system
spec:
  podSelector:
    matchLabels:
      control-plane: controller-manager
  policyTypes:
    - Egress
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - port: 443
          protocol: TCP
```

**6. Check provider logs**

```bash
kubectl logs -n kube-assist-system deployment/kube-assist-controller-manager | grep -i "ai"
```

Look for:
- `AI provider initialized` or `AI settings updated via dashboard` - success
- `failed to create AI provider` or `Failed to reconfigure AI provider` - configuration error

---

## High Memory Usage

### Symptoms
- Pod OOMKilled
- High memory consumption in metrics

### Solutions

**1. Increase memory limits**

```yaml
# values.yaml
resources:
  limits:
    memory: 512Mi  # Increase from default
  requests:
    memory: 256Mi
```

Or patch directly:

```bash
kubectl patch deployment kube-assist-controller-manager -n kube-assist-system \
  --type='json' -p='[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"512Mi"}]'
```

**2. Reduce namespace scope**

Limit which namespaces are scanned in TeamHealthRequest:

```yaml
spec:
  namespaces:
    - production  # Only scan specific namespaces
```

**3. Reduce concurrent checks**

If running many health requests simultaneously, stagger them with different intervals.

**4. Check for memory leaks**

```bash
# Monitor memory over time
kubectl top pod -n kube-assist-system -l control-plane=controller-manager --containers
```

---

## NLQ Chat Not Available

### Symptoms
- Chat panel is disabled in the UI
- `POST /api/chat` returns `503 Chat is not available`
- `POST /api/chat` returns `400 clusterId is required when multiple clusters are available`

### Solutions

**1. Enable chat and AI together**

```yaml
dashboard:
  chat:
    enabled: true
ai:
  enabled: true
```

Chat is gated by both flags.

**2. Verify AI provider is ready**

```bash
curl http://localhost:9090/api/settings/ai
# Expect hasApiKey=true and providerReady=true
```

**3. Include `clusterId` in multi-cluster mode**

When dashboard tracks multiple clusters, `/api/chat` requires `clusterId` in request JSON.

```json
{
  "message": "Summarize current critical issues",
  "clusterId": "cluster-a"
}
```

**4. Check session limits**

If you hit max sessions, `/api/chat` returns `503 maximum concurrent chat sessions reached`.
Tune `dashboard.chat.maxSessions` and `dashboard.chat.sessionTTL` as needed.

---

## Debug Logging

Enable verbose logging to diagnose issues:

```bash
# Via flag
--zap-log-level=debug

# Full args example
--enable-dashboard --zap-log-level=debug --zap-devel=true
```

Log level options:
- `info` - default, standard operational logs
- `debug` - verbose, includes checker execution details
- `error` - errors only

For development mode with stack traces:

```bash
--zap-devel=true
```

---

## AI Runtime Configuration Not Working

### Symptoms
- `POST /api/settings/ai` returns 400/500
- Dashboard save button shows error toast
- AI settings revert after saving

### Solutions

**1. Check the response body**

The API returns the error message in the response body:

```bash
curl -X POST http://localhost:9090/api/settings/ai \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"provider":"anthropic","model":"claude-haiku-4-5-20251001"}'
```

If the body includes `apiKey`, remove it. The API now rejects direct key submission.

**2. Verify provider name is valid**

Provider must be one of: `anthropic`, `openai`, `noop`. The API rejects invalid names with a 400 status.

**3. Check operator logs**

```bash
kubectl logs -n kube-assist-system deployment/kube-assist-controller-manager | grep -i "AI settings"
```

Look for `AI settings updated via dashboard (Manager)` for successful updates.

---

## Common Error Messages

| Error | Cause | Solution |
|-------|-------|----------|
| `checker "X" is not supported` | CRD not installed | Install required CRD (e.g., Flux) |
| `unable to list namespaces` | RBAC missing | Add `list namespaces` permission |
| `failed to get pods` | RBAC missing | Add `get pods` permission |
| `AI provider not configured` | Missing API key | Set via Secret/env (`KUBE_ASSIST_AI_API_KEY`) |
| `Failed to reconfigure AI provider` | Invalid provider config | Check provider name and API key |
| `Invalid provider` | Bad POST body | Provider must be `anthropic`, `openai`, or `noop` |
| `dashboard server error` | Port conflict | Check port 9090 availability |

---

## Getting Help

1. Check operator logs: `kubectl logs -n kube-assist-system deployment/kube-assist-controller-manager`
2. Check events: `kubectl get events -n kube-assist-system --sort-by='.lastTimestamp'`
3. Open an issue: https://github.com/osagberg/kube-assist-operator/issues
