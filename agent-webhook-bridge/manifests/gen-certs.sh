#!/usr/bin/env bash
# Generates a self-signed TLS cert for the webhook bridge and patches the
# ValidatingWebhookConfiguration with the CA bundle.
#
# Usage: ./gen-certs.sh [namespace]
set -euo pipefail

NAMESPACE="${1:-kagent}"
SERVICE="agent-webhook-bridge"
SECRET="agent-webhook-bridge-tls"
WEBHOOK_NAME="agent-webhook-bridge"
TMPDIR=$(mktemp -d)

echo "Generating TLS certificate for ${SERVICE}.${NAMESPACE}.svc"

# Generate CA
openssl genrsa -out "${TMPDIR}/ca.key" 2048
openssl req -x509 -new -nodes -key "${TMPDIR}/ca.key" \
  -subj "/CN=${SERVICE}-ca" -days 3650 -out "${TMPDIR}/ca.crt"

# Generate server key and CSR
openssl genrsa -out "${TMPDIR}/tls.key" 2048
openssl req -new -key "${TMPDIR}/tls.key" \
  -subj "/CN=${SERVICE}.${NAMESPACE}.svc" \
  -out "${TMPDIR}/server.csr" \
  -config <(cat <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
DNS.4 = ${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF
)

# Sign server cert
openssl x509 -req -in "${TMPDIR}/server.csr" \
  -CA "${TMPDIR}/ca.crt" -CAkey "${TMPDIR}/ca.key" \
  -CAcreateserial -out "${TMPDIR}/tls.crt" -days 3650 \
  -extensions v3_req \
  -extfile <(cat <<EOF
[v3_req]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
DNS.4 = ${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF
)

# Create TLS secret
kubectl create secret tls "${SECRET}" \
  --cert="${TMPDIR}/tls.crt" \
  --key="${TMPDIR}/tls.key" \
  --namespace="${NAMESPACE}" \
  --dry-run=client -o yaml | kubectl apply -f -

# Patch webhook with CA bundle
CA_BUNDLE=$(base64 < "${TMPDIR}/ca.crt" | tr -d '\n')

kubectl patch validatingwebhookconfiguration "${WEBHOOK_NAME}" \
  --type='json' \
  -p="[{\"op\": \"replace\", \"path\": \"/webhooks/0/clientConfig/caBundle\", \"value\": \"${CA_BUNDLE}\"}]"

echo "Done. Secret ${SECRET} created and webhook ${WEBHOOK_NAME} patched."
rm -rf "${TMPDIR}"
