"""
Agent Webhook Bridge

Intercepts AgentgatewayBackend CR changes and calls a kagent agent to
automatically configure the semantic router (IntelligentPool + IntelligentRoute)
based on annotations on the backend.

Annotations convention on AgentgatewayBackend:
  router.vllm.ai/enabled: "true"           # opt-in to router management
  router.vllm.ai/domains: "math,physics"   # comma-separated domain list
  router.vllm.ai/cost: "0.002"             # cost per token
  router.vllm.ai/latency-ms: "120"         # average latency in ms

Flow:
  kubectl apply AgentgatewayBackend
       │
       ▼
  ValidatingWebhook ──► Bridge ──► kagent A2A ──► Agent LLM
                                                      │
                                                      ▼
                                               k8s_apply_manifest
                                               (creates/updates IntelligentPool
                                                and IntelligentRoute CRs)
       ◄── always allow ◄──────────────────────────────
"""

import json
import logging
import os
import ssl
import uuid
from http.server import HTTPServer, BaseHTTPRequestHandler

import requests
import yaml

logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO").upper(),
    format="%(asctime)s %(levelname)s %(message)s",
)
log = logging.getLogger("agent-webhook-bridge")

KAGENT_ENDPOINT = os.getenv(
    "KAGENT_ENDPOINT",
    "http://kagent-controller.kagent.svc.cluster.local:8083",
)
AGENT_NAMESPACE = os.getenv("AGENT_NAMESPACE", "kagent")
AGENT_NAME = os.getenv("AGENT_NAME", "intelligent-router-configurator")
ROUTER_NAMESPACE = os.getenv("ROUTER_NAMESPACE", "vllm-semantic-router-system")
TIMEOUT_SECONDS = int(os.getenv("TIMEOUT_SECONDS", "90"))
PORT = int(os.getenv("PORT", "8443"))
TLS_CERT = os.getenv("TLS_CERT_FILE", "/certs/tls.crt")
TLS_KEY = os.getenv("TLS_KEY_FILE", "/certs/tls.key")

# Annotation prefix
ANN_PREFIX = "router.vllm.ai"


def invoke_agent(prompt: str) -> str:
    """Call the kagent agent via A2A and return the agent's text response."""
    url = f"{KAGENT_ENDPOINT}/api/a2a/{AGENT_NAMESPACE}/{AGENT_NAME}"
    payload = {
        "jsonrpc": "2.0",
        "id": f"bridge-{uuid.uuid4().hex[:12]}",
        "method": "tasks/send",
        "params": {
            "message": {
                "role": "user",
                "parts": [{"kind": "text", "text": prompt}],
            }
        },
    }

    log.info("Calling agent at %s", url)

    try:
        resp = requests.post(url, json=payload, timeout=TIMEOUT_SECONDS)
    except requests.RequestException as exc:
        log.error("Agent call failed: %s", exc)
        return f"ERROR: agent unreachable: {exc}"

    if resp.status_code != 200:
        log.error("Agent returned %d: %s", resp.status_code, resp.text[:500])
        return f"ERROR: agent returned status {resp.status_code}"

    try:
        a2a_resp = resp.json()
    except ValueError:
        return "ERROR: agent response not JSON"

    if "error" in a2a_resp and a2a_resp["error"]:
        return f"ERROR: {a2a_resp['error'].get('message', 'unknown')}"

    return _extract_text(a2a_resp)


def _extract_text(resp: dict) -> str:
    result = resp.get("result")
    if not result:
        return ""
    for artifact in result.get("artifacts", []):
        for part in artifact.get("parts", []):
            if part.get("kind") == "text" and part.get("text"):
                return part["text"]
    msg = result.get("status", {}).get("message")
    if msg:
        for part in msg.get("parts", []):
            if part.get("kind") == "text" and part.get("text"):
                return part["text"]
    return ""


def extract_router_info(obj: dict) -> dict | None:
    """Extract router annotations from an AgentgatewayBackend object.
    Returns None if the backend is not opted-in to router management."""
    metadata = obj.get("metadata", {})
    annotations = metadata.get("annotations", {})
    labels = metadata.get("labels", {})

    # Check opt-in via label or annotation
    enabled = (
        labels.get(f"{ANN_PREFIX}/enabled", "")
        or annotations.get(f"{ANN_PREFIX}/enabled", "")
    )
    if enabled.lower() != "true":
        return None

    name = metadata.get("name", "unknown")
    domains_raw = annotations.get(f"{ANN_PREFIX}/domains", "")
    domains = [d.strip() for d in domains_raw.split(",") if d.strip()]
    cost = annotations.get(f"{ANN_PREFIX}/cost", "0.001")
    latency = annotations.get(f"{ANN_PREFIX}/latency-ms", "100")

    # Try to extract the model name from the spec
    model_name = _extract_model_name(obj) or name

    return {
        "backend_name": name,
        "model_name": model_name,
        "domains": domains,
        "cost": cost,
        "latency_ms": latency,
    }


def _extract_model_name(obj: dict) -> str:
    """Best-effort extraction of model name from AgentgatewayBackend spec."""
    spec = obj.get("spec", {})

    # Try spec.ai.groups[].providers[].openai.model
    ai = spec.get("ai", {})
    for group in ai.get("groups", []):
        for provider in group.get("providers", []):
            for api in ("openai", "anthropic", "gemini"):
                if api in provider:
                    model = provider[api].get("model", "")
                    if model:
                        return model

    # Try spec.ai.provider.openai.model
    provider = ai.get("provider", {})
    for api in ("openai", "anthropic", "gemini"):
        if api in provider:
            model = provider[api].get("model", "")
            if model:
                return model

    return ""


def build_prompt(info: dict, operation: str) -> str:
    """Build the prompt for the kagent agent."""
    domains_str = ", ".join(info["domains"]) if info["domains"] else "general"

    return f"""A new AgentgatewayBackend has been {operation.lower()}d in the cluster.
Your job is to update the semantic router configuration so it knows about this model.

Backend details:
- Backend name: {info["backend_name"]}
- Model name: {info["model_name"]}
- Domains (what this model is good at): {domains_str}
- Cost per token: {info["cost"]}
- Average latency: {info["latency_ms"]}ms

You MUST do the following:
1. GET the current IntelligentPool named "router-pool" in namespace "{ROUTER_NAMESPACE}".
   If it does not exist, create it.
2. Add or update the model "{info["model_name"]}" in the pool with the pricing above.
3. GET the current IntelligentRoute named "router-route" in namespace "{ROUTER_NAMESPACE}".
   If it does not exist, create it.
4. For each domain ({domains_str}), ensure there is:
   - A domain signal in spec.signals.domains
   - A decision that routes that domain to model "{info["model_name"]}"
5. Apply both CRs using k8s_apply_manifest.

Use apiVersion: vllm.ai/v1alpha1 for both resources.
Namespace: {ROUTER_NAMESPACE}

Do NOT remove existing models or decisions — only add or update the ones for this backend."""


def build_response(uid: str, allowed: bool, message: str = "") -> dict:
    resp = {
        "apiVersion": "admission.k8s.io/v1",
        "kind": "AdmissionReview",
        "response": {"uid": uid, "allowed": allowed},
    }
    if not allowed and message:
        resp["response"]["status"] = {"code": 403, "message": message}
    return resp


class WebhookHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path == "/validate":
            self._handle_validate()
        else:
            self.send_error(404)

    def do_GET(self):
        if self.path == "/healthz":
            self._send_text(200, "ok")
        else:
            self.send_error(404)

    def _handle_validate(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))

        try:
            review = json.loads(body)
        except json.JSONDecodeError:
            self.send_error(400, "invalid JSON")
            return

        req = review.get("request")
        if not req:
            self._send_json(build_response("", True))
            return

        uid = req.get("uid", "")
        kind = req.get("kind", {}).get("kind", "Unknown")
        name = req.get("name", "unknown")
        operation = req.get("operation", "UNKNOWN")

        log.info("Admission: %s %s (%s)", operation, name, kind)

        # Always allow the AgentgatewayBackend — we are not blocking, just reacting
        obj = req.get("object", {})
        info = extract_router_info(obj)

        if info is None:
            log.info("Backend %s has no router.vllm.ai/enabled=true, skipping", name)
            self._send_json(build_response(uid, True))
            return

        log.info(
            "Backend %s opted in: model=%s domains=%s cost=%s latency=%sms",
            name, info["model_name"], info["domains"], info["cost"], info["latency_ms"],
        )

        # Call the agent asynchronously — don't block the admission
        # We spawn in a thread so the webhook responds quickly
        import threading

        def _call_agent():
            prompt = build_prompt(info, operation)
            agent_response = invoke_agent(prompt)
            log.info("Agent finished for %s: %s", name, agent_response[:200])

        thread = threading.Thread(target=_call_agent, daemon=True)
        thread.start()

        # Always admit
        self._send_json(build_response(uid, True))

    def _send_json(self, data: dict):
        body = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_text(self, code: int, text: str):
        body = text.encode()
        self.send_response(code)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        pass


def main():
    server = HTTPServer(("0.0.0.0", PORT), WebhookHandler)

    if os.path.isfile(TLS_CERT) and os.path.isfile(TLS_KEY):
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(TLS_CERT, TLS_KEY)
        server.socket = ctx.wrap_socket(server.socket, server_side=True)
        log.info("Starting with TLS on :%d", PORT)
    else:
        log.info("Starting without TLS on :%d", PORT)

    log.info("  kagent: %s/api/a2a/%s/%s", KAGENT_ENDPOINT, AGENT_NAMESPACE, AGENT_NAME)
    log.info("  router namespace: %s", ROUTER_NAMESPACE)

    server.serve_forever()


if __name__ == "__main__":
    main()
