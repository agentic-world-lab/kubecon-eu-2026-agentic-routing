# Installing Semantic Router

```bash
export HUGGINGFACE_HUB_TOKEN="<add your hf token here>"
```

```bash
kubectl create secret generic hf-token-secret \
  --namespace vllm-semantic-router-system \
  --from-literal=token=${HUGGINGFACE_HUB_TOKEN}
```

1. Install the semantic router
```bash
helm install semantic-router oci://ghcr.io/vllm-project/charts/semantic-router:v0.0.0-latest --namespace vllm-semantic-router-system   --create-namespace   -f https://raw.githubusercontent.com/vllm-project/semantic-router/refs/heads/main/deploy/kubernetes/ai-gateway/semantic-router-values/values.yaml --set persistence.storageClassName=longhorn   --set image.repository=fjvicens/vllm-sr-extproc --set config.classifier.pii_model.pii_mapping_path="models/pii_classifier_modernbert-base_presidio_token_model/pii_type_mapping.json"
```

Output:
```bash
Pulled: ghcr.io/vllm-project/charts/semantic-router:v0.0.0-latest
Digest: sha256:81eb6851f271a4d68f899bd643e9e5c082166a9e4649059acddf331b54bdf663
NAME: semantic-router
LAST DEPLOYED: Fri Jan 23 11:07:14 2026
NAMESPACE: vllm-semantic-router-system
STATUS: deployed
REVISION: 1
DESCRIPTION: Install complete
TEST SUITE: None
NOTES:
1. Get the application URL by running these commands:
  export POD_NAME=$(kubectl get pods --namespace vllm-semantic-router-system -l "app.kubernetes.io/name=semantic-router,app.kubernetes.io/instance=semantic-router" -o jsonpath="{.items[0].metadata.name}")
  export CONTAINER_PORT=$(kubectl get pod --namespace vllm-semantic-router-system $POD_NAME -o jsonpath="{.spec.containers[0].ports[0].containerPort}")
  echo "Visit http://127.0.0.1:8080 to use your application"
  kubectl --namespace vllm-semantic-router-system port-forward $POD_NAME 8080:$CONTAINER_PORT

2. Test the Classification API:
  # Health check
  curl http://localhost:8080/health

  # Intent classification
  curl -X POST http://localhost:8080/api/v1/classify/intent \
    -H "Content-Type: application/json" \
    -d '{"text": "What is machine learning?"}'

3. Access metrics:
  kubectl --namespace vllm-semantic-router-system port-forward svc/semantic-router-metrics 9190:9190
  curl http://localhost:9190/metrics

4. Access gRPC API:
  kubectl --namespace vllm-semantic-router-system port-forward svc/semantic-router 50051:50051
```



## Step 3: Install Envoy Gateway

```bash
# Install Envoy Gateway using Helm
helm upgrade -i eg oci://docker.io/envoyproxy/gateway-helm \
  --version v0.0.0-latest \
  --namespace envoy-gateway-system \
  --create-namespace \
  -f https://raw.githubusercontent.com/envoyproxy/ai-gateway/main/manifests/envoy-gateway-values.yaml

kubectl wait --timeout=2m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available
```

## Step 4: Install Envoy AI Gateway
```bash
# Install Envoy AI Gateway using Helm
helm upgrade -i aieg oci://docker.io/envoyproxy/ai-gateway-helm \
    --version v0.0.0-latest \
    --namespace envoy-ai-gateway-system \
    --create-namespace

# Install Envoy AI Gateway CRDs
helm upgrade -i aieg-crd oci://docker.io/envoyproxy/ai-gateway-crds-helm --version v0.0.0-latest --namespace envoy-ai-gateway-system

# Wait for AI Gateway Controller to be ready
kubectl wait --timeout=300s -n envoy-ai-gateway-system deployment/ai-gateway-controller --for=condition=Available
```

## Step 5: Deploy Demo LLM

We are using a pre-created backend outside the cluster

```bash
kubectl apply -f - <<EOF
apiVersion: aigateway.envoyproxy.io/v1alpha1
kind: AIServiceBackend
metadata:
  name: vllm-gpt-oss-120b
  namespace: default
spec:
  schema:
    name: OpenAI
  backendRef:
    name: vllm-gpt-oss-120b
    kind: Backend
    group: gateway.envoyproxy.io
---
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: Backend
metadata:
  name: vllm-gpt-oss-120b
  namespace: default
spec:
  endpoints:
  - fqdn:
      hostname: 10.95.161.240.nip.io
      port: 8000
EOF
```

## Step 6: Create Gateway API Resources

```bash
kubectl apply -f manifests/infrastructure/gwapi-resources.yaml
```

Output:
```bash
gatewayclass.gateway.networking.k8s.io/semantic-router created
envoyproxy.gateway.envoyproxy.io/semantic-router created
gateway.gateway.networking.k8s.io/semantic-router created
clienttrafficpolicy.gateway.envoyproxy.io/semantic-router created
aigatewayroute.aigateway.envoyproxy.io/semantic-router created
envoypatchpolicy.gateway.envoyproxy.io/ai-gateway-prepost-extproc-patch-policy created
```

