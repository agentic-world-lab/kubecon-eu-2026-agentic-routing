# Prompt for Claude Code

## Objective: Implement the Backend Evaluation Kubernetes Controller

You are given a design document:

`Design.md`

Your task is to implement a production-ready Kubernetes Operator based
on this document.

------------------------------------------------------------------------

# Context

The design document defines:

-   A `LLMBackend` Custom Resource Definition (CRD)
-   A phase-driven reconciliation model
-   Integration with:
    -   AgentGatewayBackend CR
    -   Kubernetes Job (MMLU evaluation)
    -   External Agent (for semantic router updates)
-   Status-driven state transitions
-   Production-level requirements (RBAC, idempotency, observability,
    leader election)

You must strictly follow the architecture and constraints defined in the
design document.

------------------------------------------------------------------------

# Instructions

## 1. Bootstrap the Project

Kubebuilder is already installed on the system.

Use Kubebuilder to:

1.  Initialize the project
2.  Create the API and Controller for `LLMBackend`
3.  Generate CRD manifests
4.  Configure RBAC markers

Use:

``` bash
kubebuilder init --domain edgecloudlabs.com --repo github.com/felipevicens/backend-evaluation-operator

kubebuilder create api   --group edgecloudlabs   --version v1alpha1   --kind LLMBackend
```

Enable controller creation when prompted.

------------------------------------------------------------------------

## 2. Implement the CRD Schema

Define:

### Spec Fields

-   model (string)
-   endpoint (string)
-   apiKeySecretRef (Secret reference struct)
-   dataset (string)
-   triggerEvaluation (bool)
-   routingPolicy (accuracyWeight, speedWeight, costWeight)

### Status Fields

-   phase (enum)
-   backendName
-   jobName
-   results (structured object)
-   conditions (Kubernetes conditions)

Add OpenAPI validation where appropriate.

------------------------------------------------------------------------

## 3. Implement the Reconcile Logic

Follow the phase-based state machine described in the design document.

Phases: - "" - BackendCreated - Evaluating - Evaluated - Failed

Reconcile must:

-   Be idempotent
-   Never block
-   Use OwnerReferences
-   Requeue when necessary
-   Update status properly

Implement helper methods:

-   ensureBackend()
-   ensureEvaluationJob()
-   checkJobStatus()
-   collectResults()

------------------------------------------------------------------------

## 4. Resource Creation Rules

### AgentGatewayBackend

-   Create if not exists
-   Use deterministic name
-   Set OwnerReference

### Evaluation Job

-   Create Kubernetes Job
-   Inject model, endpoint, secret
-   Mount PVC for results
-   Deterministic naming
-   Set OwnerReference

### Result Collection

-   Read results.json from mounted volume
-   Parse JSON safely
-   Validate structure
-   Store in Status.Results

Never parse pod logs.

------------------------------------------------------------------------

## 5. RBAC

Add proper RBAC markers for:

-   LLMBackend
-   Jobs
-   Pods
-   AgentGatewayBackend

Ensure status subresource updates are allowed.

------------------------------------------------------------------------

## 6. Observability

Add:

-   Structured logging
-   Error handling
-   Requeue logic with backoff
-   Metrics hooks (if feasible)

------------------------------------------------------------------------

## 7. Production Requirements

-   Enable leader election
-   Use finalizers if cleanup needed
-   Ensure reconciliation is safe on restart
-   Avoid infinite loops
-   Ensure idempotency at every step

------------------------------------------------------------------------

## 8. Deliverables

Claude Code must produce:

1.  Full Kubebuilder project structure
2.  Implemented API types
3.  Implemented controller logic
4.  Updated RBAC
5.  Sample CR YAML
6.  Instructions to build and deploy

------------------------------------------------------------------------

# Constraints

-   Follow Kubernetes controller-runtime best practices
-   No blocking loops
-   No heavy logic inside reconcile
-   No direct router config generation (Agent handles that)
-   Clean separation of infra and intelligence layers

------------------------------------------------------------------------

# Goal

Produce a clean, production-grade Kubernetes Operator that implements
the lifecycle described in `Design.md`.

------------------------------------------------------------------------

END OF PROMPT
