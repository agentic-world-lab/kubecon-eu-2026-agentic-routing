#!/usr/bin/env python3
"""
Kubernetes Event & Custom Resource Watcher → kagent A2A Bridge
=============================================================
1. Watches for `v1/Events` with reason `EvaluationCompleted` (Completions).
2. Watches for `LLMBackend` resource `DELETED` signals (Deletions).
Forward results via A2A JSON-RPC 2.0.

Design reference: docs/design.md
"""

import json
import logging
import os
import sys
import threading
import time
import uuid
from datetime import datetime, timezone

import requests
import urllib3
from kubernetes import client, config, watch

# ---------------------------------------------------------------------------
# Configuration (overridable via environment variables)
# ---------------------------------------------------------------------------
WATCH_NAMESPACE: str = os.getenv("WATCH_NAMESPACE", "default")
EVENT_REASON: str = os.getenv("EVENT_REASON", "EvaluationCompleted")

KAGENT_ENDPOINT: str = os.getenv(
    "KAGENT_ENDPOINT",
    "http://kagent-controller:8083/api/a2a/kagent/sr-config-agent/",
)

# CRD Settings
CRD_GROUP = "edgecloudlabs.edgecloudlabs.com"
CRD_VERSION = "v1alpha1"
CRD_PLURAL = "llmbackends"

# Retry / backoff settings
HTTP_MAX_RETRIES: int = int(os.getenv("HTTP_MAX_RETRIES", "5"))
HTTP_BACKOFF_SECONDS: float = float(os.getenv("HTTP_BACKOFF_SECONDS", "5"))
STREAM_RECONNECT_BACKOFF_SECONDS: float = float(
    os.getenv("STREAM_RECONNECT_BACKOFF_SECONDS", "5")
)

# HTTP timeout for the streaming response (seconds)
HTTP_STREAM_TIMEOUT: int = int(os.getenv("HTTP_STREAM_TIMEOUT", "60"))

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(threadName)-12s] %(levelname)-8s %(message)s",
    datefmt="%Y-%m-%dT%H:%M:%SZ",
    stream=sys.stdout,
)
log = logging.getLogger("event-watcher")


# ---------------------------------------------------------------------------
# kagent A2A call
# ---------------------------------------------------------------------------

def _build_payload(model_name: str, results: dict) -> dict:
    """Build a JSON-RPC 2.0 message/stream payload for add/delete."""
    task_id = f"watcher-task-{datetime.now(tz=timezone.utc).strftime('%Y%m%dT%H%M%SZ')}-{uuid.uuid4().hex[:8]}"
    
    action = results.get("action", "add")
    message_content = {
        "action": action,
        "model": model_name
    }

    if action == "add":
        metrics = results.get("results", {})
        message_content.update({
            "deployment": results.get("deployment", "local"),
            "avgResponseTime": metrics.get("avgResponseTime", "0"),
            "categoryAccuracy": metrics.get("categoryAccuracy", {}),
            "overallAccuracy": metrics.get("overallAccuracy", "0"),
            "tokPerSecond": metrics.get("tokPerSecond", "0"),
        })

    return {
        "jsonrpc": "2.0",
        "id": task_id,
        "method": "message/stream",
        "params": {
            "message": {
                "role": "user",
                "parts": [
                    {
                        "kind": "text",
                        "text": json.dumps(message_content, indent=2),
                    }
                ],
            }
        },
    }


def _extract_context_id(response_text: str) -> str | None:
    """Parse SSE/NDJSON stream from agent and extract contextId."""
    for line in response_text.splitlines():
        line = line.strip()
        if not line: continue
        if line.startswith("data:"):
            line = line[len("data:"):].strip()
        elif line.startswith("event:") or line.startswith("id:") or line.startswith(":"):
            continue

        try:
            obj = json.loads(line)
        except json.JSONDecodeError: continue

        for path in (
            ["result", "contextId"],
            ["result", "status", "contextId"],
            ["params", "contextId"],
        ):
            node = obj
            try:
                for key in path: node = node[key]
                if isinstance(node, str): return node
            except (KeyError, TypeError): continue
    return None


def call_kagent(model_name: str, results: dict) -> None:
    """POST JSON-RPC request to kagent with retry logic."""
    payload = _build_payload(model_name, results)
    log.info("Calling kagent | action=%s | model=%s", results.get("action"), model_name)

    last_exc = None
    for attempt in range(1, HTTP_MAX_RETRIES + 1):
        try:
            with requests.post(
                KAGENT_ENDPOINT, json=payload, stream=True,
                headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream"},
                timeout=HTTP_STREAM_TIMEOUT
            ) as resp:
                resp.raise_for_status()
                chunks = [c for c in resp.iter_lines(decode_unicode=True) if c]
                context_id = _extract_context_id("\n".join(chunks))
                if context_id:
                    log.info("kagent responded | model=%s | contextId=%s", model_name, context_id)
                return
        except Exception as exc:
            last_exc = exc
            wait = HTTP_BACKOFF_SECONDS * attempt
            log.warning("kagent call failed (attempt %d/%d), retrying in %.0fs", attempt, HTTP_MAX_RETRIES, wait)
            time.sleep(wait)

    log.error("kagent call failed after %d attempts: %s", HTTP_MAX_RETRIES, last_exc)


# ---------------------------------------------------------------------------
# Kubernetes Helpers
# ---------------------------------------------------------------------------

def _load_kube_config():
    try: config.load_incluster_config()
    except: config.load_kube_config()


def _get_current_resource_version(fn, **kwargs) -> str:
    """Pin the current resourceVersion so we don't replay old history."""
    while True:
        try:
            res = fn(**kwargs, limit=1)
            # Prioritize dictionary check (CustomObjects API returns plain dicts)
            if isinstance(res, dict):
                rv = res.get('metadata', {}).get('resourceVersion')
            # Fallback to object attribute check (CoreV1 API returns objects)
            elif hasattr(res, 'metadata'):
                # Handle camelCase or snake_case if necessary, but CoreV1 is usually snake_case
                metadata = getattr(res, 'metadata', None)
                rv = getattr(metadata, 'resource_version', None)
            else:
                rv = "0"
            
            log.info("Established bookmark: %s", rv or "0")
            return rv or "0"
        except Exception as exc:
            log.warning("Failed to establish bookmark (retrying in %.0fs): %s", STREAM_RECONNECT_BACKOFF_SECONDS, exc)
            time.sleep(STREAM_RECONNECT_BACKOFF_SECONDS)


def _extract_model_name(obj) -> str:
    """Determine the logical model name from LLMBackend or Event metadata."""
    # 1. Check spec.backendName (for LLMBackend objects)
    try:
        if "spec" in obj and "backendName" in obj["spec"]:
            return obj["spec"]["backendName"]
    except: pass
    
    # 2. Check involvedObject (for Event objects)
    try:
        name = obj.involved_object.name
        if name: return name
    except AttributeError: pass
    
    # 3. Check metadata.name (strip suffix)
    try:
        name = obj.metadata.name or ""
        if "." in name: return name.rsplit(".", 1)[0]
    except AttributeError: pass
    
    return "unknown"


# ---------------------------------------------------------------------------
# Watcher 1: v1/Events (EvaluationCompleted)
# ---------------------------------------------------------------------------

def watch_evaluation_events():
    """Watches for EvaluationCompleted Events (Action: add)."""
    v1 = client.CoreV1Api()
    custom_v1 = client.CustomObjectsApi()
    
    log.info("Watcher-Events started | namespace=%s | reason=%s", WATCH_NAMESPACE, EVENT_REASON)
    rv = _get_current_resource_version(v1.list_namespaced_event, namespace=WATCH_NAMESPACE)

    while True:
        w = watch.Watch()
        try:
            for raw_event in w.stream(v1.list_namespaced_event, namespace=WATCH_NAMESPACE, resource_version=rv, timeout_seconds=300):
                obj = raw_event["object"]
                rv = obj.metadata.resource_version or rv
                
                if raw_event["type"] not in ("ADDED", "MODIFIED") or obj.reason != EVENT_REASON:
                    continue

                # When evaluation is complete, fetch the latest resource and send its results
                model_resource_name = _extract_model_name(obj)
                namespace = obj.involved_object.namespace or WATCH_NAMESPACE
                
                try:
                    res = custom_v1.get_namespaced_custom_object(CRD_GROUP, CRD_VERSION, namespace, CRD_PLURAL, model_resource_name)
                    spec, status = res.get("spec", {}), res.get("status", {})
                    metrics = status.get("results")
                    if not metrics:
                        log.warning("Evaluation event matched, but no results found yet in object %s", model_resource_name)
                        continue

                    call_kagent(
                        model_name=spec.get("backendName") or spec.get("model") or model_resource_name,
                        results={"action": "add", "results": metrics, "deployment": spec.get("deployment", "local")}
                    )
                except Exception as exc:
                    log.error("Failed to fetch full results for %s: %s", model_resource_name, exc)

        except client.exceptions.ApiException as exc:
            if exc.status == 410:
                log.warning("Event stream expired (410). Re-acquiring bookmark.")
                rv = _get_current_resource_version(v1.list_namespaced_event, namespace=WATCH_NAMESPACE)
            else:
                log.warning("Event stream API error: %s. Reconnecting...", exc)
                time.sleep(STREAM_RECONNECT_BACKOFF_SECONDS)
        except Exception as exc:
            log.warning("Event stream interrupted: %s. Reconnecting...", exc)
            time.sleep(STREAM_RECONNECT_BACKOFF_SECONDS)
        finally: w.stop()


# ---------------------------------------------------------------------------
# Watcher 2: LLMBackend Custom Objects (Deletion)
# ---------------------------------------------------------------------------

def watch_llmbackend_deletions():
    """Watches Custom Resource directly (Action: delete)."""
    custom_v1 = client.CustomObjectsApi()
    
    log.info("Watcher-Deletions started | group=%s", CRD_GROUP)
    # We use list_namespaced_custom_object directly.
    # Note: custom objects API returns metadata differently.
    rv = _get_current_resource_version(custom_v1.list_namespaced_custom_object, group=CRD_GROUP, version=CRD_VERSION, namespace=WATCH_NAMESPACE, plural=CRD_PLURAL)

    while True:
        w = watch.Watch()
        try:
            for raw_event in w.stream(custom_v1.list_namespaced_custom_object, CRD_GROUP, CRD_VERSION, WATCH_NAMESPACE, CRD_PLURAL, resource_version=rv, timeout_seconds=300):
                obj = raw_event["object"]
                rv = obj.get("metadata", {}).get("resourceVersion", rv)

                if raw_event["type"] == "DELETED":
                    spec = obj.get("spec", {})
                    # Prefer backendName, fallback to model name or resource name
                    backend_name = spec.get("backendName") or spec.get("model") or obj["metadata"]["name"]
                    
                    log.info("Resource deleted | model=%s", backend_name)
                    call_kagent(backend_name, {"action": "delete"})
                    
        except client.exceptions.ApiException as exc:
            if exc.status == 410:
                log.warning("CRD watch stream expired (410). Re-acquiring bookmark.")
                rv = _get_current_resource_version(custom_v1.list_namespaced_custom_object, group=CRD_GROUP, version=CRD_VERSION, namespace=WATCH_NAMESPACE, plural=CRD_PLURAL)
            else:
                log.warning("CRD watch stream API error: %s. Reconnecting...", exc)
                time.sleep(STREAM_RECONNECT_BACKOFF_SECONDS)
        except Exception as exc:
            log.warning("CRD watch stream interrupted: %s. Reconnecting...", exc)
            time.sleep(STREAM_RECONNECT_BACKOFF_SECONDS)
        finally: w.stop()


# ---------------------------------------------------------------------------
# Main Entry Point
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    _load_kube_config()
    
    # Start both watchers in separate threads
    t1 = threading.Thread(target=watch_evaluation_events, name="Completion-Watch", daemon=True)
    t2 = threading.Thread(target=watch_llmbackend_deletions, name="Deletion-Watch", daemon=True)
    
    t1.start()
    t2.start()
    
    # Keep the main process alive
    try:
        while True: time.sleep(1)
    except KeyboardInterrupt:
        log.info("Shutting down...")
