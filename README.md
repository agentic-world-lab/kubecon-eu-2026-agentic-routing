# kubecon-eu-2026-agentic-routing
Resources for talk at KubeCon + CloudNativeCon Europe 2026 Amsterdam: Intelligent Routing for Optimized Inference

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
```

### Step 3 — Store the OpenAI API key

```bash
export OPENAI_API_KEY="<your_openai_api_key_here>"
```

```bash
kubectl create secret generic openai-secret \
  --from-literal="Authorization=Bearer $OPENAI_API_KEY" \
  --namespace agentgateway-system
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
kubectl apply -f manifests/agents/model-cost-agent.yaml
kubectl apply -f manifests/agents/sp-electricity-cost-agent.yaml
kubectl apply -f manifests/agents/sr-llm-backends.yaml 
```

#### Step 6 — Deploy the llmbackend Watcher

```bash
kubectl apply -f manifests/agent-watcher/
```

## Step 7 — Install the Backend Evaluation Operator

```bash
kubectl apply -f manifests/backend-evaluator-controller/install.yaml
```

## Step 8 — Deploy the Intelligent Router

```bash
# Intelligent-router CRD, RBAC, CR config, and workload
kubectl apply -f manifests/intelligent-router/crd-intelligent-router-config.yaml
kubectl apply -f manifests/namespace.yaml
kubectl apply -f manifests/intelligent-router/rbac.yaml
kubectl apply -f manifests/intelligent-router/service.yaml
kubectl apply -f manifests/intelligent-router/statefulset.yaml

```


## Step 9 — Create a llmbackend
```bash
kubectl apply -f manifests/llmbackend/gpt-4.1-public.yaml
kubectl apply -f manifests/llmbackend/gpt-3.5-turbo-public.yaml
kubectl apply -f manifests/llmbackend/gpt-oss-120b-local.yaml
```

```bash
watch bash -c 'oc logs -n default -l app.kubernetes.io/component=evaluator && oc get llmbackends.edgecloudlabs.edgecloudlabs.com -n default'
```

```bash
export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
echo $INGRESS_GW_ADDRESS
```

Test model gpt-3.5-turbo (General knowledge):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions   -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Hello, how are you?"
     }
    ]
  }'
```
Test model gpt-oss-120b-local (computer science domain):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions   -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Which language is commonly used for web page structure?\n\nA. Python\nB. HTML\nC. C++\nD. Java"
     }
    ]
  }'
```


Test model gpt-oss-120b-local (physics domain):
```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions   -d '{
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
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions -H "Application/json"  -d '{
    "model": "auto", 
    "messages": [
     { "role": "user", 
       "content": "Solve the integral of x^2 from 0 to 1"
     }
    ]
  }'
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