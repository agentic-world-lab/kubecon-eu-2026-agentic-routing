"""
A2A JSON-RPC 2.0 client for calling kagent agents.

Sends `message/stream` requests and extracts the final text result
from the SSE/NDJSON response stream.
"""

import json
import logging
import os
import time
import uuid
from datetime import datetime, timezone

import requests

log = logging.getLogger(__name__)

KAGENT_BASE_URL: str = os.getenv(
    "KAGENT_BASE_URL",
    "http://kagent-controller:8083/api/a2a/kagent",
)
HTTP_MAX_RETRIES: int = int(os.getenv("A2A_MAX_RETRIES", "3"))
HTTP_BACKOFF_SECONDS: float = float(os.getenv("A2A_BACKOFF_SECONDS", "3"))
HTTP_TIMEOUT: int = int(os.getenv("A2A_TIMEOUT", "120"))


def _build_payload(message_text: str) -> dict:
    """Build a JSON-RPC 2.0 message/stream payload."""
    task_id = (
        f"eval-agent-{datetime.now(tz=timezone.utc).strftime('%Y%m%dT%H%M%SZ')}"
        f"-{uuid.uuid4().hex[:8]}"
    )
    return {
        "jsonrpc": "2.0",
        "id": task_id,
        "method": "message/stream",
        "params": {
            "message": {
                "role": "user",
                "parts": [{"kind": "text", "text": message_text}],
            }
        },
    }


def _extract_final_text(response_text: str) -> str | None:
    """Parse SSE/NDJSON stream and extract the last text content."""
    last_text = None
    for line in response_text.splitlines():
        line = line.strip()
        if not line:
            continue
        if line.startswith("data:"):
            line = line[len("data:"):].strip()
        elif line.startswith(("event:", "id:", ":")):
            continue

        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue

        # Try to extract text from various response shapes
        for parts_path in (
            ["result", "status", "message", "parts"],
            ["result", "artifact", "parts"],
            ["params", "artifact", "parts"],
        ):
            node = obj
            try:
                for key in parts_path:
                    node = node[key]
                if isinstance(node, list):
                    for part in node:
                        if part.get("kind") == "text" and "text" in part:
                            last_text = part["text"]
            except (KeyError, TypeError):
                continue

    return last_text


def call_agent(agent_name: str, message: str) -> str:
    """
    Call a kagent agent via A2A JSON-RPC 2.0.

    Args:
        agent_name: Name of the kagent agent (e.g. 'model-cost-agent').
        message: The text message to send.

    Returns:
        The final text response from the agent.

    Raises:
        RuntimeError: If all retries fail or no text response is found.
    """
    url = f"{KAGENT_BASE_URL}/{agent_name}/"
    payload = _build_payload(message)
    log.info("A2A call | agent=%s | message=%s", agent_name, message[:100])

    # Kagent A2A Authentication Headers
    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
        "X-Agent-Name": os.getenv("KAGENT_NAME", "orchestrator"),
    }

    # Load kagent token if available
    token_path = "/var/run/secrets/tokens/kagent-token"
    if os.path.exists(token_path):
        try:
            with open(token_path, "r") as f:
                token = f.read().strip()
                if token:
                    headers["Authorization"] = f"Bearer {token}"
        except Exception as e:
            log.warning("Could not read kagent-token: %s", e)

    last_exc = None
    for attempt in range(1, HTTP_MAX_RETRIES + 1):
        try:
            with requests.post(
                url,
                json=payload,
                stream=True,
                headers=headers,
                timeout=HTTP_TIMEOUT,
            ) as resp:
                resp.raise_for_status()
                chunks = [c for c in resp.iter_lines(decode_unicode=True) if c]
                raw = "\n".join(chunks)
                result = _extract_final_text(raw)
                if result:
                    log.info(
                        "A2A response | agent=%s | result=%s",
                        agent_name,
                        result[:200],
                    )
                    return result
                log.warning(
                    "A2A call returned no text | agent=%s | raw=%s",
                    agent_name,
                    raw[:500],
                )
                return raw  # Return raw if we can't parse structured text
        except Exception as exc:
            last_exc = exc
            wait = HTTP_BACKOFF_SECONDS * attempt
            log.warning(
                "A2A call failed (attempt %d/%d) | agent=%s | error=%s | retry in %.0fs",
                attempt,
                HTTP_MAX_RETRIES,
                agent_name,
                exc,
                wait,
            )
            time.sleep(wait)

    raise RuntimeError(
        f"A2A call to {agent_name} failed after {HTTP_MAX_RETRIES} attempts: {last_exc}"
    )
