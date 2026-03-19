#!/usr/bin/env bash
# =============================================================================
#  KubeCon EU 2026 — Intelligent Routing Lab  ·  Uninstall (reverse of setup.sh)
# =============================================================================
set -uo pipefail   # no -e: we want to continue even if some deletes 404

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

# ── Safety prompt ────────────────────────────────────────────────────────────
banner "⚠️   Uninstall — KubeCon Intelligent Routing Lab"

echo -e "${YELLOW}This will remove ALL lab resources from the cluster.${NC}"
read -rp "Are you sure you want to continue? [y/N] " confirm
if [[ "${confirm,,}" != "y" ]]; then
  echo "Aborted."
  exit 0
fi

# =============================================================================
#  Step 8 (reverse) — Remove monitoring
# =============================================================================
banner "📊  Removing monitoring stack"

kubectl delete -f "${REPO_DIR}/manifests/observability/grafana.yaml"        --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/observability/otel-collector.yaml"  --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/observability/prometheus.yaml"      --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/observability/namespace.yaml"       --ignore-not-found
ok "Monitoring removed"

# =============================================================================
#  Step 7 (reverse) — Remove the Intelligent Router
# =============================================================================
banner "🧠  Removing Intelligent Router"

kubectl delete -f "${REPO_DIR}/manifests/intelligent-router/statefulset.yaml" --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/intelligent-router/service.yaml"     --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/intelligent-router/rbac.yaml"        --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/intelligent-router/namespace.yaml"   --ignore-not-found
ok "Intelligent Router removed"

# =============================================================================
#  Step 6 (reverse) — Remove the Backend Evaluation Operator
# =============================================================================
banner "⚙️   Removing Backend Evaluation Operator"

kubectl delete -f "${REPO_DIR}/manifests/model-agentic-controller/crd-agentic-controller.yaml" --ignore-not-found
ok "Agentic controller CRD removed"

# =============================================================================
#  Step 5 (reverse) — Remove lab manifests (agents + MCPs)
# =============================================================================
banner "📋  Removing lab manifests"

info "Removing agents …"
kubectl delete -f "${REPO_DIR}/manifests/agents/sp-electricity-cost-agent.yaml" --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/agents/model-cost-agent.yaml"          --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/agents/eval-job-agent.yaml"            --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/agents/orchestrator-rbac.yaml"         --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/agents/orchestrator-agent.yaml"        --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/agents/k8s-agent.yaml"                 --ignore-not-found
ok "Agents removed"

info "Removing MCPs …"
kubectl delete -f "${REPO_DIR}/manifests/mcps/spain-electricity-cost-mcp.yaml"  --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/mcps/openrouter-pricing-mcp.yaml"      --ignore-not-found
ok "MCPs removed"

# =============================================================================
#  Step 4 (reverse) — Uninstall Kagent
# =============================================================================
banner "🤖  Uninstalling Kagent"

if command -v kagent &>/dev/null; then
  kagent uninstall || warn "kagent uninstall returned non-zero (may already be removed)"
  ok "Kagent uninstalled"
else
  warn "kagent CLI not found — skipping"
fi

# =============================================================================
#  Step 3b (reverse) — Remove HuggingFace secret
# =============================================================================
banner "🤗  Removing HuggingFace secret"

kubectl delete secret huggingface-api-key -n intelligent-router-system --ignore-not-found
ok "huggingface-api-key removed"

# =============================================================================
#  Step 3 (reverse) — Remove OpenAI secrets
# =============================================================================
banner "🔑  Removing OpenAI secrets"

for ns in intelligent-router-system kagent agentgateway-system; do
  kubectl delete secret openai-secret -n "$ns" --ignore-not-found
  ok "openai-secret removed from ${ns}"
done

# =============================================================================
#  Step 2b (reverse) — Remove Gateway resources
# =============================================================================
banner "🌐  Removing Gateway resources"

kubectl delete -f "${REPO_DIR}/manifests/agentgateway/agentgatewaypolicy.yaml" --ignore-not-found
kubectl delete -f "${REPO_DIR}/manifests/agentgateway/gateway.yaml"            --ignore-not-found
ok "Gateway and AgentGatewayPolicy removed"

# =============================================================================
#  Step 2 (reverse) — Uninstall AgentGateway
# =============================================================================
banner "📦  Uninstalling AgentGateway"

if helm status agentgateway -n agentgateway-system &>/dev/null; then
  helm uninstall agentgateway -n agentgateway-system
  ok "agentgateway uninstalled"
else
  warn "Helm release 'agentgateway' not found — skipping"
fi

if helm status agentgateway-crds -n agentgateway-system &>/dev/null; then
  helm uninstall agentgateway-crds -n agentgateway-system
  ok "agentgateway-crds uninstalled"
else
  warn "Helm release 'agentgateway-crds' not found — skipping"
fi

info "Removing Gateway API CRDs …"
kubectl delete -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml --ignore-not-found
ok "Gateway API CRDs removed"

# =============================================================================
#  Clean up namespaces
# =============================================================================
banner "🧹  Cleaning up namespaces"

for ns in intelligent-router-system agentgateway-system kagent monitoring; do
  if kubectl get namespace "$ns" &>/dev/null; then
    kubectl delete namespace "$ns" --ignore-not-found
    ok "Namespace ${ns} deleted"
  else
    warn "Namespace ${ns} not found — skipping"
  fi
done

# =============================================================================
#  Done
# =============================================================================
banner "🎉  Uninstall complete!"

echo -e "All lab resources have been removed from the cluster."
echo -e "Your cluster is back to a clean state.\n"
