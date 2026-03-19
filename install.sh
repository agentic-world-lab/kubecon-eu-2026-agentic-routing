#!/usr/bin/env bash
# =============================================================================
#  KubeCon EU 2026 — Intelligent Routing Lab  ·  Remote Installer
#  Usage: curl https://raw.githubusercontent.com/felipevicens/kubecon-eu-2026-agentic-routing/main/install.sh | bash
# =============================================================================
set -euo pipefail

REPO_URL="https://github.com/agentic-world-lab/kubecon-eu-2026-agentic-routing.git"
INSTALL_DIR="${INSTALL_DIR:-kubecon-eu-2026-agentic-routing}"

# ── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
info() { echo -e "${CYAN}ℹ ${NC} $*"; }
ok()   { echo -e "${GREEN}✔ ${NC} $*"; }
err()  { echo -e "${RED}✖ ${NC} $*" >&2; }

# ── Pre-flight ───────────────────────────────────────────────────────────────
if ! command -v git &>/dev/null; then
  err "git is not installed. Please install git and re-run."
  exit 1
fi

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  err "Environment variable OPENAI_API_KEY is not set."
  echo "   export OPENAI_API_KEY=\"sk-…\""
  exit 1
fi

if [[ -z "${HF_TOKEN:-}" ]]; then
  err "Environment variable HF_TOKEN is not set."
  echo "   export HF_TOKEN=\"hf_…\""
  exit 1
fi

# ── Clone ────────────────────────────────────────────────────────────────────
if [[ -d "${INSTALL_DIR}" ]]; then
  info "Directory '${INSTALL_DIR}' already exists — pulling latest changes …"
  git -C "${INSTALL_DIR}" pull --ff-only
else
  info "Cloning repository …"
  git clone "${REPO_URL}" "${INSTALL_DIR}"
fi
ok "Repository ready at ./${INSTALL_DIR}"

# ── Run setup ────────────────────────────────────────────────────────────────
info "Launching setup.sh …"
echo ""
exec bash "${INSTALL_DIR}/setup.sh"
