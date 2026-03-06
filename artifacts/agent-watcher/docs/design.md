# Design Document: Kagent Event-Driven Trigger
**Version:** 1.0.0
**Date:** 2026-03-01

## 1. Objective
Create a Kubernetes-native "Watcher" service that bridges Kubernetes Events to a `kagent` agent via the A2A JSON-RPC protocol.

## 2. Technical Stack
* **Runtime:** Python 3.10+
* **Communication:** JSON-RPC 2.0 over HTTP (A2A Protocol)
* **Kubernetes Integration:** `kubernetes-python-client` (Watch API)

## 3. Trigger Criteria
The Watcher must monitor the `default` namespace for the following Event:
- **API Resource:** `v1/Event`
- **Reason:** `EvaluationCompleted`
- **Message Filter:** Matches regex/string `{{model_name}}`
- **Target Logic:** Extract accuracy and performance metrics from the message string.

## 4. Destination (kagent) Specification
- **Endpoint:** `http://kagent-controller:8083/api/a2a/kagent/sp-power-cost-agent/`
- **Protocol:** JSON-RPC 2.0
- **Method:** `message/stream`
- **Payload Schema:**
```json
{
  "jsonrpc": "2.0",
  "id": "watcher-task-{{timestamp}}",
  "method": "message/stream",
  "params": {
    "message": {
      "role": "user",
      "parts": [
        {
          "kind": "text",
          "text": "The model evaluation for {{model_name}} is complete. Details: {{RAW_EVENT_MESSAGE}}"
        }
      ]
    }
  }
}
```

## 5. Security & RBAC

The application must run under a ServiceAccount with the following permissions:
 - Verbs: `list`, `watch`
 - Resources: `events`
 - ApiGroups: ""

## 6. Development Instructions for Agent
- Implement `watcher.py` with non-blocking event streaming.
- Include a 5-second backoff retry logic for connection errors to the kagent controller.
- Ensure the contextId from the kagent response is logged for traceability.