#!/usr/bin/env bash
# multi-cluster-teardown.sh — Delete Kind clusters created by multi-cluster-setup.sh.
set -euo pipefail

KIND="${KIND:-kind}"
CLUSTER_A="${CLUSTER_A:-kube-assist-a}"
CLUSTER_B="${CLUSTER_B:-kube-assist-b}"
KUBECONFIG_A="${KUBECONFIG_A:-/tmp/kind-${CLUSTER_A}.yaml}"
KUBECONFIG_B="${KUBECONFIG_B:-/tmp/kind-${CLUSTER_B}.yaml}"

echo "==> Deleting Kind cluster '${CLUSTER_A}'..."
${KIND} delete cluster --name "${CLUSTER_A}" 2>/dev/null || true
rm -f "${KUBECONFIG_A}"

echo "==> Deleting Kind cluster '${CLUSTER_B}'..."
${KIND} delete cluster --name "${CLUSTER_B}" 2>/dev/null || true
rm -f "${KUBECONFIG_B}"

echo "==> Multi-cluster environment torn down."
