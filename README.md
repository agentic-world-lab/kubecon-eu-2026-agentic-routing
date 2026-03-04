# kubecon-eu-2026-agentic-routing
Resources for talk at KubeCon + CloudNativeCon Europe 2026 Amsterdam: Intelligent Routing for Optimized Inference

## Overview

## Installation

### Prerequisites

- Kubernetes cluster (v1.20+)
- kubectl CLI
- Helm (v3+)

### Steps

1. Install Agent Gateway

```bash
export GATEWAY_VERSION="v2.2.0-main"
```

```bash
export OPENAI_API_KEY="<your_openai_api_key_here>"
```

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

1.2. Create a gateway

```bash
kubectl apply -f manifests/agentgateway/gateway.yaml
```

2. Create a secret with the OpenAI API key

```bash
kubectl apply -f- <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: openai-secret
  namespace: default
type: Opaque
stringData:
  Authorization: $OPENAI_API_KEY
EOF
```

4. Install kagent

```bash
curl https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash
kagent install
```

4.1. Create the MCPs
```bash
kubectl apply -f manifests/mcps/openrouter-pricing-mcp.yaml
kubectl apply -f manifests/mcps/spain-electricity-cost-mcp.yaml
```

Wait for the mcp to be ready
```bash
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent openrouter-pricing-mcp
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent spain-electricity-cost-mcp
```

4.2. Create the configuration agents

```bash
kubectl apply -f manifests/agents/model-cost-agent.yaml
kubectl apply -f manifests/agents/spain-electricity-cost-agent.yaml
kubectl apply -f manifests/agents/sr-config-agent.yaml 
```

5. Deploy the llmbackend Watcher

```bash
kubectl apply -f manifests/agent-watcher/agent-watcher.yaml
```

6. Install the Backend Evaluation Operator - This will register the LLMBackend CRD and the controller.

```bash
kubectl apply -f manifests/backend-evaluator-controller/install.yaml
```

7. Deploy the semantic Router
```bash
helm upgrade -i semantic-router oci://ghcr.io/vllm-project/charts/semantic-router:v0.0.0-latest --namespace vllm-semantic-router-system --create-namespace -f manifests/semantic-router/values_local.yaml --set persistence.storageClassName=longhorn --set image.repository=fjvicens/vllm-sr-extproc --set image.tag=v0.16 --set image.pullPolicy=Always --set config.classifier.pii_model.pii_mapping_path="models/pii_classifier_modernbert-base_presidio_token_model/label_mapping.json"
```

8. Create a llmbackend
```bash
kubectl apply -f manifests/llmbackend/gpt-4.1-public.yaml
kubectl apply -f manifests/llmbackend/gpt-3.5-turbo-public.yaml
kubectl apply -f manifests/llmbackend/gpt-oss-120b-local.yaml
```


```bash
export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
echo $INGRESS_GW_ADDRESS
```

Test model gpt-3.5-turbo (General knowledge):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS//v1/chat/completions   -d '{
    "model": "MoM", 
    "messages": [
     { "role": "user", 
       "content": "Hello, how are you?"
     }
    ]
  }'
```

Test model gpt-4.1 (Math domain):

```bash
curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions   -d '{
    "model": "MoM", 
    "messages": [
     { "role": "user", 
       "content": "Solve the integral of x^2 from 0 to 1"
     }
    ]
  }'
```