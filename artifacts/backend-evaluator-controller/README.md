# Backend Evaluation Operator

A Kubernetes Operator that orchestrates backend model evaluation lifecycle:

1. **Creates** an `AgentGatewayBackend` CR for the model
2. **Triggers** an MMLU evaluation Job
3. **Collects** structured evaluation results (overall accuracy, tok/s, response time, and per-category accuracy)
4. **Exposes** results in CR status for external agent consumption

## Architecture

```
LLMBackend CR
    ↓ Controller Reconcile Loop
    ├── Ensure AgentGatewayBackend exists
    ├── Ensure MMLU Job exists
    ├── Wait for Job completion
    ├── Collect evaluation results (from pod logs)
    └── Update status
    ↓ External Agent (watches CR)
    ├── Generates routing config
    └── Updates IntelligentRouter CR
```

## Phases

| Phase | Description |
|---|---|
| `""` (empty) | Initial state — creates Backend |
| `BackendCreated` | Backend exists — creates Job |
| `Evaluating` | Job running — waiting for completion |
| `Evaluated` | Results collected — terminal success |
| `Failed` | Terminal failure |

## Prerequisites

- Go 1.23+
- Kubernetes cluster with `AgentGatewayBackend` CRD installed
- Docker (for building images)

## Quick Start

```bash
# Install CRDs
make install

# Run locally (against current kubeconfig)
make run

# Apply a sample LLMBackend
# Local Backend
kubectl apply -f config/samples/gpt-oss-120b-local.yaml

# Remote Backend
kubectl apply -f config/samples/gpt-4.1-public.yaml

# Check status
kubectl get llmbackend -o wide
```

## Build & Deploy

The simplest objective way to deploy the controller is by building a consolidated installation manifest using your registry image.

```bash
# Set your target registry image
export IMG=<your-registry>/backend-evaluation-operator:latest

# Build and Push your controller image
make docker-build docker-push IMG=$IMG

# Generate the static dist/install.yaml containing CRDs, RBAC, and Deployments
make build-installer IMG=$IMG

# Apply the generated manifest to your cluster
kubectl apply -f dist/install.yaml
```

*Alternatively*, developers looking to simply test changes locally without deploying the manager into the cluster can:

```bash
# Just install CRDs
make install

# Run Controller manager strictly out of cluster using local kubeconfig
make run
```

## Sample CR

```yaml
apiVersion: edgecloudlabs.edgecloudlabs.com/v1alpha1
kind: LLMBackend
metadata:
  name: gpt-oss-120b-eval
spec:
  model: gpt-oss-120b
  endpoint: http://<local_ip>:<port>/v1
  apiKeySecretRef:
    name: model-api-key
  triggerEvaluation: true
```

Create the secret referenced by `apiKeySecretRef`. For public providers (like OpenAI), use the following format:


# Set your OpenAI API key (replace with your actual key)
```bash
export OPENAI_API_KEY="sk-your-openai-api-key-here"
```

# Create secret with proper authorization header format in the gateway namespace
```bash
kubectl apply -f- <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: openai-secret
  namespace: agentgateway-system
type: Opaque
stringData:
  Authorization: $OPENAI_API_KEY
EOF
```

> [!IMPORTANT]
> The secret must be created in the `agentgateway-system` namespace so the gateway can access it, and it should use the `Authorization` key with the `Bearer ` prefix.

## Observability

- **Structured logging** via `logr`/`zap`
- **Prometheus metrics**: `llmbackend_reconcile_duration_seconds`, `llmbackend_reconcile_errors_total`, `llmbackend_phase_transitions_total`
- **Kubernetes Events** on phase transitions

## Uninstall

```bash
make undeploy
```
