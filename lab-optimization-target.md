# Lab — Optimization Target: Accuracy vs Latency vs Cost

This lab demonstrates how the `OPTIMIZATION_TARGET` environment variable changes which model the intelligent router selects — even when the same prompt is sent. Three LLMBackend CRs provide per-model evaluation scores for accuracy, latency, and cost. The router uses these scores to pick the best model for the detected domain.

We send the **same finance query** three times, each time with a different optimization target, and observe that a different model wins:

| OPTIMIZATION_TARGET | Winner for finance query | Why |
|---------------------|--------------------------|-----|
| `accuracy` | **gpt-4.1** | Highest accuracy in business + economics categories |
| `latency` | **gpt-5-mini** | Fastest average response time (0.5 s) |
| `cost` | **gpt-4.1-mini** | Cheapest total pricing ($2.00 / 1M tokens) |

---

## Prerequisites

Complete Steps 1–3 from the main [README.md](README.md):

- Kubernetes cluster running and accessible
- AgentGateway installed (`agentgateway-system` namespace)
- OpenAI API key secret created
- HuggingFace token secret created

---

## Step 1 — Create LLMBackend CRD and CRs

First, create the LLMBackend Custom Resource Definition:

```bash
kubectl apply -f manifests/crd-llmbackend.yaml
```

These three CRs simulate evaluated models with deliberately skewed scores so each optimization target produces a different winner.

The YAML is in `lab-llmbackends.yaml`:

```yaml
apiVersion: edgecloudlabs.edgecloudlabs.com/v1alpha1
kind: LLMBackend
metadata:
  name: gpt-4-1-backend
  namespace: default
spec:
  apiKeySecretRef:
    name: openai-secret
  deployment: remote
  model: gpt-4.1
status:
  phase: Evaluated
  results:
    avgResponseTime: "5.0000"
    tokensPerSecond: "40.00"
    overallAccuracy: "0.7821"
    categoryAccuracy:
      biology: "0.8500"
      business: "0.9500"
      chemistry: "0.8000"
      computer science: "0.7000"
      economics: "0.9000"
      engineering: "0.6500"
      health: "0.8000"
      history: "0.9000"
      law: "0.8500"
      math: "0.7500"
      other: "0.6000"
      philosophy: "0.7000"
      physics: "0.7500"
      psychology: "0.7500"
    pricing:
      prompt: "5.00"
      completion: "15.00"
---
apiVersion: edgecloudlabs.edgecloudlabs.com/v1alpha1
kind: LLMBackend
metadata:
  name: gpt-5-mini-backend
  namespace: default
spec:
  apiKeySecretRef:
    name: openai-secret
  deployment: remote
  model: gpt-5-mini
status:
  phase: Evaluated
  results:
    avgResponseTime: "0.5000"
    tokensPerSecond: "180.00"
    overallAccuracy: "0.6607"
    categoryAccuracy:
      biology: "0.7000"
      business: "0.6500"
      chemistry: "0.7500"
      computer science: "0.8000"
      economics: "0.7000"
      engineering: "0.7500"
      health: "0.6000"
      history: "0.5500"
      law: "0.5000"
      math: "0.8000"
      other: "0.6500"
      philosophy: "0.5500"
      physics: "0.8500"
      psychology: "0.5500"
    pricing:
      prompt: "3.00"
      completion: "10.00"
---
apiVersion: edgecloudlabs.edgecloudlabs.com/v1alpha1
kind: LLMBackend
metadata:
  name: gpt-4-1-mini-backend
  namespace: default
spec:
  apiKeySecretRef:
    name: openai-secret
  deployment: remote
  model: gpt-4.1-mini
status:
  phase: Evaluated
  results:
    avgResponseTime: "2.0000"
    tokensPerSecond: "90.00"
    overallAccuracy: "0.4893"
    categoryAccuracy:
      biology: "0.5000"
      business: "0.5000"
      chemistry: "0.4500"
      computer science: "0.6000"
      economics: "0.4500"
      engineering: "0.5500"
      health: "0.5000"
      history: "0.4000"
      law: "0.3500"
      math: "0.5500"
      other: "0.5000"
      philosophy: "0.4500"
      physics: "0.5000"
      psychology: "0.4500"
    pricing:
      prompt: "0.50"
      completion: "1.50"
```

Apply them:

```bash
kubectl apply -f lab-llmbackends.yaml
```

Verify all three are `Evaluated`:

```bash
kubectl get llmbackends -A
# NAME                  AGE
# gpt-4-1-backend       5s
# gpt-5-mini-backend    5s
# gpt-4-1-mini-backend  5s
```

---

## Step 2 — Deploy AgentGateway resources

Apply the gateway, backends, routes, and ExtProc policy:

```bash
kubectl apply -f manifests/gateway.yaml
kubectl apply -f manifests/agentgatewaybackends.yaml
kubectl apply -f manifests/agentgatewaypolicy.yaml
kubectl apply -f manifests/httproutes.yaml
```

---

## Step 3 — Deploy intelligent router and RBAC

```bash
kubectl apply -f manifests/namespace.yaml
kubectl apply -f intelligent-router/manifests/rbac.yaml
kubectl apply -f manifests/intelligent-router-service.yaml
ll
```

Wait for it to be ready:

```bash
kubectl rollout status statefulset/intelligent-router \
  -n intelligent-router-system --timeout=120s
```

Verify the router discovered the LLMBackend CRs:

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=5
# [llmwatcher] loaded 3 LLMBackend(s): models=[gpt-4.1 gpt-5-mini gpt-4.1-mini] default=gpt-4.1-mini target=accuracy
```

---

## Step 4 — Port-forward AgentGateway

Keep this terminal open:

```bash
kubectl port-forward deployment/agentgateway-proxy \
  -n agentgateway-system 8080:80
```

---

## Step 5 — Test: OPTIMIZATION_TARGET=accuracy

The statefulset defaults to `OPTIMIZATION_TARGET=accuracy`. Send a finance query:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"What is the best strategy for portfolio diversification using dividend stocks and equity funds?"}],"max_tokens":50}' \
  | jq '{model: .model}'
```

Expected output — **gpt-4.1** wins (highest finance accuracy: business=0.95, economics=0.90):

```json
{
  "model": "gpt-4.1-2025-04-14"
}
```

Verify in router logs:

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=3
# [ext_proc] domain=finance selected_model=gpt-4.1
```

---

## Step 6 — Switch to OPTIMIZATION_TARGET=latency

Patch the statefulset environment variable and restart:

```bash
kubectl set env statefulset/intelligent-router \
  -n intelligent-router-system \
  OPTIMIZATION_TARGET=latency

kubectl rollout restart statefulset/intelligent-router \
  -n intelligent-router-system

kubectl rollout status statefulset/intelligent-router \
  -n intelligent-router-system --timeout=120s
```

Verify:

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=3
# [llmwatcher] loaded 3 LLMBackend(s): ... target=latency
```

---

## Step 7 — Test: OPTIMIZATION_TARGET=latency

Same finance query:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"What is the best strategy for portfolio diversification using dividend stocks and equity funds?"}],"max_tokens":50}' \
  | jq '{model: .model}'
```

Expected output — **gpt-5-mini** wins (fastest response time: 0.5 s):

```json
{
  "model": "gpt-5-mini-2025-08-07"
}
```

> **Note:** gpt-5-mini requires `max_completion_tokens` instead of `max_tokens`. If you get an error about unsupported parameter, change `"max_tokens":50` to `"max_completion_tokens":50`.

---

## Step 8 — Switch to OPTIMIZATION_TARGET=cost

```bash
kubectl set env statefulset/intelligent-router \
  -n intelligent-router-system \
  OPTIMIZATION_TARGET=cost

kubectl rollout restart statefulset/intelligent-router \
  -n intelligent-router-system

kubectl rollout status statefulset/intelligent-router \
  -n intelligent-router-system --timeout=120s
```

Verify:

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=3
# [llmwatcher] loaded 3 LLMBackend(s): ... target=cost
```

---

## Step 9 — Test: OPTIMIZATION_TARGET=cost

Same finance query:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"What is the best strategy for portfolio diversification using dividend stocks and equity funds?"}],"max_tokens":50}' \
  | jq '{model: .model}'
```

Expected output — **gpt-4.1-mini** wins (cheapest: $0.50 prompt + $1.50 completion = $2.00 total):

```json
{
  "model": "gpt-4.1-mini-2025-04-14"
}
```

---

## Summary

The **same prompt** routed to **three different models** depending on the optimization target:

| OPTIMIZATION_TARGET | Selected model | Reason |
|---------------------|----------------|--------|
| `accuracy` | gpt-4.1 | Finance accuracy 0.925 (business=0.95, economics=0.90) |
| `latency` | gpt-5-mini | Response time 0.5 s (vs 2.0 s and 5.0 s) |
| `cost` | gpt-4.1-mini | Total cost $2.00 (vs $13.00 and $20.00) |

The intelligent router reads these scores from the `LLMBackend` CR status, classifies the prompt domain via keyword/BERT matching, and applies weighted scoring with the optimization target controlling which dimension dominates (weight 0.8 for the target vs 0.1 for the others).

---

## Cleanup

```bash
kubectl delete -f lab-llmbackends.yaml
```

To reset the optimization target back to accuracy:

```bash
kubectl set env statefulset/intelligent-router \
  -n intelligent-router-system \
  OPTIMIZATION_TARGET=accuracy

kubectl rollout restart statefulset/intelligent-router \
  -n intelligent-router-system
```
