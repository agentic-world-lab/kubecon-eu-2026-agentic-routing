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

The YAML is in [`lab-llmbackends.yaml`](lab-llmbackends.yaml). Apply them:

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
