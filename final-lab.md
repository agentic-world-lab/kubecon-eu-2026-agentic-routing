# Intelligent Router Verification Lab

This lab proves that the intelligent router classifies prompts by domain and routes each request to the best-scoring LLM model. You deploy everything from scratch, apply LLMBackend CRs that trigger the agentic evaluation pipeline, and then verify routing decisions via curl.

## Architecture

```
Client request  (model: "auto", no routing hint)
        |
        v
AgentGateway  (port 80)
        |  ExtProc (PreRouting) --> Intelligent Router (:18080)
        |                            reads body -> classifies domain
        |                            scores all models for that domain
        |                            injects x-router-selected-model header
        |  route selection based on header
        |--- gpt-4.1-mini  backend  (best for finance / health)
        |--- gpt-4.1      backend  (best for legal / general)
        '--- gpt-5-mini   backend  (best for science / technology)
        v
OpenAI API
```

**How it works:**
1. The agentic pipeline (kagent orchestrator) evaluates each LLMBackend CR using MMLU-Pro benchmarks and enriches it with pricing data.
2. The intelligent router watches all LLMBackend CRs with `phase=Evaluated`, extracts per-domain accuracy scores, and builds a scoring model.
3. On every request, the router classifies the prompt domain (BERT classifier + keyword fallback) and selects the model with the highest weighted score for that domain.
4. The routing decision is proven by the `model` field in the OpenAI response and the router logs.

---

## Prerequisites

| Tool | Install |
|------|---------|
| kubectl >= 1.28 | `brew install kubectl` |
| helm >= 3.14 | `brew install helm` |
| jq | `brew install jq` |
| OpenAI API key | Set as `$OPENAI_API_KEY` in your shell |
| HuggingFace token | Set as `$HF_TOKEN` in your shell |
| Kubernetes cluster | Running and accessible via kubectl |

Verify tools are installed:
```bash
kubectl version --client && helm version --short && jq --version
```

Verify API keys are set:
```bash
[ -n "$OPENAI_API_KEY" ] && echo "OPENAI_API_KEY is set" || echo "ERROR: OPENAI_API_KEY not set"
[ -n "$HF_TOKEN" ] && echo "HF_TOKEN is set" || echo "ERROR: HF_TOKEN not set"
```

---

## Step 1 — Verify cluster access

```bash
kubectl cluster-info
```

You should see the cluster endpoint. Confirm no prior lab resources exist:

```bash
kubectl get ns agentgateway-system intelligent-router-system kagent 2>/dev/null || echo "Clean cluster - ready to go"
```

---

## Step 2 — Install AgentGateway

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

Wait for the control plane to be ready:
```bash
kubectl rollout status deployment/agentgateway -n agentgateway-system --timeout=120s
```

Create the Gateway and the ExtProc policy that connects to the intelligent router:
```bash
kubectl apply -f manifests/agentgateway/gateway.yaml
kubectl apply -f manifests/agentgateway/agentgatewaypolicy.yaml
```

---

## Step 3 — Store secrets

Create the namespaces that need secrets:
```bash
kubectl create namespace intelligent-router-system 2>/dev/null || true
kubectl create namespace kagent 2>/dev/null || true
```

Create the OpenAI secret in all required namespaces:
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

Create the HuggingFace secret for the BERT model download:
```bash
kubectl create secret generic huggingface-api-key \
  --from-literal="HF_TOKEN=$HF_TOKEN" \
  --namespace intelligent-router-system
```

---

## Step 4 — Install the LLMBackend CRD

The LLMBackend custom resource tracks model evaluations (accuracy, latency, pricing per domain):

```bash
kubectl apply -f manifests/model-agentic-controller/crd-agentic-controller.yaml
```

Verify:
```bash
kubectl get crd llmbackends.edgecloudlabs.edgecloudlabs.com
```

---

## Step 5 — Install kagent

kagent provides the agent runtime (A2A orchestration, MCP servers, BYO agents):

```bash
curl https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash
kagent install
```

Wait for kagent to be ready:
```bash
kubectl rollout status deployment/kagent-controller -n kagent --timeout=180s
```

---

## Step 6 — Deploy MCP servers

These MCP servers provide real-time pricing and energy cost data to the agentic pipeline:

```bash
kubectl apply -f manifests/mcps/openrouter-pricing-mcp.yaml
kubectl apply -f manifests/mcps/spain-electricity-cost-mcp.yaml
```

Wait for MCP servers to be ready:
```bash
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent openrouter-pricing-mcp --timeout=120s
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent spain-electricity-cost-mcp --timeout=120s
```

---

## Step 7 — Deploy agents

Deploy all agents — including the orchestrator that evaluates LLMBackend CRs:

```bash
kubectl apply -f manifests/agents/k8s-agent.yaml
kubectl apply -f manifests/agents/model-cost-agent.yaml
kubectl apply -f manifests/agents/sp-electricity-cost-agent.yaml
kubectl apply -f manifests/agents/sr-llm-backends.yaml
kubectl apply -f manifests/agents/orchestrator-agent.yaml
```

Verify the orchestrator agent pod is running:
```bash
kubectl get pods -n kagent -l kagent.dev/agent-name=orchestrator
```

---

## Step 8 — Deploy the Intelligent Router

The intelligent router is an ExtProc gRPC server that classifies prompt domains using a BERT model and routes requests to the best-scoring backend.

```bash
kubectl apply -f manifests/intelligent-router/namespace.yaml
kubectl apply -f manifests/intelligent-router/rbac.yaml
kubectl apply -f manifests/intelligent-router/service.yaml
kubectl apply -f manifests/intelligent-router/statefulset.yaml
```

> **Note:** The first startup downloads ~2 GB of BERT model weights from HuggingFace and merges LoRA adapters. This can take 5-10 minutes. Subsequent restarts use the cached model on the PVC.

Watch the init containers complete:
```bash
kubectl get pods -n intelligent-router-system -w
```

Wait for the router to be ready:
```bash
kubectl rollout status statefulset/intelligent-router -n intelligent-router-system --timeout=600s
```

At this point, the router starts but has no models to route to (no Evaluated LLMBackends yet). Check its logs:
```bash
kubectl logs -n intelligent-router-system statefulset/intelligent-router --tail=20
```

You should see it polling for LLMBackend CRs but finding none with `phase=Evaluated`.

---

## Step 9 — Apply LLMBackend CRs (triggers the agentic pipeline)

This is where the magic happens. Each LLMBackend CR triggers the orchestrator agent to:
1. Create an AgentgatewayBackend + HTTPRoute
2. Launch an MMLU-Pro evaluation job
3. Collect accuracy scores per domain (science, finance, technology, health, legal, general)
4. Enrich with pricing data from OpenRouter API
5. Update the CR status to `phase=Evaluated`

Apply all 4 models:
```bash
kubectl apply -f manifests/llmbackend/gpt-4.1-public.yaml
kubectl apply -f manifests/llmbackend/gpt-3.5-turbo-public.yaml
kubectl apply -f manifests/llmbackend/gpt-4.1-mini-public.yaml
kubectl apply -f manifests/llmbackend/gpt-5-mini-public.yaml
```

Watch the evaluation progress:
```bash
watch -n 5 'kubectl get llmbackends -A \
  -o custom-columns="NAME:.metadata.name,NS:.metadata.namespace,MODEL:.spec.model,PHASE:.status.phase,ACCURACY:.status.results.overallAccuracy"'
```

Wait until **all 4 models** show `phase=Evaluated`. This typically takes 2-5 minutes per model.

---

## Step 10 — Discover the domain-to-model mapping

Once all LLMBackends are evaluated, inspect their per-domain accuracy scores:

```bash
echo "=== Per-Domain Accuracy Scores ==="
for backend in $(kubectl get llmbackends -A -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'); do
  ns=$(kubectl get llmbackends -A -o jsonpath="{.items[?(@.metadata.name=='$backend')].metadata.namespace}")
  model=$(kubectl get llmbackend $backend -n $ns -o jsonpath='{.spec.model}')
  echo ""
  echo "--- $model ($backend) ---"
  echo "  Overall accuracy: $(kubectl get llmbackend $backend -n $ns -o jsonpath='{.status.results.overallAccuracy}')"
  echo "  Pricing (prompt/completion): $(kubectl get llmbackend $backend -n $ns -o jsonpath='{.status.results.pricing.prompt}') / $(kubectl get llmbackend $backend -n $ns -o jsonpath='{.status.results.pricing.completion}')"
  echo "  Category accuracy:"
  kubectl get llmbackend $backend -n $ns -o jsonpath='{.status.results.categoryAccuracy}' | jq -r 'to_entries[] | "    \(.key): \(.value)"' 2>/dev/null || echo "    (no category data)"
done
```

Check that the intelligent router picked up the evaluated models:
```bash
kubectl logs -n intelligent-router-system statefulset/intelligent-router --tail=30 | grep -E "loaded|model|config|domain"
```

You should see log lines showing the router loaded the models and their scores. Note which model wins for each domain — the one with the highest categoryAccuracy for that domain will be selected (since `OPTIMIZATION_TARGET=accuracy`).

---

## Step 11 — Test routing with curl

Set up access to the gateway:
```bash
kubectl port-forward deployment/agentgateway-proxy -n agentgateway-system 8080:80 &
export INGRESS_GW_ADDRESS="localhost:8080"
```

> If your cluster has an externally reachable LoadBalancer, you can use that instead:
> ```bash
> export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy \
>   -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
> ```

### Test 1: Finance domain
```bash
echo "=== FINANCE ==="
curl -s http://$INGRESS_GW_ADDRESS/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "What stocks should I invest in for retirement?"}]
  }' | jq '{model: .model, content: .choices[0].message.content[:100]}'
```

### Test 2: Technology domain
```bash
echo "=== TECHNOLOGY ==="
curl -s http://$INGRESS_GW_ADDRESS/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Explain Kubernetes networking and how pods communicate"}]
  }' | jq '{model: .model, content: .choices[0].message.content[:100]}'
```

### Test 3: Science domain
```bash
echo "=== SCIENCE ==="
curl -s http://$INGRESS_GW_ADDRESS/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Balance the chemical equation: H2 + O2 -> H2O"}]
  }' | jq '{model: .model, content: .choices[0].message.content[:100]}'
```

### Test 4: Health domain
```bash
echo "=== HEALTH ==="
curl -s http://$INGRESS_GW_ADDRESS/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "What are the symptoms and treatment options for type 2 diabetes?"}]
  }' | jq '{model: .model, content: .choices[0].message.content[:100]}'
```

### Test 5: Legal domain
```bash
echo "=== LEGAL ==="
curl -s http://$INGRESS_GW_ADDRESS/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "What is the legal process for filing a patent?"}]
  }' | jq '{model: .model, content: .choices[0].message.content[:100]}'
```

### Test 6: General knowledge
```bash
echo "=== GENERAL ==="
curl -s http://$INGRESS_GW_ADDRESS/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Hello, how are you?"}]
  }' | jq '{model: .model, content: .choices[0].message.content[:100]}'
```

### Verify routing decisions in the router logs

After running the tests, check the intelligent router logs to confirm each classification:

```bash
kubectl logs -n intelligent-router-system statefulset/intelligent-router --tail=50 | grep -E "domain=|selected_model="
```

You should see entries like:
```
[route] ML classifier: domain=finance confidence=0.95
[ext_proc] domain=finance selected_model=gpt-4.1-mini
[route] ML classifier: domain=technology confidence=0.999
[ext_proc] domain=technology selected_model=gpt-5-mini
[route] ML classifier: domain=science confidence=1.000
[ext_proc] domain=science selected_model=gpt-5-mini
[route] ML classifier: domain=health confidence=0.673
[ext_proc] domain=health selected_model=gpt-4.1-mini
[route] ML classifier: domain=legal confidence=0.937
[ext_proc] domain=legal selected_model=gpt-4.1
[route] ML classifier: domain=general confidence=1.000
[ext_proc] domain=general selected_model=gpt-4.1
```

### Verification summary

The router uses the BERT ML classifier (with keyword fallback) to detect the domain, then selects the model with the highest weighted accuracy for that domain (`OPTIMIZATION_TARGET=accuracy`).

**Expected routing (based on MMLU-Pro evaluation scores):**

| Domain | Prompt example | Expected model | Why |
|--------|---------------|----------------|-----|
| finance | "What stocks should I invest in?" | gpt-4.1-mini | Highest finance accuracy (business+economics avg) |
| technology | "Explain Kubernetes networking" | gpt-5-mini | Highest technology accuracy (CS+engineering avg) |
| science | "Balance the chemical equation" | gpt-5-mini | Highest science accuracy (bio+chem+math+physics avg) |
| health | "Symptoms of type 2 diabetes?" | gpt-4.1-mini | Highest health accuracy (health+psychology avg) |
| legal | "Legal process for filing a patent?" | gpt-4.1 | Highest legal accuracy (law+history avg) |
| general | "Hello, how are you?" | gpt-4.1 | Highest general accuracy (other+philosophy avg) |

For each test, confirm:
1. **Response `model` field** — The model in the JSON response matches the expected winner for that domain.
2. **Router logs** — The log shows the correct domain classification and the selected model.
3. **Different domains route to different models** — All 3 models are used across the 6 tests, proving the router is making domain-aware decisions based on evaluation data.

---

## Cleanup

```bash
# Remove LLMBackend CRs
kubectl delete -f manifests/llmbackend/

# Remove intelligent router
kubectl delete -f manifests/intelligent-router/statefulset.yaml
kubectl delete -f manifests/intelligent-router/service.yaml
kubectl delete -f manifests/intelligent-router/rbac.yaml
kubectl delete -f manifests/intelligent-router/namespace.yaml

# Remove agents and MCPs
kubectl delete -f manifests/agents/
kubectl delete -f manifests/mcps/

# Remove LLMBackend CRD
kubectl delete -f manifests/model-agentic-controller/crd-agentic-controller.yaml

# Uninstall kagent
kagent uninstall

# Remove gateway resources
kubectl delete -f manifests/agentgateway/agentgatewaypolicy.yaml
kubectl delete -f manifests/agentgateway/gateway.yaml

# Remove secrets
kubectl delete secret openai-secret -n agentgateway-system
kubectl delete secret openai-secret -n kagent
kubectl delete secret openai-secret -n intelligent-router-system
kubectl delete secret huggingface-api-key -n intelligent-router-system

# Uninstall AgentGateway
helm uninstall agentgateway agentgateway-crds -n agentgateway-system
kubectl delete namespace agentgateway-system intelligent-router-system kagent
```
