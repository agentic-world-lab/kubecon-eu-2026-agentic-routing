## Deploy a cluster (k3s)
1. Deploying a k3s Cluster
```bash
curl -sfL https://get.k3s.io  | INSTALL_K3S_EXEC="--disable=traefik" sh -
```

2. Creating storage to k3s (longhorn)
```bash
kubectl apply -f https://raw.githubusercontent.com/longhorn/longhorn/v1.10.1/deploy/longhorn.yaml
```
3. Remove local-path as default storage backend
```bash
kubectl patch storageclass local-path \
  -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":null}}}'
```

## Step 1: Install AgentGateway

```bash
export GATEWAY_VERSION="v2.2.0-main"
```

```bash
export OPENAI_API_KEY="<your_openai_api_key_here>"
```

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml
```

```bash
helm upgrade -i agentgateway-crds oci://ghcr.io/kgateway-dev/charts/agentgateway-crds \
--create-namespace --namespace agentgateway-system \
--version ${GATEWAY_VERSION} \
--set controller.image.pullPolicy=Always

helm upgrade -i agentgateway oci://ghcr.io/kgateway-dev/charts/agentgateway \
  --namespace agentgateway-system \
  --version ${GATEWAY_VERSION} \
  --set controller.image.pullPolicy=Always \
  --set controller.extraEnv.KGW_ENABLE_GATEWAY_API_EXPERIMENTAL_FEATURES=true
```

```bash
helm template  kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds \
--create-namespace --namespace kgateway-system --output-dir ./out2 \
--version v2.2.0-main \
--set controller.image.pullPolicy=Always
```


```bash
kubectl apply -f manifests/gateway.yaml
```

```bash
kubectl apply -f- <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: openai-secret
  namespace: agentgateway-system
type: Opaque
stringData:
  Authorization: $OPENAI_API_KEY
EOF
```

```bash
kubectl apply -f manifests/semantic-router/agentgatewaybackends.yaml
```

```bash
kubectl apply -f manifests/semantic-router/httproutes.yaml
```

-- for local deployment
```bash
kubectl apply -f manifests/semantic-router/httproutes_local.yaml
```

```bash
kubectl apply -f manifests/semantic-router/agentgatewaypolicy.yaml
```

```bash
export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
echo $INGRESS_GW_ADDRESS
```

## Step 2: Deploy the semantic Router
1. Prepare the infrastructure

```bash
kubectl create ns vllm-semantic-router-system
```

```bash
export HUGGINGFACE_HUB_TOKEN="<add your hf token here>"
```
```bash
kubectl create secret generic hf-token-secret \
  --namespace vllm-semantic-router-system \
  --from-literal=token=${HUGGINGFACE_HUB_TOKEN}
```

2. Install the semantic router
```bash
helm upgrade -i semantic-router oci://ghcr.io/vllm-project/charts/semantic-router \
--version v0.0.0-latest --namespace vllm-semantic-router-system   --create-namespace   -f manifests/semantic-router/values.yaml --set persistence.storageClassName=longhorn   --set image.repository=fjvicens/vllm-sr-extproc --set image.tag=dev --set config.classifier.pii_model.pii_mapping_path="models/pii_classifier_modernbert-base_presidio_token_model/label_mapping.json" --set image.pullPolicy=Always
```

2.1. For local deployment with OpenAI key

```bash
helm upgrade -i semantic-router oci://ghcr.io/vllm-project/charts/semantic-router:v0.0.0-latest --namespace vllm-semantic-router-system   --create-namespace   -f manifests/semantic-router/values_local.yaml --set persistence.storageClassName=longhorn   --set image.repository=fjvicens/vllm-sr-extproc --set image.tag=dev --set config.classifier.pii_model.pii_mapping_path="models/pii_classifier_modernbert-base_presidio_token_model/label_mapping.json" --set image.pullPolicy=Always
```


```bash
curl -i -X POST http://10.95.161.251/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MoM",
    "messages": [
      {"role": "user", "content": "What is the derivative of f(x) = x^3?"}
    ]
  }'
```

```bash
curl -i -X POST http://10.95.161.250 \
  -H "Content-Type: application/json" \
  -d '{
    "model": "MoM",
    "messages": [
      {"role": "user", "content": "hello!"}
    ]
  }'
```