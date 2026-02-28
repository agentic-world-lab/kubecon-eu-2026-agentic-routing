# Lab — Intelligent Agentic Routing with AgentGateway

This lab shows how the intelligent router classifies the domain of every LLM request and automatically selects the right model from the pool. The client sets the `x-vsr-selected-model` header to instruct AgentGateway which backend to use; the router independently confirms the same selection via domain classification.

```
Client request
        │  x-vsr-selected-model: gpt-4.1   (client instructs routing)
        ▼
AgentGateway  ──ExtProc──►  intelligent-router
        │                     classifies domain from prompt keywords
        │                     confirms: domain=finance → gpt-4.1
        │
        ▼  HTTPRoute matches header
        ├─ x-vsr-selected-model: gpt-4.1      → gpt-4-1      backend
        ├─ x-vsr-selected-model: gpt-5-mini   → gpt-5-mini   backend
        └─ x-vsr-selected-model: gpt-4.1-mini → gpt-4-1-mini backend
```

The routing decision is proven two ways:
1. **Response `model` field** — OpenAI echoes back the actual model used, confirming which backend served the request.
2. **Router logs** — `domain=finance selected_model=gpt-4.1` shows the intelligent router independently classified the same prompt to the same model.

> **Building the router image**: see [intelligent-router/README.md](intelligent-router/README.md).
> The lab uses the pre-built image `antonioberben/intelligent-router:latest`.

---

## Prerequisites

| Tool | Install |
|------|---------|
| Docker Desktop (with buildx) | [docs.docker.com](https://docs.docker.com/desktop/) |
| kubectl ≥ 1.28 | `brew install kubectl` |
| helm ≥ 3.14 | `brew install helm` |
| jq | `brew install jq` |
| OpenAI API key | [platform.openai.com](https://platform.openai.com/api-keys) |

| Existing Kubernetes cluster | Must be running and accessible via kubectl |

```bash
kubectl version --client && helm version --short && jq --version
```

---

## Step 1 — Verify cluster access

Ensure your Kubernetes cluster is running and accessible:

```bash
kubectl cluster-info
```

---

## Step 2 — Install AgentGateway

```bash
# Gateway API CRDs
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml

# AgentGateway CRDs
helm upgrade -i agentgateway-crds \
  oci://ghcr.io/kgateway-dev/charts/agentgateway-crds \
  --create-namespace \
  --namespace agentgateway-system \
  --version v2.2.1 \
  --set controller.image.pullPolicy=Always

# AgentGateway control plane
helm upgrade -i agentgateway \
  oci://ghcr.io/kgateway-dev/charts/agentgateway \
  --namespace agentgateway-system \
  --version v2.2.1 \
  --set controller.image.pullPolicy=Always

kubectl rollout status deployment/agentgateway -n agentgateway-system --timeout=120s
```

---

## Step 3 — Store the OpenAI API key

```bash
export OPENAI_API_KEY=<your-openai-api-key>
```

```bash
kubectl create secret generic openai-secret \
  --from-literal="Authorization=Bearer $OPENAI_API_KEY" \
  --namespace agentgateway-system
```

---

## Step 4 — Deploy lab manifests

Apply resources in dependency order:

```bash
# Intelligent-router namespace, CRD, and workload
kubectl apply -f manifests/namespace.yaml
kubectl apply -f manifests/crd-intelligent-router-config.yaml
kubectl apply -f manifests/intelligent-router-pool-configmap.yaml
kubectl apply -f manifests/intelligent-router-route-configmap.yaml
kubectl apply -f manifests/intelligent-router-service.yaml
kubectl apply -f manifests/intelligent-router-statefulset.yaml

# AgentGateway resources
kubectl apply -f manifests/gateway.yaml
kubectl apply -f manifests/agentgatewaybackends.yaml
kubectl apply -f manifests/agentgatewaypolicy.yaml
kubectl apply -f manifests/httproutes.yaml
```

---

## Step 5 — Verify everything is running

```bash
# Intelligent router
kubectl rollout status statefulset/intelligent-router \
  -n intelligent-router-system --timeout=60s

kubectl get pods -n intelligent-router-system
# NAME                     READY   STATUS    RESTARTS   AGE
# intelligent-router-0     1/1     Running   0          30s

# AgentGateway proxy
kubectl rollout status deployment/agentgateway-proxy \
  -n agentgateway-system --timeout=120s

# Backends (should show Accepted)
kubectl get agentgatewaybackends -n agentgateway-system
# NAME
# gpt-4-1
# gpt-5-mini
# gpt-4-1-mini

# HTTPRoute (should show Accepted / Programmed)
kubectl get httproute -n agentgateway-system
```

---

## Step 6 — Port-forward the gateway

Keep this terminal open for all test steps:

```bash
kubectl port-forward deployment/agentgateway-proxy \
  -n agentgateway-system 8080:80
```

---

## Step 7 — Scenario 1: Finance query → gpt-4.1

A prompt about **portfolio diversification, equity bonds, and dividend funds** belongs to the `finance` domain. The intelligent router scores `gpt-4.1` highest for finance (dedicated domain, highest quality weight). The client sets the header to route to the `gpt-4-1` backend.

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'x-vsr-selected-model: gpt-4.1' \
  -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"What is the best strategy for portfolio diversification using dividend stocks, equity funds and bond investments?"}]}' \
  | jq '{selected_model: .model, content: .choices[0].message.content[:100]}'
```

Expected output — the `gpt-4.1` backend served the request:

```json
{
  "selected_model": "gpt-4.1-2025-04-14",
  "content": "..."
}
```

**Verify the router independently classified the same domain:**

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=5
# [ext_proc] domain=finance selected_model=gpt-4.1
```

---

## Step 8 — Scenario 2: Technology query → gpt-4.1-mini

A prompt about **Kubernetes, Docker, and programming algorithms** belongs to the `technology` domain. Both `gpt-4.1-mini` and `gpt-5-mini` cover technology, but `gpt-4.1-mini` wins on cost (0.0004 vs 0.001 per input token) and latency (85 ms vs 100 ms).

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'x-vsr-selected-model: gpt-4.1-mini' \
  -d '{"model":"gpt-4.1","messages":[{"role":"user","content":"Explain how Kubernetes StatefulSets differ from Deployments when running a database in Docker containers, focusing on the algorithm for pod scheduling."}]}' \
  | jq '{selected_model: .model, content: .choices[0].message.content[:100]}'
```

Expected output — `gpt-4.1-mini` backend served the request even though the client sent `gpt-4.1`:

```json
{
  "selected_model": "gpt-4.1-mini-2025-04-14",
  "content": "..."
}
```

**Verify the router classification:**

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=5
# [ext_proc] domain=technology selected_model=gpt-4.1-mini
```

---

## Step 9 — Scenario 3: Science query → gpt-5-mini

A prompt about **quantum physics, chemistry, and laboratory experiments** belongs to the `science` domain. Only `gpt-5-mini` is configured for science in the pool, making it the unambiguous winner.

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'x-vsr-selected-model: gpt-5-mini' \
  -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"Explain the quantum chemistry behind molecular orbital theory and how laboratory experiments with hydrogen can validate the hypothesis."}]}' \
  | jq '{selected_model: .model, content: .choices[0].message.content[:100]}'
```

Expected output — `gpt-5-mini` backend served the request:

```json
{
  "selected_model": "gpt-5-mini-2025-08-07",
  "content": "..."
}
```

**Verify the router classification:**

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=5
# [ext_proc] domain=science selected_model=gpt-5-mini
```

---

## Step 10 — Observe router decisions in logs

In a second terminal, watch the router live:

```bash
kubectl logs -n intelligent-router-system \
  -l app=intelligent-router -f --tail=30
```

Each request produces lines like:

```
[ext_proc] new stream
[ext_proc] RequestHeaders  api_key=...sk-xxxx
[ext_proc] api_key=...sk-xxxx budget_pressure=0.000
[ext_proc] domain=finance selected_model=gpt-4.1
[ext_proc] api_key=...sk-xxxx estimated_input=21 new_total=21
```

Check Prometheus metrics (in another terminal):

```bash
kubectl port-forward -n intelligent-router-system \
  svc/intelligent-router 9091:9091 &

curl -s http://localhost:9091/metrics | grep intelligent_router_decisions_total
# intelligent_router_decisions_total{domain="finance",selected_model="gpt-4.1"} 1
# intelligent_router_decisions_total{domain="technology",selected_model="gpt-4.1-mini"} 1
# intelligent_router_decisions_total{domain="science",selected_model="gpt-5-mini"} 1
```

---

## Routing summary

| Prompt keywords | Domain detected | Model selected | Backend |
|-----------------|----------------|----------------|---------|
| portfolio, dividend, equity, bond | finance | gpt-4.1 | gpt-4-1 |
| kubernetes, docker, programming, algorithm | technology | gpt-4.1-mini | gpt-4-1-mini |
| quantum, physics, chemistry, laboratory | science | gpt-5-mini | gpt-5-mini |

---

## Cleanup

```bash
# Remove lab manifests
kubectl delete -f manifests/httproutes.yaml
kubectl delete -f manifests/agentgatewaypolicy.yaml
kubectl delete -f manifests/agentgatewaybackends.yaml
kubectl delete -f manifests/gateway.yaml
kubectl delete -f manifests/intelligent-router-statefulset.yaml
kubectl delete -f manifests/intelligent-router-service.yaml
kubectl delete -f manifests/intelligent-router-route-configmap.yaml
kubectl delete -f manifests/intelligent-router-pool-configmap.yaml
kubectl delete -f manifests/crd-intelligent-router-config.yaml
kubectl delete -f manifests/namespace.yaml
kubectl delete secret openai-secret -n agentgateway-system

# Uninstall AgentGateway
helm uninstall agentgateway agentgateway-crds -n agentgateway-system
```
