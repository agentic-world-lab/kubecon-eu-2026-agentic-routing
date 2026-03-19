# kubecon-eu-2026-agentic-routing
Resources for talk at KubeCon + CloudNativeCon Europe 2026 Amsterdam: Intelligent Routing for Optimized Inference

[![Intelligent Routing for Optimized Inference - Demo](https://img.youtube.com/vi/5ds6_J8qu7Q/0.jpg)](https://www.youtube.com/watch?v=5ds6_J8qu7Q)

## Overview

This lab shows how the intelligent router classifies the domain of every LLM request and automatically selects the right model from the pool. The client sends a plain request with no routing hint; AgentGateway calls the intelligent router via ExtProc (pre-routing), which reads the body, detects the domain, and injects the `x-router-selected-model` header. AgentGateway then selects the HTTPRoute based on that header.

```bash
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

## Steps

> And yes, you can install the whole application by running:
```bash
export OPENAI_API_KEY="sk-…"
export HF_TOKEN="hf_…" 
curl -sL https://raw.githubusercontent.com/agentic-world-lab/kubecon-eu-2026-agentic-routing/main/install.sh | bash
```

If you want to go step by step, you can follow the guide below:

### Step 1 — Verify cluster access

Ensure your Kubernetes cluster is running and accessible:

```bash
kubectl cluster-info
```

### Step 2 — Install AgentGateway

```bash
export GATEWAY_VERSION="v2.2.0-main"

# Gateway API CRDs
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml


# AgentGateway CRDs
helm upgrade -i agentgateway-crds oci://ghcr.io/kgateway-dev/charts/agentgateway-crds \
--create-namespace --namespace agentgateway-system \
--version ${GATEWAY_VERSION} \
--set controller.image.pullPolicy=Always

# AgentGateway control plane
helm upgrade -i agentgateway oci://ghcr.io/kgateway-dev/charts/agentgateway \
  --namespace agentgateway-system \
  --version ${GATEWAY_VERSION} \
  --set controller.image.pullPolicy=Always \
  --set controller.extraEnv.KGW_ENABLE_GATEWAY_API_EXPERIMENTAL_FEATURES=true
```

### Step 2b — Create a gateway

```bash
kubectl apply -f manifests/agentgateway/gateway.yaml
kubectl apply -f manifests/agentgateway/agentgatewaypolicy.yaml
```

### Step 3 — Store the OpenAI API key

```bash
export OPENAI_API_KEY="<your_openai_api_key_here>"
```

```bash
kubectl create secret generic openai-secret \
  --from-literal="Authorization=Bearer $OPENAI_API_KEY" \
  --namespace agentgateway-system

kubectl create secret generic openai-secret \
  --from-literal="OPENAI_API_KEY=Bearer $OPENAI_API_KEY" \
  --namespace kagent

kubectl create secret generic openai-secret \
  --from-literal="OPENAI_API_KEY=Bearer $OPENAI_API_KEY" \
  --namespace intelligent-router-system

```

### Step 3b — Store the HuggingFace token

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


### Step 4 — Install Kagent


```bash
curl https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash
kagent install
```

### Step 5 — Deploy lab manifests

#### 5.1 Create the MCPs
```bash
# Pricing MCP
kubectl apply -f manifests/mcps/openrouter-pricing-mcp.yaml

# Electricity cost MCP
kubectl apply -f manifests/mcps/spain-electricity-cost-mcp.yaml
```

Wait for the mcp to be ready
```bash
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent openrouter-pricing-mcp
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent spain-electricity-cost-mcp
```

#### 5.2 Create the configuration agents

```bash
kubectl apply -f manifests/agents/k8s-agent.yaml
kubectl apply -f manifests/agents/orchestrator-agent.yaml
kubectl apply -f manifests/agents/eval-job-agent.yaml
kubectl apply -f manifests/agents/model-cost-agent.yaml
kubectl apply -f manifests/agents/sp-electricity-cost-agent.yaml
```

## Step 6 — Install the Backend Evaluation Operator

```bash
kubectl apply -f manifests/model-agentic-controller/crd-agentic-controller.yaml
```

## Step 7 — Deploy the Intelligent Router

```bash
# Intelligent-router CRD, RBAC, CR config, and workload
kubectl apply -f manifests/intelligent-router/namespace.yaml
kubectl apply -f manifests/intelligent-router/rbac.yaml
kubectl apply -f manifests/intelligent-router/service.yaml
kubectl apply -f manifests/intelligent-router/statefulset.yaml
```

## Step 8 — Deploy monitoring (Prometheus + Grafana + OTel Collector)

Deploy the observability stack to scrape metrics from AgentGateway and the Intelligent Router:

```bash
kubectl apply -f manifests/observability/namespace.yaml
kubectl apply -f manifests/observability/prometheus.yaml
kubectl apply -f manifests/observability/otel-collector.yaml
kubectl apply -f manifests/observability/grafana.yaml
```

Wait for the monitoring pods to be ready:
```bash
kubectl rollout status deployment/prometheus -n monitoring --timeout=60s
kubectl rollout status deployment/otel-collector -n monitoring --timeout=60s
kubectl rollout status deployment/grafana -n monitoring --timeout=60s
```

Access Grafana:
```bash
kubectl port-forward svc/grafana -n monitoring 3000:3000 &
```
Open http://localhost:3000 (login: admin / admin). Two dashboards are pre-loaded in the **AgentGateway** folder:
- **AgentGateway Dashboard** — overview, tokens, latency, error rates, infrastructure
- **Latency, Requests & Day 2 Health** — P50/P95/P99, SLI, runtime, rate limiting

## Step 9 — Create a llmbackend
```bash
kubectl apply -f manifests/llmbackend/gpt-3.5-turbo-public.yaml
kubectl apply -f manifests/llmbackend/gpt-4.1-mini-public.yaml
kubectl apply -f manifests/llmbackend/gpt-4.1-public.yaml
kubectl apply -f manifests/llmbackend/gpt-5-mini-public.yaml
kubectl apply -f manifests/llmbackend/gpt-oss-120b-local.yaml
```

```bash
watch bash -c 'oc logs -n default -l app.kubernetes.io/component=evaluator && oc get llmbackends.edgecloudlabs.edgecloudlabs.com -n default'
```

## Step 10 — Test the intelligent routing

```bash
export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
echo $INGRESS_GW_ADDRESS
```

Test model gpt-3.5-turbo (General knowledge):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions -H "content-type: application/json"  -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Hello, how are you?"
     }
    ]
  }'
```
Test model gpt-oss-120b-local (technology domain):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions -H "content-type: application/json"  -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Which language is commonly used for web page structure? A. Python, B. HTML, C. C++, D. Java"
     }
    ]
  }'
```


Test model gpt-oss-120b-local (physics domain):
```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions -H "content-type: application/json"  -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Calculate the force needed to accelerate a 10kg mass at 5m/s^2"
     }
    ]
  }'
```

Test model gpt-4.1 (Math domain):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions  -H "content-type: application/json" -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Solve the integral of x^2 from 0 to 1"
     }
    ]
  }'
```

Test model gpt-4.1 (science domain):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions  -H "content-type: application/json" -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Explain the process of photosynthesis in plants"
     }
    ]
  }'
```

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions  -H "content-type: application/json" -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Balance the chemical equation: H2 + O2 → H2O"
     }
    ]
  }'
```


## Step 10 — Running the Demos

You can use the provided Python scripts in the `artifacts/` folder to interactively test and visualize the intelligent router.

### Running the Live Demo Loop (`demo_loop.py`)

This script sends a continuous stream of categorized prompts to the Gateway, showing real-time latency, throughput, cost, and the router's dynamic model selection.

```bash
cd artifacts/
./demo_loop.py
```

**Available Flags:**
- `--long`: Uses complex, high-token prompts (~500+ tokens) instead of short questions. This is specifically designed to trigger and demonstrate the router's **token budget pressure** mechanism.
- `--pressure`: Unhides the `PRESSURE` column in the console output, allowing you to monitor the real-time token budget pressure from the router's logs.

**Example: Simulating Budget Pressure**
```bash
./demo_loop.py --long --pressure
```

### Visualizing the Routing Logic (`show_heatmap.py`)

This script connects to the Kubernetes cluster, reads all evaluated `LLMBackend` configurations, and reverse-engineers the intelligent router's scoring algorithm to display a matrix of the top choices for each domain.

```bash
cd artifacts/
./show_heatmap.py
```

**Available Flags:**
- `--live`: Instead of using static evaluation metrics, this flag queries the intelligent router's internal Prometheus endpoint to fetch the **live** average latency for each model and the live budget pressure. It recalculates the heatmap dynamically, showing exactly why the router is making its current decisions.

```bash
./show_heatmap.py --live
```


### 🎯 Change to Accuracy (Default)

```bash
kubectl patch statefulset intelligent-router -n intelligent-router-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/env/0/value", "value":"accuracy"}]'
```

### ⚡ Change to Latency

```bash
kubectl patch statefulset intelligent-router -n intelligent-router-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/env/0/value", "value":"latency"}]'
```
### 💰 Change to Cost
```bash
kubectl patch statefulset intelligent-router -n intelligent-router-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/env/0/value", "value":"cost"}]'
```

## Cleanup

```bash
# Remove lab manifests
kubectl delete -f manifests/agentgatewaypolicy.yaml
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

---

## Future Work: Advanced Hybrid LLM Routing Strategies

> For a comprehensive deep-dive, see [Hybrid LLM Routing Strategies](artifacts/Hybrid%20LLM%20Routing%20Strategies.md).

The intelligent router demonstrated in this lab implements domain-aware quality/latency/cost scoring with dynamic budget pressure. This is one strategy among a rich and rapidly evolving taxonomy of hybrid routing techniques. Below is a curated summary of advanced strategies identified in contemporary research, grouped by category, that represent exciting future directions for this project.

### Micro-Architectural & Token-Level Routing

| Strategy | Description |
|---|---|
| **Token-Level Collaborative Routing (CITER)** | A lightweight RL-trained router analyzes hidden states at every decode step, transferring execution to the cloud only when the local model's confidence collapses on a specific token. [[44]](https://github.com/aiming-lab/citer) |
| **Adaptive Multi-Level Speculative Chain (SpecRouter)** | Dynamically constructs an optimal chain of draft/verifier models on the fly based on real-time latency and token distribution divergence. [[49]](https://arxiv.org/abs/2505.07680) |
| **MoE Dynamic Expert Capacity Budgeting** | Restricts the number of active Mixture-of-Experts invoked during speculative decoding to prevent memory bandwidth starvation, increasing throughput by ~30%. [[53]](https://arxiv.org/html/2602.16052v1) |
| **Remaining-Token Orthogonality Pruning** | Predicts sequence completion via token-to-sink attention analysis, pruning uninformative tokens to collapse complexity from quadratic to linear. [[68]](https://arxiv.org/html/2602.02180v1) |

### Memory & Cache-Aware Routing

| Strategy | Description |
|---|---|
| **KV Cache-Affinity Optimization** | Routes queries to the node already holding the matching prompt prefix in its attention cache, achieving >87% cache hit rates. [[9]](https://developers.redhat.com/articles/2025/10/07/master-kv-cache-aware-routing-llm-d-efficient-ai-inference) |
| **PCIe Overlap I/O Optimization (KVPR)** | Overlaps GPU recomputation with PCIe cache transfer to completely mask physical transfer latency. [[36]](https://aclanthology.org/2025.findings-acl.997.pdf) |
| **Dynamic VRAM Footprint Projection** | Projects the total memory requirement of the context window, preemptively offloading to the cloud if the local node lacks contiguous VRAM. [[27]](https://github.com/ollama/ollama/issues/12591) |

### Uncertainty & Quality Assurance

| Strategy | Description |
|---|---|
| **Semantic Entropy Hallucination Routing** | Forces multiple quantized local drafts, clusters them by meaning; high semantic divergence triggers reroute to a more capable cloud model. [[56]](https://arxiv.org/html/2502.04428v1) |
| **Semantic Energy Boltzmann Analysis** | Applies a Boltzmann-inspired energy distribution to detect subtle reasoning failures more accurately than raw entropy. [[60]](https://arxiv.org/html/2508.14496v1) |
| **Consensus-Based Hierarchical Deflection** | Duplicates high-stakes queries across heterogeneous cloud providers; a local judge model enforces majority-vote factual alignment. [[61]](https://proactivemgmt.com/blog/2025/03/06/reducing-ai-hallucinations-multi-llm-consensus/) |
| **Hard-Blocking Long-Tail Filtration** | A lightweight firewall model blocks unsolvable "long-tail" queries from consuming cloud compute, returning curated fallbacks. [[82]](https://aclanthology.org/2025.emnlp-main.331/) |

### Infrastructure & Hardware-Aware Routing

| Strategy | Description |
|---|---|
| **Thermal and Power-Aware Scheduling (TAPAS)** | Monitors die temperatures and fan telemetry, diverting prefill requests away from hardware approaching thermal throttling limits. [[10]](https://arxiv.org/html/2501.02600v1) |
| **Spot-Instance Volatility Arbitrage** | Routes async bulk workloads to discounted, ephemeral cloud nodes; auto-fails back to local edge hardware on preemption events (~44% cost reduction). [[11]](https://arxiv.org/html/2411.01438v1) |
| **Network-Aware QoS Arbitration (SONAR)** | Analyzes real-time packet loss and WAN latency, confining latency-sensitive payloads to local edge when trans-oceanic links degrade. [[70]](https://arxiv.org/html/2510.13467v1) |
| **Energy-per-Token Ecological Optimization** | Calculates fluctuating electrical costs of local vs. cloud, routing to the endpoint with the lowest carbon footprint per token. [[8]](https://arxiv.org/html/2501.08219v4) |

### Security, Privacy & Compliance

| Strategy | Description |
|---|---|
| **PII Vaulting and Format-Preservation** | Routes through a local NER model to mask sensitive data with reversible vault identifiers before sending sterilized payloads to cloud APIs. [[74]](https://arxiv.org/html/2508.16765v1) |
| **Adversarial-Aware Deflection Sandbox** | Scans prompts for jailbreak vectors via spatial-aware alignment, routing malicious payloads to isolated, read-only local logging models. [[77]](https://neurips.cc/virtual/2025/loc/san-diego/session/128336) |
| **Geo-Location Data Residency Constraints (GDPR)** | Interrogates origin IP metadata to confine EU citizen data to EU-certified cloud zones or local edge clusters. [[12]](https://arxiv.org/html/2602.16100v1) |

### Economic & Operational Routing

| Strategy | Description |
|---|---|
| **Real-Time Token Budget Degradation** | Tracks aggregate tenant expenditure, automatically degrading routing from premium to free local models when financial limits are breached. [[87]](https://oneuptime.com/blog/post/2026-01-30-llm-rate-limiting/view) |
| **Circuit-Breaker Auto-Ejection** | Tracks continuous failure latency of external APIs, temporarily ejecting degraded endpoints from the routing table. [[5]](https://medium.com/@kamyashah2018/the-complete-guide-to-llm-routing-5-ai-gateways-transforming-production-ai-infrastructure-b5c68ee6d641) |
| **A/B Testing Silent Traffic Mirroring** | Duplicates a percentage of live cloud-bound traffic, silently routing the copy to a new local SLM to evaluate quality harmlessly. [[98]](https://aclanthology.org/2025.emnlp-industry.28.pdf) |
| **Policy Hot-Reloading Configuration** | Extracts routing logic from external declarative configs, permitting live runtime updates without daemon restarts. [[101]](https://developer.nvidia.com/blog/deploying-the-nvidia-ai-blueprint-for-cost-efficient-llm-routing/) |

### Key References

| # | Title | Link |
|---|---|---|
| 1 | Multi-LLM Routing Strategies for GenAI on AWS | [aws.amazon.com](https://aws.amazon.com/blogs/machine-learning/multi-llm-routing-strategies-for-generative-ai-applications-on-aws/) |
| 5 | The Complete Guide to LLM Routing: 5 AI Gateways | [medium.com](https://medium.com/@kamyashah2018/the-complete-guide-to-llm-routing-5-ai-gateways-transforming-production-ai-infrastructure-b5c68ee6d641) |
| 9 | KV Cache Aware Routing with llm-d (Red Hat) | [developers.redhat.com](https://developers.redhat.com/articles/2025/10/07/master-kv-cache-aware-routing-llm-d-efficient-ai-inference) |
| 10 | TAPAS: Thermal- and Power-Aware Scheduling | [arxiv.org](https://arxiv.org/html/2501.02600v1) |
| 11 | SkyServe: Serving AI Models with Spot Instances | [arxiv.org](https://arxiv.org/html/2411.01438v1) |
| 12 | LLM-Driven Privacy-Aware Orchestration Across Cloud-Edge | [arxiv.org](https://arxiv.org/html/2602.16100v1) |
| 44 | CITER: Token-Level Collaborative Routing | [github.com](https://github.com/aiming-lab/citer) |
| 49 | SpecRouter: Adaptive Multi-Level Speculative Decoding | [arxiv.org](https://arxiv.org/abs/2505.07680) |
| 53 | MoE-Spec: Expert Budgeting for Speculative Decoding | [arxiv.org](https://arxiv.org/html/2602.16052v1) |
| 56 | Uncertainty-Based On-device LLM Routing | [arxiv.org](https://arxiv.org/html/2502.04428v1) |
| 60 | Semantic Energy: Detecting Hallucination Beyond Entropy | [arxiv.org](https://arxiv.org/html/2508.14496v1) |
| 74 | Privacy Gatekeepers for Cloud-Based AI Interactions | [arxiv.org](https://arxiv.org/html/2508.16765v1) |
| 82 | Firewall Routing: Blocking for Better Hybrid Inference | [aclanthology.org](https://aclanthology.org/2025.emnlp-main.331/) |
| 101 | NVIDIA AI Blueprint for Cost-Efficient LLM Routing | [developer.nvidia.com](https://developer.nvidia.com/blog/deploying-the-nvidia-ai-blueprint-for-cost-efficient-llm-routing/) |