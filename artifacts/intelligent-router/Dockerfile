# Multi-stage Docker build for model-router.
# Supports AMD64 and ARM64 out-of-the-box: pure-Go binary, no CGO, no Rust.
#
# Build for the current host:
#   docker build -t model-router .
#
# Cross-compile for a specific platform:
#   docker buildx build --platform linux/arm64 -t model-router:arm64 .
#   docker buildx build --platform linux/amd64 -t model-router:amd64 .

FROM --platform=$BUILDPLATFORM golang:alpine AS builder

RUN apk --no-cache add make ca-certificates

WORKDIR /app
COPY go.mod go.sum ./

# The replace directive for candle-binding points to ../candle-binding, which is
# outside this build context. Since we compile with CGO_ENABLED=0, mlclassifier.go
# is excluded via //go:build cgo and candle-binding is not needed. Strip it so that
# `go mod download` does not fail trying to resolve a path that does not exist here.
RUN sed -i '/candle-binding/d' go.mod && go mod download

COPY . .

ARG TARGETOS TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 \
    go build -ldflags="-w -s" -o _output/router .

# ── Runtime image ─────────────────────────────────────────────────────────────
# scratch avoids any platform-specific base-image pull (binary is fully static).
FROM scratch

# CA certificates for outbound TLS (metrics, health endpoints).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app
COPY --from=builder /app/_output/router /app/router
COPY config.yaml /app/config.yaml

# Non-root user (matches convention in example-of-extPRoc-server)
USER 10101

# gRPC ExtProc port | Prometheus metrics port
EXPOSE 18080 9091

CMD ["/app/router", "--config=/app/config.yaml"]
