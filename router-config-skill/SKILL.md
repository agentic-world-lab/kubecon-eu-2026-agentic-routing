---
name: router-config-skill
description: Validate and generate IntelligentPool and IntelligentRoute CRs for the semantic router
---

# Router Configuration Skill

Use this skill whenever you need to create, update, or validate `IntelligentPool` or `IntelligentRoute` custom resources for the semantic router.

## IMPORTANT — Validation before apply

**You MUST validate every CR before applying it to the cluster.** Always follow this workflow:

1. Generate the CR YAML
2. Call `scripts/validate_cr.py` passing the YAML as argument
3. If the script outputs `VALID`, proceed to apply with `k8s_apply_manifest`
4. If the script outputs `INVALID`, fix the issues listed and re-validate

Never skip validation. Never apply a CR that has not passed validation.

## Instructions

### Validating a CR

Call `scripts/validate_cr.py` with the YAML content as argument:

```
python3 scripts/validate_cr.py '<yaml content here>'
```

The script validates structural correctness and outputs either:
- `VALID: <summary>` — the CR is safe to apply
- `INVALID: <list of errors>` — the CR has issues that must be fixed

### Generating an IntelligentPool

When asked to add or update models, generate a CR like this:

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentPool
metadata:
  name: router-pool
  namespace: vllm-semantic-router-system
spec:
  defaultModel: "gpt-4.1-mini"
  models:
    - name: "gpt-4.1"
      pricing:
        inputTokenPrice: 0.002
        outputTokenPrice: 0.006
    - name: "gpt-4.1-mini"
      pricing:
        inputTokenPrice: 0.0004
        outputTokenPrice: 0.0016
```

Rules:
- `defaultModel` must be the name of one of the models in the list
- Every model must have a non-empty `name`
- Pricing values must be non-negative numbers

### Generating an IntelligentRoute

When asked to configure routing, generate a CR like this:

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: router-route
  namespace: vllm-semantic-router-system
spec:
  signals:
    domains:
      - name: math
        description: "Mathematics and quantitative reasoning"
      - name: general
        description: "General knowledge"
  decisions:
    - name: math_decision
      priority: 100
      signals:
        operator: AND
        conditions:
          - type: domain
            name: math
      modelRefs:
        - model: gpt-4.1
          useReasoning: true
    - name: general_decision
      priority: 50
      signals:
        operator: AND
        conditions:
          - type: domain
            name: general
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

Rules:
- Every domain in a decision condition must exist in `signals.domains`
- Decision `priority` must be non-negative (higher = higher priority)
- `operator` must be `AND` or `OR`
- Every decision must have at least one `modelRef`
- Embedding signal thresholds must be between 0 and 1

### Workflow for adding a new backend model

When the bridge tells you a new AgentgatewayBackend was added:

1. GET the current IntelligentPool `router-pool` in namespace `vllm-semantic-router-system`
2. If it doesn't exist, create it. If it exists, add the new model to it.
3. GET the current IntelligentRoute `router-route` in namespace `vllm-semantic-router-system`
4. If it doesn't exist, create it. If it exists, add domain signals and decisions for the new model.
5. Validate BOTH CRs with `scripts/validate_cr.py` before applying
6. Apply both using `k8s_apply_manifest`

Never remove existing models or decisions — only add or update.

## Example

User: "Add model gpt-5-mini for domains math and physics with cost 0.001"

Agent:
1. GET current IntelligentPool
2. Add gpt-5-mini to the models list
3. Validate with `scripts/validate_cr.py`
4. Apply IntelligentPool
5. GET current IntelligentRoute
6. Add domain signals math, physics (if not already present)
7. Add decisions routing math and physics to gpt-5-mini
8. Validate with `scripts/validate_cr.py`
9. Apply IntelligentRoute
