# Dockerfile.ml — model-router with M-Vert Pro (BERT/candle) domain classifier.
#
# Build context: the intelligent-router/ directory itself.
#
# The BERT model is NOT embedded in this image.
# It is downloaded at pod startup by an initContainer (see manifests/statefulset.yaml)
# and stored on a PersistentVolume, so restarts skip the download.
#
# Build for ARM64:  make build-ml
# Push to registry: make push

# ── Stage 1: Build the Rust candle-binding library (CPU-only) ─────────────────
FROM rust:bookworm AS rust-builder

RUN apt-get update && apt-get install -y \
    build-essential pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the full candle-binding directory and build the shared library.
# CPU-only build: --no-default-features disables CUDA/MKL/Accelerate.
COPY candle-binding/ candle-binding/
RUN cd candle-binding && \
    cargo build --release --no-default-features && \
    echo "Built:" && ls -lh target/release/libcandle_semantic_router.so

# ── Stage 2: Build the Go binary with CGO ─────────────────────────────────────
FROM golang:bookworm AS go-builder

WORKDIR /app/intelligent-router

COPY candle-binding/go.mod candle-binding/semantic-router.go ./candle-binding/
COPY --from=rust-builder /app/candle-binding/target/release/libcandle_semantic_router.so \
    ./candle-binding/target/release/libcandle_semantic_router.so

COPY go.mod go.sum ./
RUN GOFLAGS="-mod=mod" go mod download

COPY . .

ARG TARGETOS=linux TARGETARCH
RUN CGO_ENABLED=1 \
    GOOS=${TARGETOS} \
    GOFLAGS="-mod=mod" \
    go build \
    -ldflags="-w -s -extldflags=-Wl,-rpath,/app/lib" \
    -o /app/router .

# ── Stage 3: Runtime image ────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=go-builder /app/router /app/router
COPY --from=rust-builder \
    /app/candle-binding/target/release/libcandle_semantic_router.so \
    /app/lib/libcandle_semantic_router.so
# /app/models is populated at runtime by the initContainer (see manifests/statefulset.yaml).
RUN mkdir -p /app/models/domain-classifier

ENV LD_LIBRARY_PATH=/app/lib

USER 10101

EXPOSE 18080 9091

CMD ["/app/router"]
