#!/usr/bin/env bash
# multi-cluster-setup.sh — Create two Kind clusters for console backend integration testing.
# Kubeconfigs are written to /tmp so they don't pollute ~/.kube/config.
set -euo pipefail

KIND="${KIND:-kind}"
CLUSTER_A="${CLUSTER_A:-kube-assist-a}"
CLUSTER_B="${CLUSTER_B:-kube-assist-b}"
KUBECONFIG_A="${KUBECONFIG_A:-/tmp/kind-${CLUSTER_A}.yaml}"
KUBECONFIG_B="${KUBECONFIG_B:-/tmp/kind-${CLUSTER_B}.yaml}"

echo "==> Creating Kind cluster '${CLUSTER_A}'..."
if ${KIND} get clusters 2>/dev/null | grep -q "^${CLUSTER_A}$"; then
    echo "    Cluster '${CLUSTER_A}' already exists, skipping."
else
    ${KIND} create cluster --name "${CLUSTER_A}" --kubeconfig "${KUBECONFIG_A}" --wait 60s
fi

echo "==> Creating Kind cluster '${CLUSTER_B}'..."
if ${KIND} get clusters 2>/dev/null | grep -q "^${CLUSTER_B}$"; then
    echo "    Cluster '${CLUSTER_B}' already exists, skipping."
else
    ${KIND} create cluster --name "${CLUSTER_B}" --kubeconfig "${KUBECONFIG_B}" --wait 60s
fi

echo ""
echo "==> Multi-cluster environment ready."
echo "    Cluster A: ${CLUSTER_A}  kubeconfig=${KUBECONFIG_A}"
echo "    Cluster B: ${CLUSTER_B}  kubeconfig=${KUBECONFIG_B}"
echo ""
echo "    Start console backend:"
echo "      go run ./cmd/console-backend/ --kubeconfigs=${CLUSTER_A}=${KUBECONFIG_A},${CLUSTER_B}=${KUBECONFIG_B}"
