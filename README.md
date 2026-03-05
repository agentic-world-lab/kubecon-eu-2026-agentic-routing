# Lab — Intelligent Agentic Routing with AgentGateway

This lab shows how the intelligent router classifies the domain of every LLM request and automatically selects the right model from the pool. The client sends a plain request with no routing hint; AgentGateway calls the intelligent router via ExtProc (pre-routing), which reads the body, detects the domain, and injects the `x-router-selected-model` header. AgentGateway then selects the HTTPRoute based on that header.

```
Client request  (no routing header)
        │
        ▼
AgentGateway  (port 80)
        │  gateway-level ExtProc (PreRouting) ──► Intelligent Router (:18080)
        │                                           reads body → classifies domain
        │                                           injects x-router-selected-model: gpt-4.1
        │  select_best_route() → HTTPRoute matches header
        ├─ gpt-4.1      → gpt-4-1      backend  (finance / health / legal)
        ├─ gpt-5-mini   → gpt-5-mini   backend  (science)
        └─ gpt-4.1-mini → gpt-4-1-mini backend  (technology / general)
        ▼
OpenAI API
```

The routing decision is proven two ways:
1. **Response `model` field** — OpenAI echoes back the actual model used, confirming which backend served the request.
2. **Router logs** — `[ext_proc] domain=finance selected_model=gpt-4.1` shows the intelligent router classified the prompt and injected the routing header.

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

## Step 3b — Store the HuggingFace token

The intelligent router downloads a BERT classification model from HuggingFace at first startup. A free HuggingFace account with an access token is required.

```bash
export HF_TOKEN=<your-huggingface-token>
```

```bash
kubectl create namespace intelligent-router-system 2>/dev/null || true

kubectl create secret generic huggingface-api-key \
  --from-literal="HF_TOKEN=$HF_TOKEN" \
  --namespace intelligent-router-system
```

---

## Step 4 — Deploy lab manifests

Apply resources in dependency order:

```bash
# Intelligent-router CRD, RBAC, CR config, and workload
kubectl apply -f manifests/namespace.yaml
kubectl apply -f manifests/crd-intelligent-router-config.yaml
kubectl apply -f intelligent-router/manifests/rbac.yaml
kubectl apply -f manifests/intelligent-router-config-cr.yaml
kubectl apply -f manifests/intelligent-router-service.yaml
kubectl apply -f intelligent-router/manifests/statefulset.yaml

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

## Step 6 — Port-forward AgentGateway

Keep this terminal open for all test steps:

```bash
kubectl port-forward deployment/agentgateway-proxy \
  -n agentgateway-system 8080:80
```

Requests flow: `curl → AgentGateway (:80) → gateway-level ExtProc → intelligent-router classifies body → injects x-router-selected-model → AgentGateway selects HTTPRoute → correct AgentgatewayBackend → OpenAI`.

---

## Step 7 — Scenario 1: Finance query → gpt-4.1

A prompt about **portfolio diversification, equity bonds, and dividend funds** belongs to the `finance` domain. The intelligent router scores `gpt-4.1` highest for finance: it is the only model whose `domains` list includes `finance`, and domain matching carries the highest scoring weight (0.6). The router injects the routing header automatically — no client hint needed.

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"What is the best strategy for portfolio diversification using dividend stocks, equity funds and bond investments?"}]}' \
  | jq
```

Expected output — the `gpt-4.1` backend served the request:

```json
{
  "selected_model": "gpt-4.1-2025-04-14",
  "content": "..."
}
```

**Verify the router classified the domain and injected the header:**

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=5
# [ext_proc] domain=finance selected_model=gpt-4.1
```

---

## Step 8 — Scenario 2: Technology query → gpt-4.1-mini

A prompt about **Kubernetes, Docker, and programming algorithms** belongs to the `technology` domain. Only `gpt-4.1-mini` is configured for the technology domain (domains: `["technology", "general"]`). `gpt-4.1` covers finance/health/legal and `gpt-5-mini` covers science — neither matches technology — so the intelligent router selects `gpt-4.1-mini` as the unambiguous winner, regardless of what model name the client body specified.

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4.1","messages":[{"role":"user","content":"Explain how Kubernetes StatefulSets differ from Deployments when running a database in Docker containers, focusing on the algorithm for pod scheduling."}]}' \
  | jq '{selected_model: .model, content: .choices[0].message.content[:100]}'
```

Expected output — `gpt-4.1-mini` backend served the request even though the client body said `gpt-4.1`:

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

The log line `[ext_proc] domain=... selected_model=...` appears for every request, showing which domain was detected and which model was selected by the intelligent router.

Each request produces lines like:

```
[ext_proc] domain=finance selected_model=gpt-4.1
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
kubectl delete -f intelligent-router/manifests/statefulset.yaml
kubectl delete -f manifests/intelligent-router-service.yaml
kubectl delete -f manifests/intelligent-router-config-cr.yaml
kubectl delete -f intelligent-router/manifests/rbac.yaml
kubectl delete -f manifests/crd-intelligent-router-config.yaml
kubectl delete -f manifests/namespace.yaml
kubectl delete secret openai-secret -n agentgateway-system

# Uninstall AgentGateway
helm uninstall agentgateway agentgateway-crds -n agentgateway-system
```
