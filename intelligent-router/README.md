# intelligent-router — ExtProc Server for Intelligent LLM Routing

A lightweight, pure-Go [AgentGateway ExtProc](https://agentgateway.dev/docs/standalone/main/configuration/traffic-management/extproc/) server that classifies the domain of every LLM request (finance, health, legal, technology, science) and routes it to the best-matching model. AgentGateway sets the `x-vsr-selected-model` header based on the router's decision; the HTTPRoute layer then selects the correct `AgentgatewayBackend`.

---

## How it works

```
Client (OpenAI API call)
      │
      ▼
┌─────────────────┐    gRPC ExtProc      ┌──────────────────────────────────────┐
│   AgentGateway  │ ───────────────────► │          intelligent-router           │
│                 │ ◄─────────────────── │                                      │
└─────────────────┘   routing headers   │  1. Extract API key (Auth header)    │
      │                                  │  2. Classify domain (keywords)       │
      │  x-vsr-selected-model: gpt-4.1  │  3. Score models (quality/cost/lat.) │
      ▼                                  │  4. Set x-vsr-selected-model header  │
┌─────────────────┐                      └──────────────────────────────────────┘
│  HTTPRoute      │
│  matches header │──► AgentgatewayBackend (gpt-4-1 / gpt-5-mini / gpt-4-1-mini)
└─────────────────┘
```

### Output headers set on every request

| Header | Value |
|--------|-------|
| `x-vsr-selected-model` | Model name chosen by the router (e.g. `gpt-4.1`) |
| `x-vsr-selected-domain` | Detected domain (`finance`, `technology`, `unknown`, …) |
| `x-vsr-model-scores` | JSON array of all model scores (for observability) |

### Routing configuration — `IntelligentRouterConfig` CR

The router is configured via a single `IntelligentRouterConfig` CR mounted from a ConfigMap. It supports hot-reload: edit the ConfigMap and the router picks up the change within 5 seconds, no pod restart.

```yaml
apiVersion: vllm.ai/v1alpha1
kind: IntelligentRouterConfig
metadata:
  name: config-router
  namespace: intelligent-router-system
spec:
  pool:
    defaultModel: "gpt-4.1-mini"
    models:
      - name: "gpt-4.1"
        initialAverageLatencyMs: 120
        qualityScores:
          finance: 0.9
          health: 0.8
          legal: 0.7
        costScore: 0.8

      - name: "gpt-5-mini"
        initialAverageLatencyMs: 100
        qualityScores:
          technology: 0.9
          science: 0.9
        costScore: 0.5

      - name: "gpt-4.1-mini"
        initialAverageLatencyMs: 85
        qualityScores:
          technology: 0.9
          general: 0.8
        costScore: 0.3

  weights:
    quality: 0.6    # primary: domain affinity
    latency: 0.2    # prefer lower-latency models
    cost: 0.2       # prefer cheaper models

  keywordRules:
    - name: finance
      operator: OR
      keywords: [stock, investment, portfolio, dividend, trading, bond, equity, fund]
    - name: science
      operator: OR
      keywords: [physics, chemistry, biology, quantum, experiment, molecule]
    - name: technology
      operator: OR
      keywords: [software, code, programming, kubernetes, docker, algorithm]
```

---

## Building

### Prerequisites

- Go 1.24+
- Docker with Buildx enabled (included in Docker Desktop)

### Run tests

```bash
cd intelligent-router/
go test ./... -v
```

### Build the pure-Go image (fast iteration, no ML classifier)

Platform is auto-detected from `uname -m`:

```bash
make build
# Apple Silicon → linux/arm64
# Intel/AMD     → linux/amd64
```

Or specify the platform explicitly:

```bash
docker buildx build --platform linux/amd64 --load -t intelligent-router:latest .
docker buildx build --platform linux/arm64 --load -t intelligent-router:latest .
```

### Build the ML image (M-Vert Pro BERT classifier)

The ML image adds a Rust/candle compile stage before the Go build.
Build context is the **parent directory** (the repo root), not `intelligent-router/`:

```bash
# From inside intelligent-router/
make build-ml
# Equivalent to:
docker buildx build \
  --platform linux/arm64 \
  --load \
  -f Dockerfile.ml \
  -t intelligent-router:ml \
  ../
```

> The Rust compilation takes several minutes on the first build. Subsequent builds use the Docker layer cache.

---

## Pushing to a registry

### Push single-arch image

```bash
export REGISTRY_IMAGE=antonioberben/intelligent-router
export REGISTRY_TAG=latest
```

```bash
# Pure-Go (no ML)
docker buildx build \
  --platform linux/amd64 \
  --push \
  -t $REGISTRY_IMAGE:$REGISTRY_TAG .
```

### Push multi-arch image (amd64 + arm64)

Requires an active multi-arch builder:

```bash
docker buildx create --use --name multiarch
```

```bash
# Pure-Go
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --push \
  -t $REGISTRY_IMAGE:$REGISTRY_TAG .

# With M-Vert Pro ML classifier (build context = repo root)
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --push \
  -f Dockerfile.ml \
  -t $REGISTRY_IMAGE:$REGISTRY_TAG \
  ../
```

Or use the Makefile shortcut:

```bash
make push
# Pushes linux/arm64 ML image to antonioberben/intelligent-router:latest
```

### Override registry/tag at build time

```bash
make push REGISTRY_IMAGE=myrepo/intelligent-router REGISTRY_TAG=v1.2.3
```

---

## Configuration reference

The router is started with `--cr` pointing to a mounted `IntelligentRouterConfig` YAML:

```
router --cr /app/config/config.yaml --grpcport :18080 --metricsport :9091
```

### `spec.pool`

| Field | Description |
|-------|-------------|
| `defaultModel` | Fallback model when no scoring match is found |
| `models[].name` | Model identifier — written to `x-vsr-selected-model` header |
| `models[].initialAverageLatencyMs` | Seed latency; updated at runtime from observed requests |
| `models[].qualityScores` | Map of `domain → score [0,1]` (higher = better for that domain) |
| `models[].costScore` | Normalised cost `[0,1]` — higher = more expensive |

### `spec.weights`

| Field | Description |
|-------|-------------|
| `quality` | Weight for domain quality score (positive contributor) |
| `latency` | Weight for latency penalty (higher latency → lower score) |
| `cost` | Weight for cost penalty (higher cost → lower score) |

### `spec.keywordRules[]`

| Field | Description |
|-------|-------------|
| `name` | Domain name produced when this rule matches |
| `operator` | `OR` (any keyword), `AND` (all keywords), `NOR` (no keyword) |
| `keywords` | List of terms — matched with word-boundary prefix (`\bterm`) |
| `caseSensitive` | Default: `false` |

### `spec.tokenBudget`

| Field | Description |
|-------|-------------|
| `enabled` | Enable per-API-key token budget pressure routing |
| `threshold` | Token count at which cheaper models start getting a score boost |
| `quota` | Token count at which maximum cost pressure is applied |
| `windowSeconds` | Sliding window duration in seconds |

---

## Prometheus metrics

Exposed on `:9091/metrics`:

| Metric | Labels | Description |
|--------|--------|-------------|
| `intelligent_router_decisions_total` | `selected_model`, `domain` | Routing decisions made |
| `intelligent_router_score` | `model` | Last computed final score per model |
| `intelligent_router_tokens_used` | `api_key` | Current tokens in window per API key |
| `intelligent_router_budget_pressure` | `api_key` | Current budget pressure `[0,1]` |
| `intelligent_router_latency_ms` | `model` | Current tracked average latency in ms |

---

## File reference

| File | Description |
|------|-------------|
| [main.go](main.go) | gRPC ExtProc server, routing orchestration, config hot-reload, Prometheus metrics |
| [config.go](config.go) | `IntelligentRouterConfig` CR types, YAML loaders, converter logic |
| [classifier.go](classifier.go) | Pure-Go keyword classifier (AND/OR/NOR, word-boundary matching) |
| [scorer.go](scorer.go) | Weighted scoring formula (quality + latency + cost + token budget) |
| [tokenstore.go](tokenstore.go) | Per-API-key sliding-window token store and budget pressure |
| [Dockerfile](Dockerfile) | Multi-arch pure-Go build (`linux/amd64` + `linux/arm64`) |
| [Dockerfile.ml](Dockerfile.ml) | ML build with M-Vert Pro BERT classifier (Rust/candle) |
| [Makefile](Makefile) | Build, push, kind-cluster, deploy automation |
| [manifests/](manifests/) | Kubernetes manifests for standalone deployment |
