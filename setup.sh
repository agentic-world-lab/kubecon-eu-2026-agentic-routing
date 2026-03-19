#!/usr/bin/env bash
# =============================================================================
#  KubeCon EU 2026 — Intelligent Routing Lab  ·  Automated Setup (Steps 1–8)
# =============================================================================
set -euo pipefail

# ── Colours & helpers ────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'
BOLD='\033[1m'; NC='\033[0m'

info()  { echo -e "${CYAN}ℹ ${NC} $*"; }
ok()    { echo -e "${GREEN}✔ ${NC} $*"; }
warn()  { echo -e "${YELLOW}⚠ ${NC} $*"; }
err()   { echo -e "${RED}✖ ${NC} $*" >&2; }
banner(){ echo -e "\n${BOLD}${CYAN}═══════════════════════════════════════════════${NC}"; \
          echo -e "${BOLD}  $*${NC}"; \
          echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════${NC}\n"; }

# ── Resolve repo root (where this script lives) ─────────────────────────────
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Gateway version ──────────────────────────────────────────────────────────
GATEWAY_VERSION="${GATEWAY_VERSION:-v2.2.0-main}"

# =============================================================================
#  Pre-flight checks
# =============================================================================
banner "🔍  Pre-flight checks"

for tool in kubectl helm jq curl; do
  if ! command -v "$tool" &>/dev/null; then
    err "Required tool '${tool}' is not installed. Please install it and re-run."
    exit 1
  fi
  ok "${tool} found"
done

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  err "Environment variable OPENAI_API_KEY is not set."
  echo "   export OPENAI_API_KEY=\"sk-…\""
  exit 1
fi
ok "OPENAI_API_KEY is set"

if [[ -z "${HF_TOKEN:-}" ]]; then
  err "Environment variable HF_TOKEN is not set."
  echo "   export HF_TOKEN=\"hf_…\""
  exit 1
fi
ok "HF_TOKEN is set"

# =============================================================================
#  Step 1 — Verify cluster access
# =============================================================================
banner "🚀  Step 1 — Verify cluster access"

if ! kubectl cluster-info &>/dev/null; then
  err "Cannot reach the Kubernetes cluster. Make sure it is running and kubectl is configured."
  exit 1
fi
kubectl cluster-info
ok "Cluster is reachable"

# =============================================================================
#  Step 2 — Install AgentGateway
# =============================================================================
banner "📦  Step 2 — Install AgentGateway"

info "Applying Gateway API CRDs …"
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml

# --- agentgateway-crds ---
if helm status agentgateway-crds -n agentgateway-system &>/dev/null; then
  ok "Helm release 'agentgateway-crds' already present — skipping install"
else
  info "Installing agentgateway-crds …"
  helm upgrade -i agentgateway-crds oci://ghcr.io/kgateway-dev/charts/agentgateway-crds \
    --create-namespace --namespace agentgateway-system \
    --version "${GATEWAY_VERSION}" \
    --set controller.image.pullPolicy=Always
  ok "agentgateway-crds installed"
fi

# --- agentgateway ---
if helm status agentgateway -n agentgateway-system &>/dev/null; then
  ok "Helm release 'agentgateway' already present — skipping install"
else
  info "Installing agentgateway …"
  helm upgrade -i agentgateway oci://ghcr.io/kgateway-dev/charts/agentgateway \
    --namespace agentgateway-system \
    --version "${GATEWAY_VERSION}" \
    --set controller.image.pullPolicy=Always \
    --set controller.extraEnv.KGW_ENABLE_GATEWAY_API_EXPERIMENTAL_FEATURES=true
  ok "agentgateway installed"
fi

# =============================================================================
#  Step 2b — Create Gateway resources
# =============================================================================
banner "🌐  Step 2b — Create Gateway resources"

kubectl apply -f "${REPO_DIR}/manifests/agentgateway/gateway.yaml"
kubectl apply -f "${REPO_DIR}/manifests/agentgateway/agentgatewaypolicy.yaml"
ok "Gateway and AgentGatewayPolicy applied"

# =============================================================================
#  Step 3 — Store the OpenAI API key
# =============================================================================
banner "🔑  Step 3 — Store the OpenAI API key"

for ns in agentgateway-system kagent intelligent-router-system; do
  kubectl create namespace "$ns" 2>/dev/null || true

  if [[ "$ns" == "agentgateway-system" ]]; then
    # AgentGateway uses this as an HTTP Authorization header → needs "Bearer " prefix
    kubectl create secret generic openai-secret \
      --from-literal="Authorization=Bearer ${OPENAI_API_KEY}" \
      --namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -
  else
    # Other namespaces use the raw API key (no "Bearer " prefix)
    kubectl create secret generic openai-secret \
      --from-literal="OPENAI_API_KEY=${OPENAI_API_KEY}" \
      --namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -
  fi
  ok "openai-secret configured in ${ns}"
done

# =============================================================================
#  Step 3b — Store the HuggingFace token
# =============================================================================
banner "🤗  Step 3b — Store the HuggingFace token"

kubectl create namespace intelligent-router-system 2>/dev/null || true

kubectl create secret generic huggingface-api-key \
  --from-literal="HF_TOKEN=${HF_TOKEN}" \
  --namespace intelligent-router-system --dry-run=client -o yaml | kubectl apply -f -
ok "huggingface-api-key configured"

# =============================================================================
#  Step 4 — Install Kagent
# =============================================================================
banner "🤖  Step 4 — Install Kagent"

if command -v kagent &>/dev/null; then
  ok "kagent CLI already installed — skipping download"
else
  info "Downloading kagent installer …"
  curl -sL https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash
fi

info "Running kagent install …"
kagent install
ok "Kagent installed"

# =============================================================================
#  Step 5 — Deploy lab manifests
# =============================================================================
banner "📋  Step 5 — Deploy lab manifests"

info "5.1  Creating MCPs …"
kubectl apply -f "${REPO_DIR}/manifests/mcps/openrouter-pricing-mcp.yaml"
kubectl apply -f "${REPO_DIR}/manifests/mcps/spain-electricity-cost-mcp.yaml"

info "Waiting for MCPs to become ready …"
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent openrouter-pricing-mcp --timeout=120s
kubectl wait --for=condition=ready mcpservers.kagent.dev -n kagent spain-electricity-cost-mcp --timeout=120s
ok "MCPs are ready"

info "5.2  Creating configuration agents …"
kubectl apply -f "${REPO_DIR}/manifests/agents/k8s-agent.yaml"
kubectl apply -f "${REPO_DIR}/manifests/agents/orchestrator-agent.yaml"
kubectl apply -f "${REPO_DIR}/manifests/agents/orchestrator-rbac.yaml"
kubectl apply -f "${REPO_DIR}/manifests/agents/eval-job-agent.yaml"
kubectl apply -f "${REPO_DIR}/manifests/agents/model-cost-agent.yaml"
kubectl apply -f "${REPO_DIR}/manifests/agents/sp-electricity-cost-agent.yaml"
ok "Agents deployed"

# =============================================================================
#  Step 6 — Install the Backend Evaluation Operator
# =============================================================================
banner "⚙️   Step 6 — Install Backend Evaluation Operator"

kubectl apply -f "${REPO_DIR}/manifests/model-agentic-controller/crd-agentic-controller.yaml"
ok "Agentic controller CRD applied"

# =============================================================================
#  Step 7 — Deploy the Intelligent Router
# =============================================================================
banner "🧠  Step 7 — Deploy the Intelligent Router"

kubectl apply -f "${REPO_DIR}/manifests/intelligent-router/namespace.yaml"
kubectl apply -f "${REPO_DIR}/manifests/intelligent-router/rbac.yaml"
kubectl apply -f "${REPO_DIR}/manifests/intelligent-router/service.yaml"
kubectl apply -f "${REPO_DIR}/manifests/intelligent-router/statefulset.yaml"
ok "Intelligent Router deployed"

# =============================================================================
#  Step 8 — Deploy monitoring (Prometheus + Grafana + OTel Collector)
# =============================================================================
banner "📊  Step 8 — Deploy monitoring"

kubectl apply -f "${REPO_DIR}/manifests/observability/namespace.yaml"
kubectl apply -f "${REPO_DIR}/manifests/observability/prometheus.yaml"
kubectl apply -f "${REPO_DIR}/manifests/observability/otel-collector.yaml"
kubectl apply -f "${REPO_DIR}/manifests/observability/grafana.yaml"

info "Waiting for monitoring pods …"
kubectl rollout status deployment/prometheus -n monitoring --timeout=60s
kubectl rollout status deployment/otel-collector -n monitoring --timeout=60s
kubectl rollout status deployment/grafana -n monitoring --timeout=60s
ok "Monitoring stack deployed (Prometheus + OTel Collector + Grafana)"

# =============================================================================
#  🎉  What's Next
# =============================================================================
banner "🎉  Setup complete! — What's next?"

cat <<'EOF'
The infrastructure is up and running. Continue with these remaining steps:

  📌  Step 9 — Create LLM Backends
      kubectl apply -f manifests/llmbackend/gpt-3.5-turbo-public.yaml
      kubectl apply -f manifests/llmbackend/gpt-4.1-mini-public.yaml
      kubectl apply -f manifests/llmbackend/gpt-4.1-public.yaml
      kubectl apply -f manifests/llmbackend/gpt-5-mini-public.yaml
      kubectl apply -f manifests/llmbackend/gpt-oss-120b-local.yaml

  📌  Step 10 — Test the intelligent routing
      export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system \
        agentgateway-proxy -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
      curl -si -X POST $INGRESS_GW_ADDRESS/v1/chat/completions \
        -H "content-type: application/json" \
        -d '{"model":"auto","messages":[{"role":"user","content":"Hello, how are you?"}]}'

  📊  Grafana is available at:
      kubectl port-forward svc/grafana -n monitoring 3000:3000 &
      Open http://localhost:3000 (login: admin / admin)

Refer to the README for the full instructions and demo scripts.
EOF
