# MODEL ROUTING ARCHITECTURE -- BASED ON SEMANTIC ROUTER

## 1. OBJECTIVE

Implement a **Model Router Service** that:

-   Receives requests exclusively from `EXT_Proc`
-   Implements the Agent Gateway streaming model
-   Reuses existing Semantic Router functionality
-   Makes routing decisions based only on:
    1.  Domain classification (signals)
    2.  Latency
    3.  Fixed configured cost

------------------------------------------------------------------------

## 2. ARCHITECTURAL CONSTRAINTS

-   The service does **NOT** expose public endpoints.
-   Traffic flow:

```{=html}
<!-- -->
```
    EXT_Proc → Model Router Service

-   Communication must be compatible with Agent Gateway streaming.
-   Phase 1 requirement: reuse existing Semantic Router components only.

------------------------------------------------------------------------

## 3. SYSTEM COMPONENTS

### INPUT: EXT_Proc

Sends routing request with:

-   User input
-   Optional metadata

### Router Responsibilities

-   Receive request
-   Evaluate signals
-   Execute decision engine
-   Return selected model
-   Maintain streaming compatibility

------------------------------------------------------------------------

## 4. REUSED FUNCTIONALITY FROM SEMANTIC ROUTER

Only the following three capabilities must be reused:

------------------------------------------------------------------------

### 4.1 SIGNALS -- DOMAIN CLASSIFICATION

-   Use Semantic Router signals module.
-   Use **M-Vert Pro** for domain evaluation.
-   Each model is associated with one or more domains.

Process:

1.  Analyze input\
2.  Determine domain\
3.  Compute domain score per model

Example intermediate output:

``` json
{
  "domain": "finance",
  "domain_scores": {
    "model_a": 0.82,
    "model_b": 0.34
  }
}
```

------------------------------------------------------------------------

### 4.2 DECISION ENGINE -- LATENCY

-   Each model maintains:
    -   `average_latency_ms`
-   Latency influences scoring.

Example concept:

    latency_score = 1 / average_latency_ms

------------------------------------------------------------------------

### 4.3 DECISION ENGINE -- FIXED COST

-   Cost is **NOT** dynamically calculated.
-   Cost is statically defined in configuration.
-   Lower cost improves score.

Example concept:

    cost_score = 1 / cost

Example configuration:

``` yaml
models:
  model_a:
    cost: 0.002
    domains: ["finance", "legal"]
    average_latency_ms: 120
  model_b:
    cost: 0.0008
    domains: ["general"]
    average_latency_ms: 85

weights:
  domain: 0.5
  latency: 0.3
  cost: 0.2
```

------------------------------------------------------------------------

## 5. DECISION ALGORITHM

Example formula:

    final_score =
        (w_domain * domain_score) +
        (w_latency * normalized_latency_score) +
        (w_cost * normalized_cost_score)

The model with the highest `final_score` is selected.

------------------------------------------------------------------------

## 6. COMPLETE FLOW

    EXT_Proc
       ↓
    Model Router Service
       ↓
    1) Signals → Detect domain (M-Vert Pro)
    2) Compute domain score per model
    3) Retrieve latency metrics
    4) Retrieve fixed cost from config
    5) Compute final score
    6) Select best model
       ↓
    Return model ID (streaming compatible)

------------------------------------------------------------------------

## 7. CODE STRUCTURE REQUIREMENTS

Claude must:

-   Reuse:
    -   signals module
    -   decision engine module
-   Modify decision engine to limit criteria to:
    -   domain
    -   latency
    -   fixed cost
-   Implement:
    -   configuration loader
    -   score calculator
    -   model selector
-   Expose interface compatible with:
    -   EXT_Proc
    -   Agent Gateway streaming

------------------------------------------------------------------------

## 8. EXCLUSIONS

Do **NOT** implement:

-   Dynamic cost evaluation
-   Health-based routing
-   Multi-hop routing
-   Reinforcement learning
-   Feedback loops
-   Fallback trees
-   A/B testing

Only:

-   Domain
-   Latency
-   Fixed cost

------------------------------------------------------------------------

## 9. SERVICE OUTPUT FORMAT

Minimal response example:

``` json
{
  "selected_model": "model_a",
  "scores": {
    "model_a": 0.91,
    "model_b": 0.67
  },
  "domain": "finance"
}
```

------------------------------------------------------------------------

## FINAL SUMMARY

This service is a reduced and specialized version of Semantic Router
limited to:

-   Signals (domain classification using M-Vert Pro)
-   Latency (historical metric)
-   Fixed configurable cost

It receives traffic only from `EXT_Proc` and responds within the Agent
Gateway streaming model.
