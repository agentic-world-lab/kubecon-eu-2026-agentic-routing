# Backend Evaluator Agent

A pure A2A orchestrator agent that manages the LLM backend evaluation lifecycle.

## Architecture

This agent does **not** interact with Kubernetes directly. All operations are
delegated via A2A to other kagent agents:

- **K8s Agent** — creates backends, launches eval jobs, collects results, updates CR status
- **model-cost-agent** — token pricing from OpenRouter
- **sp-electricity-cost-agent** — electricity cost from ESIOS

## Build

```bash
docker build -t fjvicens/agentic-controller-router:latest .
docker push fjvicens/agentic-controller-router:latest
```

## Deploy

```bash
kubectl apply -f agent.yaml
```
