# Deploying vLLM for CPU

Export your env variables:

```bash
export HF_TOKEN=
# export MODEL="Qwen/Qwen2.5-VL-7B-Instruct"
export MODEL="meta-llama/Llama-2-7b-hf"
```

## Creating vLLM Namespace
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: vllm
EOF
```

## Creating PVCs for models 
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: vllm-models
  namespace: vllm
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 200Gi
EOF
```

## Creating HuggingFace secret to download the models
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: hf-token-secret
  namespace: vllm
type: Opaque
stringData:
  token: "${HF_TOKEN}"
EOF
```

## Deploying the model exposing it with a service

```bash
cat <<EOF |kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm-server
  namespace: vllm
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: vllm
  template:
    metadata:
      labels:
        app.kubernetes.io/name: vllm
    spec:
      containers:
      - name: vllm
        image: public.ecr.aws/q9t5s3a7/vllm-cpu-release-repo:v0.13.0
        command: ["/bin/sh", "-c"]
        args: [
          "vllm serve ${MODEL}"
        ]
        env:
        - name: HF_TOKEN
          valueFrom:
            secretKeyRef:
              name: hf-token-secret
              key: token
        - name: VLLM_LOGGING_LEVEL
          value: DEBUG
        ports:
          - containerPort: 8000
        volumeMounts:
          - name: llama-storage
            mountPath: /root/.cache/huggingface
      volumes:
      - name: llama-storage
        persistentVolumeClaim:
          claimName: vllm-models
---
apiVersion: v1
kind: Service
metadata:
  name: vllm-server
  namespace: vllm
spec:
  selector:
    app.kubernetes.io/name: vllm
  ports:
  - protocol: TCP
    port: 8000
    targetPort: 8000
  type: ClusterIP
EOF
``` 

## OpenShift Deployment
```bash
oc adm policy add-scc-to-user privileged -z default -n vllm
```

```bash
cat <<EOF |kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm-server
  namespace: vllm
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: vllm
  template:
    metadata:
      labels:
        app.kubernetes.io/name: vllm
    spec:
      containers:
      - name: vllm
        # image: public.ecr.aws/q9t5s3a7/vllm-cpu-release-repo:v0.13.0
        image: vllm/vllm-openai:latest
        # command: ["/bin/sh", "-c"]
        # args: [
        #   "vllm serve ${MODEL}"
        # ]
        args:
          - "--model"
          - "${MODEL}"
          - "--dtype"
          - "float32"
          - "--device"
          - "cpu"
          - "--port"
          - "8000"
        env:
        - name: HF_TOKEN
          valueFrom:
            secretKeyRef:
              name: hf-token-secret
              key: token
        - name: VLLM_LOGGING_LEVEL
          value: DEBUG
        ports:
          - containerPort: 8000
        securityContext:
          allowPrivilegeEscalation: true
        resources:
          requests:
            cpu: 48
            memory: "256Gi"
          limits:
            cpu: 48
            memory: "256Gi"
        volumeMounts:
          - name: llama-storage
            mountPath: /root/.cache/huggingface
      volumes:
      - name: llama-storage
        persistentVolumeClaim:
          claimName: vllm-models
EOF
```

```bash
cat <<EOF |kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: vllm-server
  namespace: vllm
spec:
  selector:
    app.kubernetes.io/name: vllm
  ports:
  - protocol: TCP
    port: 8000
    targetPort: 8000
  type: LoadBalancer
  loadBalancerIP: 10.95.161.240
EOF
``` 
## NVIDIA GPU

```bash
cat <<EOF |kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gpt-oss-120b
  namespace: vllm
  labels:
    app.kubernetes.io/name: vllm
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: vllm
  template:
    metadata:
      labels:
        app.kubernetes.io/name: vllm
    spec:
      volumes:
      - name: cache-volume
        persistentVolumeClaim:
          claimName: vllm-models
      # vLLM needs to access the host's shared memory for tensor parallel inference.
      #- name: shm
      #  emptyDir:
      #    medium: Memory
      #    sizeLimit: "2Gi"
      containers:
      - name: gpt-oss-120b
        image: vllm/vllm-openai:latest
        command: ["/bin/sh", "-c"]
        args: [
          "vllm serve openai/gpt-oss-120b --async-scheduling --no-enable-prefix-caching --max-num-batched-tokens 8192 --max-cudagraph-capture-size 2048 --stream-interval 20 --tensor-parallel-size 1 --tool-call-parser openai --enable-auto-tool-choice"
        ]
        env:
        - name: HF_TOKEN
          valueFrom:
            secretKeyRef:
              name: hf-token-secret
              key: token
        ports:
        - containerPort: 8000
        securityContext:
          allowPrivilegeEscalation: true
        resources:
          limits:
            # cpu: "120"
            # memory: 256G
            nvidia.com/gpu: "1"
          requests:
            # cpu: "120"
            # memory: 128G
            nvidia.com/gpu: "1"
        volumeMounts:
        - mountPath: /root/.cache/huggingface
          name: cache-volume
        #- name: shm
        #  mountPath: /dev/shm
        #livenessProbe:
        #  httpGet:
        #    path: /health
        #    port: 8000
        #  initialDelaySeconds: 360
        #  periodSeconds: 10
        #readinessProbe:
        #  httpGet:
        #    path: /health
        #    port: 8000
        #  initialDelaySeconds: 360
        #  periodSeconds: 5
EOF
```


## Kagent Installation

If using OpenAI:

```bash
export OPENAI_API_KEY=
```

```bash
export KAGENT_VERSION=0.7.4
```

```bash
helm upgrade --install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
    --namespace kagent \
    --version $KAGENT_VERSION \
    --create-namespace
```

```bash
helm upgrade --install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
    --namespace kagent \
    --create-namespace \
    --version $KAGENT_VERSION \
    --set providers.openAI.apiKey=$OPENAI_API_KEY \
    --set service.type=LoadBalancer
```


## Kgateway Installation

This guide walks you through installing KGateway with AgentGateway.

```bash
export KGATEWAY_VERSION=v2.1.1
```

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml
```

```bash
helm upgrade -i kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds \
    --create-namespace \
    --namespace kgateway-system \
    --version $KGATEWAY_VERSION \
    --set controller.image.pullPolicy=Always
```

Install kgateway with AgentGateway enabled

```bash
helm upgrade -i kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway \
    --namespace kgateway-system \
    --create-namespace \
    --version $KGATEWAY_VERSION \
    --set controller.image.pullPolicy=Always \
    --set agentgateway.enabled=true \
    --set agentgateway.enableAlphaAPIs=true
```

Deploy a gateway to access the kagent UI:

```bash
kubectl apply -f gateway.yaml
```

```bash
sleep 5
kubectl rollout status deploy kagent-gw-ui -n kagent --timeout=90s
```

Get the gateway IP and register a domain:

```bash
export GW_IP=$(kubectl get gtw -n kagent kagent-gw-ui -ojsonpath='{.status.addresses[0].value}')
../../register-domain.sh my-kagent.example ${GW_IP}
```

Access the UI at http://my-kagent.example:8080