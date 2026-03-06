# Backend Evaluation Kubernetes Controller

## Design Document

Version: 1.0\
Target: Production-ready Kubernetes Operator\
Language: Go\
Framework: Kubebuilder / controller-runtime

------------------------------------------------------------------------

# 1. Overview

This document describes the design of a Kubernetes controller
responsible for:

1.  Creating an AgentGateway backend from a backend configuration
2.  Triggering an MMLU evaluation Job
3.  Collecting evaluation results
4.  Updating CR status with structured results
5.  Allowing an external Agent to generate and update Semantic Router
    configuration

The controller follows Kubernetes reconciliation principles and
implements a deterministic state machine.

------------------------------------------------------------------------

# 2. High-Level Architecture

LLMBackend CR ↓ Controller Reconcile Loop ├── Ensure
AgentGatewayBackend exists ├── Ensure MMLU Job exists ├── Wait for Job
completion ├── Collect evaluation results ├── Update status ↓ External
Agent (watches CR) ├── Generates routing config └── Updates
IntelligentRouter CR

------------------------------------------------------------------------

# 3. Design Goals

-   Fully declarative
-   Idempotent reconciliation
-   Status-driven state machine
-   No blocking calls
-   Separation of infra and intelligence
-   Production-grade observability
-   Clear ownership relationships
-   Safe retries

------------------------------------------------------------------------

# 4. Custom Resource Definitions

## 4.1 LLMBackend CRD

### API Group

edgecloudlabs.com

### Version

v1alpha1

### Scope

Namespaced

------------------------------------------------------------------------

## 4.2 LLMBackend Spec

``` yaml
apiVersion: edgecloudlabs.com/v1alpha1
kind: LLMBackend
metadata:
  name: gpt-oss-120b-eval
spec:
  model: gpt-oss-120b
  endpoint: http://10.95.161.240:8000/v1
  apiKeySecretRef:
    name: model-api-key
```

------------------------------------------------------------------------

## 4.3 LLMBackend Status

``` yaml
status:
  phase: Pending | BackendCreated | Evaluating | Evaluated | Failed
  backendName: gpt-oss-120b
  jobName: mmlu-pro-job-abc123
  results:
    overallAccuracy: "0.7500"
    tokensPerSecond: "197.60"
    avgResponseTime: "2.3800"
    categoryAccuracy:
      business: "0.6000"
      law: "0.6000"
      psychology: "0.4000"
      biology: "1.0000"
  conditions:

    - type: Ready
      status: "True"
      lastTransitionTime: ...
```

------------------------------------------------------------------------

# 5. Reconciliation Model

The controller implements a phase-based state machine.

## Phases

  Phase            Meaning
  ---------------- -----------------------------
  "" (empty)       Initial state
  BackendCreated   AgentGatewayBackend created
  Evaluating       Job running
  Evaluated        Results collected
  Failed           Terminal failure

------------------------------------------------------------------------

# 6. Reconcile Loop Logic

``` go
func (r *LLMBackendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

    var eval LLMBackend
    if err := r.Get(ctx, req.NamespacedName, &eval); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    switch eval.Status.Phase {

    case "":
        return r.ensureBackend(ctx, &eval)

    case "BackendCreated":
        return r.ensureEvaluationJob(ctx, &eval)

    case "Evaluating":
        return r.checkJobStatus(ctx, &eval)

    case "Evaluated":
        return ctrl.Result{}, nil
    }

    return ctrl.Result{}, nil
}
```

------------------------------------------------------------------------

# 7. Phase Implementations

## 7.1 Ensure AgentGatewayBackend

-   Create AgentGatewayBackend CR
-   Set OwnerReference to LLMBackend
-   Update status.phase = BackendCreated
-   Must be idempotent

------------------------------------------------------------------------

## 7.2 Ensure MMLU Job

-   Create Kubernetes Job
-   Inject model, endpoint, secret
-   Mount PVC for results
-   Set OwnerReference
-   Update status.phase = Evaluating

------------------------------------------------------------------------

## 7.3 Check Job Completion

-   Fetch Job status
-   If active → requeue
-   If failed → update phase Failed
-   If complete:
    -   Retrieve results.json
    -   Parse JSON
    -   Store structured data in status.results
    -   Update phase = Evaluated

Never parse logs in production.

------------------------------------------------------------------------

# 8. Result Handling

MMLU Job must write:

/shared/results.json

Example:

``` json
{
  "model": "gpt-oss-120b",
  "overall_accuracy": 0.75,
  "avg_response_time": 2.38,
  "tok_s": 197.6
}
```

Controller must: - Read file - Validate structure - Convert to typed
struct - Update status

------------------------------------------------------------------------

# 9. Agent Integration Pattern

Controller does NOT generate router config.

Instead:

1.  Agent watches LLMBackend CR
2.  When status.phase == Evaluated
3.  Agent computes routing logic
4.  Agent updates IntelligentRouter CR

Separation of concerns: - Controller = Infrastructure orchestration -
Agent = Decision intelligence

------------------------------------------------------------------------

# 10. Owner References

All created resources must include:

``` go
controllerutil.SetControllerReference(&eval, childResource, r.Scheme)
```

Ensures automatic garbage collection.

------------------------------------------------------------------------

# 11. RBAC Requirements

Controller must have permissions for:

-   llmbackends
-   llmbackends/status
-   jobs
-   pods
-   agentgatewaybackends

Kubebuilder markers example:

``` go
// +kubebuilder:rbac:groups=edgecloudlabs.com,resources=llmbackends,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch
```

------------------------------------------------------------------------

# 12. Idempotency Rules

All reconcile steps must:

-   Check if resource exists before creating
-   Compare desired vs current state
-   Avoid duplicate creation
-   Use deterministic names

Example job name:

mmlu-`<evaluation-name>`{=html}

------------------------------------------------------------------------

# 13. Failure Handling

If Job fails:

-   Set phase = Failed
-   Record error message
-   Avoid infinite retries

Example:

``` go
return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
```

------------------------------------------------------------------------

# 14. Observability

Controller must include:

-   Structured logging
-   Prometheus metrics
-   Reconcile duration histogram
-   Error counters
-   Kubernetes Events for phase transitions

------------------------------------------------------------------------

# 15. Deployment

Build and deploy using:

make docker-build\
make docker-push\
make deploy

Controller runs as:

-   Deployment
-   1 replica
-   Leader election enabled

------------------------------------------------------------------------

# 16. Production Considerations

## Leader Election

Enable in manager options.

## Finalizers

If cleanup required: - Remove external resources - Then remove finalizer

## Validation

Add OpenAPI validation in CRD.

## Versioning Strategy

-   v1alpha1
-   v1beta1
-   v1

------------------------------------------------------------------------

# 17. Non-Goals

This controller does NOT:

-   Perform heavy ML computation
-   Generate router configs
-   Run evaluation logic itself

------------------------------------------------------------------------

# 18. Future Enhancements

-   Historical result persistence
-   Drift detection
-   Scheduled evaluations
-   Multi-model comparison CRD
-   Cost-aware scoring inside status

------------------------------------------------------------------------

# 19. Summary

This controller:

-   Orchestrates backend lifecycle
-   Triggers evaluation
-   Collects results
-   Updates CR status
-   Enables external agent-based routing decisions

It follows Kubernetes best practices: - Declarative - Idempotent -
Phase-driven - Observable - Extensible

------------------------------------------------------------------------

END OF DOCUMENT
