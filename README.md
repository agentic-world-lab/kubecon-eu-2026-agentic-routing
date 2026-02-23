# Intelligent Routing for Optimized Inference

Lab for KubeCon + CloudNativeCon Europe 2026 Amsterdam.

This lab walks you through deploying an intelligent model routing system using Agent Gateway, a Semantic Router (ExtProc), and kagent. By the end, you will have an AI agent that dynamically configures routing decisions, and a ValidatingWebhook that calls that agent to validate every configuration change before it is admitted into the cluster.

## Architecture

```
                 ┌──────────────────────────────────────────────────────┐
                 │               Kubernetes API Server                  │
                 │                                                      │
                 │  IntelligentPool CR    IntelligentRoute CR           │
                 │         │                      │                     │
                 │         └──────┬───────────────┘                     │
                 │                │                                     │
                 │    ValidatingWebhookConfiguration                   │
                 │                │                                     │
                 └────────────────┼─────────────────────────────────────┘
                                  │ POST /validate
                                  ▼
                 ┌──────────────────────────────────┐
                 │  Agent Webhook Bridge             │
                 │  Translates AdmissionReview       │
                 │  into a prompt for the agent      │
                 └───────────────┬──────────────────┘
                                 │ A2A protocol (JSON-RPC)
                                 ▼
                 ┌──────────────────────────────────┐
                 │  kagent                           │
                 │  "intelligent-router-             │
                 │   configurator" agent             │
                 │  Validates + applies CRs          │
                 └──────────────────────────────────┘

┌──────────┐    ┌──────────────────┐    ┌──────────────────────────────┐
│  Client  │───▶│  Agent Gateway   │───▶│  Semantic Router (ExtProc)   │
│          │    │  (Gateway API)   │    │  Domain classification       │
│          │    │                  │◀───│  Weighted scoring             │
│          │    │  Routes by       │    │  (domain + latency + cost)   │
│          │    │  x-vsr-selected- │    └──────────────────────────────┘
│          │    │  model header    │
│          │    └────────┬─────────┘
│          │◀────────────┘
└──────────┘
```

## Prerequisites

- Kubernetes cluster (v1.28+)
- kubectl CLI
- Helm (v3+)
- OpenAI API key
- Gemini API key (for kagent)

---

## Step 1: Install Agent Gateway

```bash
export GATEWAY_VERSION="v2.2.0-main"
export OPENAI_API_KEY="<your_openai_api_key_here>"
```

Install Gateway API CRDs and Agent Gateway:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml
```

```bash
helm upgrade -i agentgateway-crds oci://ghcr.io/kgateway-dev/charts/agentgateway-crds \
  --create-namespace --namespace agentgateway-system \
  --version ${GATEWAY_VERSION} \
  --set controller.image.pullPolicy=Always

helm upgrade -i agentgateway oci://ghcr.io/kgateway-dev/charts/agentgateway \
  --namespace agentgateway-system \
  --version ${GATEWAY_VERSION} \
  --set controller.image.pullPolicy=Always \
  --set controller.extraEnv.KGW_ENABLE_GATEWAY_API_EXPERIMENTAL_FEATURES=true
```

Create the Gateway and OpenAI secret:

```bash
kubectl apply -f manifests/gateway.yaml
```

```bash
kubectl apply -f- <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: openai-secret
  namespace: agentgateway-system
type: Opaque
stringData:
  Authorization: $OPENAI_API_KEY
EOF
```

## Step 2: Deploy Agent Gateway Backends

```bash
kubectl apply -f manifests/agentgatewaybackends.yaml
kubectl apply -f manifests/httproutes-openai.yaml
```

## Step 3: Deploy the Semantic Router

```bash
kubectl create ns vllm-semantic-router-system
kubectl apply -f semantic-router/manifests/configmap.yaml -n vllm-semantic-router-system
kubectl apply -f semantic-router/manifests/deployment.yaml -n vllm-semantic-router-system
kubectl apply -f semantic-router/manifests/service.yaml -n vllm-semantic-router-system
kubectl apply -f manifests/agentgatewaypolicy.yaml
```

Verify the router is running:

```bash
kubectl get pods -n vllm-semantic-router-system
```

## Step 4: Test the routing

```bash
export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy \
  -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
echo $INGRESS_GW_ADDRESS
```

Send a finance query (should route to the powerful model):

```bash
curl -s "http://$INGRESS_GW_ADDRESS/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MoM",
    "messages": [
      {"role": "user", "content": "What is the current P/E ratio of the S&P 500 and how does it compare to historical averages?"}
    ]
  }' | jq '.model'
```

Send a general query (should route to the cheaper model):

```bash
curl -s "http://$INGRESS_GW_ADDRESS/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MoM",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ]
  }' | jq '.model'
```

Check the routing decision headers:

```bash
curl -si "http://$INGRESS_GW_ADDRESS/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MoM",
    "messages": [
      {"role": "user", "content": "Explain the derivative of x^3"}
    ]
  }' 2>&1 | grep -i "x-vsr"
```

---

## Step 5: Deploy kagent and the intelligent-router-configurator agent

> **TODO**: This section needs implementation.

### 5.1 Install kagent

```bash
export GEMINI_API_KEY="<your_gemini_api_key_here>"
```

```bash
helm upgrade --install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
    --namespace kagent \
    --version=0.7.13 \
    --create-namespace
```

```bash
helm upgrade --install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
    --namespace kagent \
    --version=0.7.13 \
    --set providers.default=gemini \
    --set ui.service.type=LoadBalancer \
    --set providers.gemini.apiKey=$GEMINI_API_KEY
```

### 5.2 Install the semantic router CRDs

> **TODO**: Package and install the `IntelligentPool` and `IntelligentRoute` CRDs (`vllm.ai/v1alpha1`).

```bash
# TODO: kubectl apply -f semantic-router/manifests/crds/
```

### 5.3 Create the intelligent-router-configurator agent

> **TODO**: Create the kagent Agent CR. The agent should:
>
> - Use `k8s_apply_manifest` and `k8s_get_resource` tools to read/write `IntelligentPool` and `IntelligentRoute` CRs
> - Understand the CR schema (see [configuration-by-crs.md](semantic-router/configuration-by-crs.md))
> - Generate valid CRs from natural language and apply them to the cluster

```yaml
# TODO: Create this Agent CR
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: intelligent-router-configurator
  namespace: kagent
spec:
  description: >
    AI agent that configures the semantic router by creating and updating
    IntelligentPool and IntelligentRoute custom resources.
  type: Declarative
  declarative:
    modelConfig: default-model-config
    stream: true
    systemMessage: |
      You are an AI agent that configures an intelligent model router.
      You operate two CRs in the vllm.ai/v1alpha1 API group:
      1. IntelligentPool - defines available models and pricing
      2. IntelligentRoute - defines signals, domains, decisions, and plugins
      Always deploy to namespace: vllm-semantic-router-system
    tools:
      - type: McpServer
        mcpServer:
          name: kagent-tool-server
          kind: RemoteMCPServer
          toolNames:
            - k8s_apply_manifest
            - k8s_get_resource
            - k8s_get_resource_yaml
            - k8s_list_resources
```

### 5.4 Test the agent

```bash
export KAGENT_UI_ADDRESS=$(kubectl get svc -n kagent kagent-ui \
  -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
open "http://$KAGENT_UI_ADDRESS:8080"
```

Example prompts:
- "Route all finance and legal queries to gpt-4.1 with a system prompt"
- "Add a new model gpt-5-mini with input price 0.001"
- "Show me the current routing configuration"

---

## Step 6: Deploy the Agent Webhook Bridge

The bridge is a ValidatingWebhook that intercepts `AgentgatewayBackend` CR changes. When a backend has router annotations, the bridge calls the kagent agent to automatically create/update the `IntelligentPool` and `IntelligentRoute` CRs so the semantic router learns about the new model.

```
kubectl apply AgentgatewayBackend (with router annotations)
       │
       ▼
  ValidatingWebhook ──► Bridge ──► kagent A2A ──► Agent LLM
       │                                              │
       │                                     k8s_apply_manifest
       │                                     (creates/updates
       │                                      IntelligentPool +
       │                                      IntelligentRoute)
       ▼
  always admitted
```

The bridge **always admits** the `AgentgatewayBackend` (it never blocks). It fires the agent call asynchronously in the background. The agent then uses `k8s_apply_manifest` to configure the router.

### Annotation convention

Add these annotations to an `AgentgatewayBackend` to opt-in to automatic router configuration:

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `router.vllm.ai/enabled` | Yes (label or annotation) | Opt-in flag | `"true"` |
| `router.vllm.ai/domains` | Yes | Comma-separated domains this model is good at | `"math,physics"` |
| `router.vllm.ai/cost` | No | Cost per token | `"0.002"` |
| `router.vllm.ai/latency-ms` | No | Average latency in ms | `"120"` |

### 6.1 Build and push the bridge image

```bash
cd agent-webhook-bridge
docker buildx build --push --platform linux/arm64 \
  -t antonioberben/agent-webhook-bridge:latest .
```

### 6.2 Deploy the bridge

```bash
kubectl apply -f agent-webhook-bridge/manifests/webhook.yaml
kubectl apply -f agent-webhook-bridge/manifests/deployment.yaml

# Generate TLS certs, create Secret, patch webhook caBundle
agent-webhook-bridge/manifests/gen-certs.sh kagent
```

Verify:

```bash
kubectl get pods -n kagent -l app=agent-webhook-bridge
```

### 6.3 Test: add a new backend with router annotations

Apply an `AgentgatewayBackend` with annotations telling the router this model is good at math and physics:

```bash
kubectl apply -f- <<EOF
apiVersion: agentgateway.dev/v1alpha1
kind: AgentgatewayBackend
metadata:
  name: gpt-4.1
  namespace: agentgateway-system
  labels:
    router.vllm.ai/enabled: "true"
  annotations:
    router.vllm.ai/domains: "math,physics,engineering"
    router.vllm.ai/cost: "0.002"
    router.vllm.ai/latency-ms: "120"
spec:
  ai:
    groups:
      - providers:
          - name: openai-gpt-4.1
            openai:
              model: gpt-4.1
            policies:
              auth:
                secretRef:
                  name: openai-secret
EOF
```

The bridge intercepts this, calls the agent, and the agent creates/updates the `IntelligentPool` and `IntelligentRoute` CRs so the router knows to send math/physics/engineering queries to `gpt-4.1`.

Check the bridge logs:

```bash
kubectl logs -n kagent -l app=agent-webhook-bridge
```

Verify the router CRs were created:

```bash
kubectl get intelligentpools -n vllm-semantic-router-system
kubectl get intelligentroutes -n vllm-semantic-router-system
```

### 6.4 Test: add a second backend for general queries

```bash
kubectl apply -f- <<EOF
apiVersion: agentgateway.dev/v1alpha1
kind: AgentgatewayBackend
metadata:
  name: gpt-4.1-mini
  namespace: agentgateway-system
  labels:
    router.vllm.ai/enabled: "true"
  annotations:
    router.vllm.ai/domains: "general,business,history"
    router.vllm.ai/cost: "0.0004"
    router.vllm.ai/latency-ms: "85"
spec:
  ai:
    groups:
      - providers:
          - name: openai-gpt-4.1-mini
            openai:
              model: gpt-4.1-mini
            policies:
              auth:
                secretRef:
                  name: openai-secret
EOF
```

The agent will add `gpt-4.1-mini` to the pool and create decisions routing general/business/history queries to it, without removing the existing math/physics/engineering decisions for `gpt-4.1`.

### 6.5 Test: backend without router annotations (ignored)

```bash
kubectl apply -f- <<EOF
apiVersion: agentgateway.dev/v1alpha1
kind: AgentgatewayBackend
metadata:
  name: internal-backend
  namespace: agentgateway-system
spec:
  ai:
    groups:
      - providers:
          - name: some-provider
            openai:
              model: some-model
EOF
```

The bridge sees no `router.vllm.ai/enabled: "true"` label, so it skips this backend entirely. No agent call is made.

---

## Summary

| Step | Component | Purpose |
|------|-----------|---------|
| 1 | Agent Gateway | Gateway API implementation with ExtProc support |
| 2 | AgentgatewayBackend | LLM provider backends (OpenAI models) |
| 3 | Semantic Router | ExtProc server for domain-based model routing |
| 4 | Test routing | Verify domain classification and model selection |
| 5 | kagent + agent | AI agent that configures routing via CRs |
| 6 | Agent Webhook Bridge | ValidatingWebhook on AgentgatewayBackend that triggers the agent to auto-configure the router |

## References

- [Agent Gateway docs](https://agentgateway.dev/docs/kubernetes/main/)
- [Semantic Router CRD examples](semantic-router/configuration-by-crs.md)
- [kagent docs](https://kagent.dev/docs)
- [kagent A2A protocol](https://kagent.dev/docs/kagent/examples/a2a-agents)
