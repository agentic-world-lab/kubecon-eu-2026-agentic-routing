# Configuring the Semantic Router via Custom Resources

The semantic router can be configured dynamically at runtime using two Kubernetes Custom Resources from the `vllm.ai/v1alpha1` API group:

| CRD | Purpose |
|-----|---------|
| `IntelligentPool` | Defines the **model pool** (available models, pricing, default model) |
| `IntelligentRoute` | Defines the **routing logic** (signals, domains, decisions, plugins) |

When these CRs are created or updated, the Kubernetes controller detects the change and hot-reloads the router configuration without restarting the server.

---

## Prerequisites

The server must be started with `config_source: kubernetes` in its static config file:

```yaml
# Static config (config.yaml) - only classifier and embedding models
config_source: kubernetes

embedding_models:
  qwen3_model_path: "models/mom-embedding-pro"
  use_cpu: true

classifier:
  category_model:
    model_id: "models/mom-domain-classifier"
    threshold: 0.6
    use_cpu: true
    category_mapping_path: "models/mom-domain-classifier/category_mapping.json"
```

The server startup command also needs `--namespace` to specify which namespace to watch:

```bash
/app/extproc-server --config /app/config/config.yaml --namespace vllm-semantic-router-system
```

---

## Example 1: Basic domain-based routing

Route finance queries to a powerful model, everything else to a cheaper model.

### IntelligentPool

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentPool
metadata:
  name: model-pool
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

### IntelligentRoute

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: basic-routing
  namespace: vllm-semantic-router-system
spec:
  signals:
    domains:
      - name: finance
        description: "Financial and investment topics"
      - name: general
        description: "General knowledge"
  decisions:
    - name: finance_decision
      priority: 100
      description: "Route finance queries to the powerful model"
      signals:
        operator: AND
        conditions:
          - type: domain
            name: finance
      modelRefs:
        - model: gpt-4.1
          useReasoning: false
    - name: general_decision
      priority: 50
      description: "Route everything else to the cheaper model"
      signals:
        operator: AND
        conditions:
          - type: domain
            name: general
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

---

## Example 2: Multi-domain routing with system prompts

Route different domains to different models, each with a specialized system prompt.

### IntelligentPool

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentPool
metadata:
  name: multi-domain-pool
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
    - name: "gpt-5-mini"
      pricing:
        inputTokenPrice: 0.001
        outputTokenPrice: 0.004
```

### IntelligentRoute

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: multi-domain-routing
  namespace: vllm-semantic-router-system
spec:
  signals:
    domains:
      - name: finance
        description: "Financial and investment topics"
      - name: legal
        description: "Legal questions"
      - name: health
        description: "Health and medical topics"
      - name: math
        description: "Mathematics and quantitative reasoning"
      - name: computer_science
        description: "Computer science and programming"
      - name: other
        description: "General knowledge"
  decisions:
    - name: finance_decision
      priority: 100
      description: "Route finance to powerful model with expert prompt"
      signals:
        operator: AND
        conditions:
          - type: domain
            name: finance
      modelRefs:
        - model: gpt-4.1
          useReasoning: false
      plugins:
        - type: system_prompt
          configuration:
            enabled: true
            system_prompt: "You are a senior financial analyst. Provide data-driven insights on markets, investments, and corporate finance."
            mode: "replace"

    - name: legal_decision
      priority: 100
      description: "Route legal queries with disclaimer"
      signals:
        operator: AND
        conditions:
          - type: domain
            name: legal
      modelRefs:
        - model: gpt-4.1
          useReasoning: false
      plugins:
        - type: system_prompt
          configuration:
            enabled: true
            system_prompt: "You are a legal expert. Provide accurate legal information. Always state that your responses do not constitute legal advice."
            mode: "replace"

    - name: math_decision
      priority: 100
      description: "Route math queries with reasoning enabled"
      signals:
        operator: AND
        conditions:
          - type: domain
            name: math
      modelRefs:
        - model: gpt-5-mini
          useReasoning: true
          reasoningEffort: high
      plugins:
        - type: system_prompt
          configuration:
            enabled: true
            system_prompt: "You are a mathematics expert. Show step-by-step solutions."
            mode: "replace"

    - name: health_decision
      priority: 100
      signals:
        operator: AND
        conditions:
          - type: domain
            name: health
      modelRefs:
        - model: gpt-4.1
          useReasoning: false
      plugins:
        - type: system_prompt
          configuration:
            enabled: true
            system_prompt: "You are a health information expert. Always recommend consulting healthcare professionals."
            mode: "replace"

    - name: cs_decision
      priority: 80
      signals:
        operator: AND
        conditions:
          - type: domain
            name: computer_science
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false

    - name: fallback_decision
      priority: 10
      description: "Catch-all for unmatched queries"
      signals:
        operator: AND
        conditions:
          - type: domain
            name: other
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

---

## Example 3: Keyword + domain combined signals

Use keywords to catch specific intents and combine them with domain classification.

### IntelligentRoute

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: keyword-domain-routing
  namespace: vllm-semantic-router-system
spec:
  signals:
    keywords:
      - name: urgent_request
        operator: OR
        keywords: ["urgent", "critical", "emergency", "asap"]
        caseSensitive: false
      - name: code_review
        operator: AND
        keywords: ["review", "code"]
        caseSensitive: false
      - name: translation
        operator: OR
        keywords: ["translate", "translation", "traducir", "traduire"]
        caseSensitive: false
    domains:
      - name: finance
        description: "Financial topics"
      - name: computer_science
        description: "Programming and CS"
      - name: other
        description: "General"
  decisions:
    # Urgent finance queries go to the most powerful model
    - name: urgent_finance
      priority: 200
      description: "Urgent finance queries need the best model"
      signals:
        operator: AND
        conditions:
          - type: keyword
            name: urgent_request
          - type: domain
            name: finance
      modelRefs:
        - model: gpt-4.1
          useReasoning: true
          reasoningEffort: high

    # Code reviews go to a code-optimized model
    - name: code_review_decision
      priority: 150
      signals:
        operator: AND
        conditions:
          - type: keyword
            name: code_review
      modelRefs:
        - model: gpt-4.1
          useReasoning: false
      plugins:
        - type: system_prompt
          configuration:
            enabled: true
            system_prompt: "You are a senior software engineer conducting a code review. Focus on bugs, performance, and best practices."
            mode: "replace"

    # Regular finance
    - name: finance_decision
      priority: 100
      signals:
        operator: AND
        conditions:
          - type: domain
            name: finance
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false

    # Fallback
    - name: general_decision
      priority: 10
      signals:
        operator: OR
        conditions:
          - type: domain
            name: other
          - type: domain
            name: computer_science
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

---

## Example 4: Embedding-based semantic signals

Use embedding similarity for more nuanced intent detection beyond simple keyword matching.

### IntelligentRoute

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: embedding-routing
  namespace: vllm-semantic-router-system
spec:
  signals:
    embeddings:
      - name: investment_analysis
        threshold: 0.75
        aggregationMethod: max
        candidates:
          - "stock market analysis and portfolio management"
          - "investment strategy and risk assessment"
          - "financial modeling and valuation"
          - "market trends and economic forecasts"
      - name: medical_diagnosis
        threshold: 0.78
        aggregationMethod: mean
        candidates:
          - "patient symptoms and differential diagnosis"
          - "medical treatment options and drug interactions"
          - "clinical guidelines and evidence-based medicine"
      - name: legal_contract
        threshold: 0.72
        aggregationMethod: max
        candidates:
          - "contract terms and conditions review"
          - "legal liability and indemnification clauses"
          - "intellectual property rights and licensing"
    domains:
      - name: other
        description: "General knowledge"
  decisions:
    - name: investment_decision
      priority: 100
      description: "Investment analysis needs a reasoning-capable model"
      signals:
        operator: AND
        conditions:
          - type: embedding
            name: investment_analysis
      modelRefs:
        - model: gpt-4.1
          useReasoning: true
          reasoningEffort: medium

    - name: medical_decision
      priority: 100
      signals:
        operator: AND
        conditions:
          - type: embedding
            name: medical_diagnosis
      modelRefs:
        - model: gpt-4.1
          useReasoning: false
      plugins:
        - type: system_prompt
          configuration:
            enabled: true
            system_prompt: "You are a medical information assistant. Always recommend consulting a healthcare professional. Never provide diagnosis."
            mode: "replace"

    - name: legal_contract_decision
      priority: 100
      signals:
        operator: AND
        conditions:
          - type: embedding
            name: legal_contract
      modelRefs:
        - model: gpt-4.1
          useReasoning: false

    - name: general_decision
      priority: 10
      signals:
        operator: AND
        conditions:
          - type: domain
            name: other
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

---

## Example 5: Live model swap (update the pool)

Change the default model or add a new model without restarting the server. Just apply an updated `IntelligentPool`:

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentPool
metadata:
  name: model-pool
  namespace: vllm-semantic-router-system
spec:
  # Changed default from gpt-4.1-mini to gpt-5-mini
  defaultModel: "gpt-5-mini"
  models:
    - name: "gpt-4.1"
      pricing:
        inputTokenPrice: 0.002
        outputTokenPrice: 0.006
    - name: "gpt-4.1-mini"
      pricing:
        inputTokenPrice: 0.0004
        outputTokenPrice: 0.0016
    # New model added on the fly
    - name: "gpt-5-mini"
      pricing:
        inputTokenPrice: 0.001
        outputTokenPrice: 0.004
```

Then update the route to use the new model:

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: basic-routing
  namespace: vllm-semantic-router-system
spec:
  signals:
    domains:
      - name: finance
      - name: other
  decisions:
    - name: finance_decision
      priority: 100
      signals:
        operator: AND
        conditions:
          - type: domain
            name: finance
      modelRefs:
        # Swapped from gpt-4.1 to the new gpt-5-mini
        - model: gpt-5-mini
          useReasoning: false
    - name: general_decision
      priority: 50
      signals:
        operator: AND
        conditions:
          - type: domain
            name: other
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

Apply:
```bash
kubectl apply -f intelligentpool.yaml
kubectl apply -f intelligentroute.yaml
```

The router picks up the changes within seconds, no restart needed.

---

## Example 6: OR conditions (match any signal)

Route to a powerful model when the query matches **any** of several domains:

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRoute
metadata:
  name: or-routing
  namespace: vllm-semantic-router-system
spec:
  signals:
    domains:
      - name: math
      - name: physics
      - name: chemistry
      - name: engineering
      - name: other
  decisions:
    - name: stem_decision
      priority: 100
      description: "All STEM domains go to a reasoning model"
      signals:
        operator: OR
        conditions:
          - type: domain
            name: math
          - type: domain
            name: physics
          - type: domain
            name: chemistry
          - type: domain
            name: engineering
      modelRefs:
        - model: gpt-4.1
          useReasoning: true
          reasoningEffort: high
    - name: general_decision
      priority: 10
      signals:
        operator: AND
        conditions:
          - type: domain
            name: other
      modelRefs:
        - model: gpt-4.1-mini
          useReasoning: false
```

---

## How it works internally

```
┌──────────────────────────────────┐
│  Kubernetes API Server           │
│                                  │
│  IntelligentPool CR  ──┐         │
│  IntelligentRoute CR ──┤         │
│                        ▼         │
│              controller-runtime  │
│              Reconciler watches  │
│              both CRDs           │
└──────────────┬───────────────────┘
               │ On any change:
               │ 1. CRDConverter translates CRs → RouterConfig
               │ 2. Merges with static config (classifiers, embeddings)
               │ 3. config.Replace(newConfig)
               │ 4. Router hot-swaps via atomic pointer
               ▼
┌──────────────────────────────────┐
│  Semantic Router (ExtProc gRPC)  │
│  Port 50051                      │
│                                  │
│  New requests immediately use    │
│  the updated routing config      │
└──────────────────────────────────┘
```

The static config file provides:
- Classifier model paths (BERT models, embedding models)
- Observability settings (metrics, tracing)
- `config_source: kubernetes` flag

The CRDs provide:
- Available models and their pricing (`IntelligentPool`)
- Routing signals, decisions, and plugins (`IntelligentRoute`)

This separation means ML model infrastructure is stable (static), while routing logic can be changed on the fly (dynamic via CRDs).
