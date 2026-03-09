# Lab — Intelligent Agentic Routing with AgentGateway

End-to-end lab demonstrating agentic model evaluation, intelligent domain-based routing, and optimization target switching. The system automatically evaluates LLM models, enriches them with pricing data via AI agents, and routes requests to the optimal model based on domain classification and the chosen optimization target.

```
User creates LLMBackend (spec only)
        │
        ▼
Backend Evaluator Controller ── runs MMLU-Pro evaluation ──► fills status.results
        │
        ▼
Event Watcher ── triggers Orchestrator Agent
        │
        ├─► Model Cost Agent (OpenRouter pricing)
        ├─► Energy Cost Agent (Spain electricity)
        └─► K8s Agent ── creates AgentgatewayBackend + HTTPRoute
                │
                ▼
Client request → AgentGateway → ExtProc → Intelligent Router
                                            classifies domain (BERT/keywords)
                                            selects best model (accuracy/latency/cost)
                                            injects x-router-selected-model header
                                      → HTTPRoute matches header → OpenAI
```

---

## Part 1 — Infrastructure Setup

### Step 1 — Verify cluster access

```bash
kubectl cluster-info
```

### Step 2 — Install AgentGateway

```bash
export GATEWAY_VERSION="v2.2.0-main"

kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml

helm upgrade -i agentgateway-crds oci://ghcr.io/kgateway-dev/charts/agentgateway-crds \
  --create-namespace --namespace agentgateway-system \
  --version ${GATEWAY_VERSION} --set controller.image.pullPolicy=Always

helm upgrade -i agentgateway oci://ghcr.io/kgateway-dev/charts/agentgateway \
  --namespace agentgateway-system --version ${GATEWAY_VERSION} \
  --set controller.image.pullPolicy=Always \
  --set controller.extraEnv.KGW_ENABLE_GATEWAY_API_EXPERIMENTAL_FEATURES=true

kubectl rollout status deployment/agentgateway -n agentgateway-system --timeout=120s
```

### Step 3 — Create secrets

```bash
export OPENAI_API_KEY="<your-openai-api-key>"
export HF_TOKEN="<your-huggingface-token>"
```

OpenAI secret in `agentgateway-system` (for AgentGateway backends):

```bash
kubectl create secret generic openai-secret \
  --from-literal="Authorization=Bearer $OPENAI_API_KEY" \
  -n agentgateway-system --dry-run=client -o yaml | kubectl apply -f -
```

OpenAI secret in `default` namespace (for backend evaluator jobs):

```bash
kubectl create secret generic openai-secret \
  --from-literal="Authorization=Bearer $OPENAI_API_KEY" \
  -n default --dry-run=client -o yaml | kubectl apply -f -
```

HuggingFace token (for BERT model download):

```bash
kubectl create namespace intelligent-router-system 2>/dev/null || true
kubectl create secret generic huggingface-api-key \
  --from-literal="HF_TOKEN=$HF_TOKEN" \
  -n intelligent-router-system --dry-run=client -o yaml | kubectl apply -f -
```

### Step 4 — Install Kagent

```bash
curl https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash
kagent install
```

Kagent creates a `kagent-openai` secret in the `default` namespace. The agents run in the `kagent` namespace and need access to it:

```bash
kubectl create namespace kagent 2>/dev/null || true
kubectl get secret kagent-openai -n default -o json \
  | jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp)' \
  | kubectl apply -n kagent -f -
```

### Step 5 — Deploy MCPs

```bash
kubectl apply -f manifests/mcps/openrouter-pricing-mcp.yaml
kubectl apply -f manifests/mcps/spain-electricity-cost-mcp.yaml

kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent openrouter-pricing-mcp --timeout=120s
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent spain-electricity-cost-mcp --timeout=120s
```

### Step 6 — Deploy agents

```bash
kubectl apply -f manifests/agents/
```

### Step 7 — Deploy event watcher

```bash
kubectl apply -f manifests/agent-watcher/
```

### Step 8 — Deploy backend evaluator controller

This controller watches for new LLMBackend CRs, runs MMLU-Pro evaluation jobs, and writes results into the CR status.

```bash
kubectl apply -f manifests/model-agentic-controller/install.yaml
```

### Step 9 — Deploy intelligent router

The intelligent router is an ExtProc server that classifies request domains using BERT and routes to the best model based on LLMBackend evaluation scores.

```bash
kubectl apply -f manifests/intelligent-router/namespace.yaml
kubectl apply -f manifests/intelligent-router/rbac.yaml
kubectl apply -f manifests/intelligent-router/service.yaml
kubectl apply -f manifests/intelligent-router/statefulset.yaml

kubectl rollout status statefulset/intelligent-router -n intelligent-router-system --timeout=300s
```

### Step 10 — Deploy AgentGateway gateway and ExtProc policy

```bash
kubectl apply -f manifests/agentgateway/gateway.yaml
kubectl apply -f manifests/agentgateway/agentgatewaypolicy.yaml

kubectl rollout status deployment/agentgateway-proxy -n agentgateway-system --timeout=120s
```

---

## Part 2 — User Workflow: Register Models

### Step 11 — Create LLMBackend resources

The user registers three OpenAI models. Each CR contains only the model spec — no evaluation data yet.

```bash
kubectl apply -f manifests/llmbackend/gpt-4.1-public.yaml
kubectl apply -f manifests/llmbackend/gpt-5-mini-public.yaml
kubectl apply -f manifests/llmbackend/gpt-4.1-mini-public.yaml
```

### Step 12 — Wait for agentic evaluation

The backend evaluator controller detects new LLMBackends, runs MMLU-Pro evaluation jobs, and fills `status.results` with per-category accuracy, response time, and throughput. The orchestrator agent then enriches each backend with pricing data and creates the AgentgatewayBackend + HTTPRoute resources.

Watch progress:

```bash
watch "kubectl get llmbackends -n default && echo '---' && kubectl get agentgatewaybackends -n agentgateway-system && echo '---' && kubectl get httproute -n agentgateway-system"
```

Wait until all three LLMBackends show `phase: Evaluated` and the corresponding AgentgatewayBackend + HTTPRoute resources appear. This typically takes 5-10 minutes.

### Step 13 — Verify the intelligent router loaded the backends

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=5 | grep llmwatcher
```

Expected output (models and scores may vary based on evaluation results):

```
[llmwatcher] loaded 3 LLMBackend(s): models=[gpt-4.1 gpt-5-mini gpt-4.1-mini] default=gpt-4.1-mini target=accuracy
```

> **Fallback:** If the agentic evaluation does not complete, you can apply pre-baked LLMBackend CRs with deterministic scores and manually create the AgentgatewayBackend + HTTPRoute resources:
>
> ```bash
> kubectl apply -f lab-llmbackends.yaml
> kubectl apply -f manifests/agentgateway/agentgatewaybackends.yaml
> kubectl apply -f manifests/agentgateway/httproutes.yaml
> ```

---

## Part 3 — Test Domain-Based Routing

The intelligent router classifies each request's domain using BERT and keyword matching, then selects the model with the highest accuracy for that domain. The `OPTIMIZATION_TARGET` defaults to `accuracy`.

### Step 14 — Port-forward AgentGateway

Keep this terminal open:

```bash
kubectl port-forward deployment/agentgateway-proxy -n agentgateway-system 8080:80
```

### Step 15 — Scenario 1: Finance query

A prompt about **compound interest, savings, and macroeconomic growth** is classified as `finance` by the BERT classifier (MMLU categories: `business` + `economics`). The model with the highest combined accuracy for these categories wins.

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"Calculate compound interest on a savings account with 5% annual rate and explain the GDP growth formula used in macroeconomics"}],"max_completion_tokens":50}' \
  | jq '{model: .model}'
```

The response `model` field shows which OpenAI model actually served the request. With the pre-baked scores, the expected winner is **gpt-4.1** (business=0.95, economics=0.90 — highest finance accuracy).

### Step 16 — Scenario 2: Technology query

A prompt about **Kubernetes, Docker, and algorithms** is classified as `technology` (MMLU categories: `computer science` + `engineering`).

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"Explain how Kubernetes StatefulSets differ from Deployments when running a database in Docker containers, focusing on the algorithm for pod scheduling."}],"max_completion_tokens":50}' \
  | jq '{model: .model}'
```

The winning model depends on the evaluation scores. With pre-baked scores (`lab-llmbackends.yaml`), the expected winner is **gpt-4.1-mini** (computer science=0.95, engineering=0.90). With real evaluation scores, the winner may differ.

### Step 17 — Scenario 3: Science query

A prompt about **quantum physics, chemistry, and laboratory experiments** is classified as `science` (MMLU categories: `biology` + `chemistry` + `math` + `physics`).

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"Explain the quantum chemistry behind molecular orbital theory and how laboratory experiments with hydrogen can validate the hypothesis."}],"max_completion_tokens":50}' \
  | jq '{model: .model}'
```

With pre-baked scores (`lab-llmbackends.yaml`), the expected winner is **gpt-5-mini** (biology=0.90, chemistry=0.95, math=0.95, physics=0.95). With real evaluation scores, the winner may differ.

> **Note:** gpt-5-mini requires `max_completion_tokens` instead of `max_tokens`. All curl examples in this lab use `max_completion_tokens` for compatibility with all models.

### Step 18 — Verify router logs

```bash
kubectl logs -n intelligent-router-system -l app=intelligent-router --tail=10 | grep ext_proc
```

Each request produces a log line showing the classified domain and selected model:

```
[ext_proc] domain=finance selected_model=gpt-4.1
[ext_proc] domain=technology selected_model=gpt-4.1-mini
[ext_proc] domain=science selected_model=gpt-5-mini
```

---

## Part 4 — Test Optimization Targets

The same finance query is sent three times. By changing `OPTIMIZATION_TARGET`, the winning model changes because different scoring dimensions dominate (weight 0.8 for the target, 0.1 for the others).

### Step 19 — Switch to latency optimization

```bash
kubectl set env statefulset/intelligent-router -n intelligent-router-system OPTIMIZATION_TARGET=latency
kubectl rollout restart statefulset/intelligent-router -n intelligent-router-system
kubectl rollout status statefulset/intelligent-router -n intelligent-router-system --timeout=120s
```

### Step 20 — Test: latency target + finance query

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"Calculate compound interest on a savings account with 5% annual rate and explain the GDP growth formula used in macroeconomics"}],"max_completion_tokens":50}' \
  | jq '{model: .model}'
```

With pre-baked scores, the expected winner is **gpt-5-mini** (avgResponseTime=0.5s — fastest).

### Step 21 — Switch to cost optimization

```bash
kubectl set env statefulset/intelligent-router -n intelligent-router-system OPTIMIZATION_TARGET=cost
kubectl rollout restart statefulset/intelligent-router -n intelligent-router-system
kubectl rollout status statefulset/intelligent-router -n intelligent-router-system --timeout=120s
```

### Step 22 — Test: cost target + finance query

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"any","messages":[{"role":"user","content":"Calculate compound interest on a savings account with 5% annual rate and explain the GDP growth formula used in macroeconomics"}],"max_completion_tokens":50}' \
  | jq '{model: .model}'
```

When pricing data is available (enriched by the agentic flow), the cheapest model wins. Without pricing, the router uses inverse throughput (`1/tokensPerSecond`) as a cost proxy — higher throughput = lower cost per token. In this case **gpt-5-mini** wins (180 tok/s — highest throughput).

---

## Summary

### Domain-based routing (OPTIMIZATION_TARGET=accuracy)

| Prompt domain | MMLU categories used | Expected winner |
|---------------|---------------------|-----------------|
| finance | business + economics | gpt-4.1 |
| technology | computer science + engineering | gpt-4.1-mini |
| science | biology + chemistry + math + physics | gpt-5-mini |

### Optimization target switching (same finance query)

| OPTIMIZATION_TARGET | Expected winner | Reason |
|---------------------|----------------|--------|
| accuracy | gpt-4.1 | Highest finance domain accuracy |
| latency | gpt-5-mini | Fastest response time (0.5s) |
| cost | gpt-5-mini | Highest throughput (180 tok/s) as cost proxy |

---

## Cleanup

Reset optimization target:

```bash
kubectl set env statefulset/intelligent-router -n intelligent-router-system OPTIMIZATION_TARGET=accuracy
kubectl rollout restart statefulset/intelligent-router -n intelligent-router-system
```

Remove all lab resources:

```bash
kubectl delete -f manifests/llmbackend/
kubectl delete -f manifests/agentgateway/httproutes.yaml
kubectl delete -f manifests/agentgateway/agentgatewaypolicy.yaml
kubectl delete -f manifests/agentgateway/agentgatewaybackends.yaml
kubectl delete -f manifests/agentgateway/gateway.yaml
kubectl delete -f manifests/intelligent-router/statefulset.yaml
kubectl delete -f manifests/intelligent-router/service.yaml
kubectl delete -f manifests/intelligent-router/rbac.yaml
kubectl delete -f manifests/intelligent-router/namespace.yaml
kubectl delete -f manifests/agent-watcher/
kubectl delete -f manifests/agents/
kubectl delete -f manifests/mcps/
kubectl delete -f manifests/model-agentic-controller/install.yaml
kubectl delete secret openai-secret -n agentgateway-system
kubectl delete secret openai-secret -n default

helm uninstall agentgateway agentgateway-crds -n agentgateway-system
```
