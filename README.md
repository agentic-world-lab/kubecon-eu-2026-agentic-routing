# kubecon-eu-2026-agentic-routing
Resources for talk at KubeCon + CloudNativeCon Europe 2026 Amsterdam: Intelligent Routing for Optimized Inference


## Overview

## Installation

### Prerequisites

- Kubernetes cluster (v1.20+)
- kubectl CLI
- Helm (v3+)

### Steps

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
kubectl apply -f manifests/agentgatewaybackends.yaml
```

```bash
kubectl apply -f manifests/httproutes.yaml
```

```bash
kubectl apply -f manifests/extproc-server.yaml
kubectl apply -f manifests/agentgatewaypolicy.yaml
```

```bash
export INGRESS_GW_ADDRESS=$(kubectl get svc -n agentgateway-system agentgateway-proxy -o jsonpath="{.status.loadBalancer.ingress[0]['hostname','ip']}")
echo $INGRESS_GW_ADDRESS
```

Using the default model configured in the AgentgatewayBackend

```bash
curl "$INGRESS_GW_ADDRESS/" -H content-type:application/json  -d '{
   "messages": [
     {
       "role": "user",
       "content": "follow the series: 1, 2, 3, ... What is the next number?"
     }
   ]
 }' | jq
 ```

 Using the cpu models configured in the AgentgatewayBackend

```bash
curl "$INGRESS_GW_ADDRESS/" -H content-type:application/json -H 'x-example: cpu'  -d '{
   "messages": [
     {
       "role": "user",
       "content": "follow the series: 1, 2, 3, ... What is the next number?"
     }
   ]
 }' | jq
```

Using the gpu models configured in the AgentgatewayBackend

```bash
curl "$INGRESS_GW_ADDRESS/" -H content-type:application/json -H "x-example: gpu" -d '{
   "messages": [
     {
       "role": "user",
       "content": "follow the series: 1, 2, 3, ... What is the next number?"
     }
   ]
 }' | jq
```