# Makefile for the intelligent-router ExtProc server.
#
# Platform detection:
#   Apple Silicon (arm64/aarch64) → linux/arm64
#   Intel/AMD (x86_64)            → linux/amd64
#
# Quick start on Apple Silicon:
#   make all      # test + build + deploy
#
# The deployed image always includes M-Vert Pro (BERT classifier via Rust/candle).
# BERT is optional at runtime — enable/disable with ml_classifier.enabled in config.
#
# Iterate on routing config without rebuilding the image:
#   1. Edit manifests/pool-configmap.yaml or manifests/route-configmap.yaml
#   2. make reload-config
#   3. Wait 5 s for the router to pick up the change

IMAGE       := intelligent-router:latest
IMAGE_ML    := intelligent-router:ml
CLUSTER     := kubeconeu2026
NAMESPACE   := intelligent-router-system

# Detect host architecture and map to Docker platform.
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_M),arm64)
  PLATFORM := linux/arm64
else ifeq ($(UNAME_M),aarch64)
  PLATFORM := linux/arm64
else
  PLATFORM := linux/amd64
endif

# Registry — single image, always the ML build.
REGISTRY_IMAGE ?= antonioberben/intelligent-router
REGISTRY_TAG   ?= latest

.PHONY: all test build build-ml push deploy reload-config clean logs status

## Full pipeline: test → build-ml → deploy.
all: test build-ml deploy

# ── Go tests ─────────────────────────────────────────────────────────────────

## Run all Go unit tests (pure-Go, no CGO required).
test:
	go test ./... -v -count=1

# ── Docker ───────────────────────────────────────────────────────────────────

## Build the pure-Go image locally (fast, no Rust, no BERT — for quick iteration).
## BERT will be a no-op stub in this image.
build:
	@echo ">>> Building $(IMAGE) for platform $(PLATFORM) (pure-Go, no BERT)"
	docker buildx build \
		--platform $(PLATFORM) \
		--load \
		-t $(IMAGE) .

## Build the full image with M-Vert Pro (Rust/candle BERT classifier).
## Build context is the intelligent-router/ parent directory.
## This is the image that gets pushed to the registry.
build-ml:
	@echo ">>> Building $(IMAGE_ML) for platform $(PLATFORM) (with M-Vert Pro)"
	docker buildx build \
		--platform $(PLATFORM) \
		--load \
		-f Dockerfile.ml \
		-t $(IMAGE_ML) \
		../

## Push the ML image to the registry as the canonical latest tag (amd64 + arm64).
## Requires an active buildx builder: docker buildx create --use --name multiarch
push:
	@echo ">>> Pushing $(REGISTRY_IMAGE):$(REGISTRY_TAG) (with M-Vert Pro, linux/amd64+arm64)"
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--push \
		-f Dockerfile.ml \
		-t $(REGISTRY_IMAGE):$(REGISTRY_TAG) \
		../

# ── Kubernetes deployment ─────────────────────────────────────────────────────────────


## Deploy the router to Kubernetes (applies manifests in dependency order).
deploy:
	kubectl apply -f manifests/namespace.yaml
	kubectl apply -f manifests/crd-intelligent-router-config.yaml
	kubectl apply -f manifests/router-config-configmap.yaml
	kubectl apply -f manifests/service.yaml
	kubectl apply -f manifests/statefulset.yaml
	@echo ""
	@echo ">>> Waiting for StatefulSet rollout..."
	kubectl rollout status statefulset/intelligent-router -n $(NAMESPACE) --timeout=120s
	@echo ""
	@echo ">>> Router is ready."
	@echo "    gRPC ExtProc : localhost:18080"
	@echo "    Metrics      : http://localhost:9091/metrics"

## Re-apply only the ConfigMap so the router hot-reloads the routing config.
## The pod is NOT restarted; the router picks up the change within 5 seconds.
reload-config:
	kubectl apply -f manifests/router-config-configmap.yaml
	@echo ">>> ConfigMap updated. Router will reload within 5 s."

# ── Housekeeping ─────────────────────────────────────────────────────────────

## Tail logs from the running router pod.
logs:
	kubectl logs -n $(NAMESPACE) -l app=intelligent-router -f --tail=100

## Show pod and service status.
status:
	kubectl get pods,svc -n $(NAMESPACE) -o wide
