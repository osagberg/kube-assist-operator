#!/usr/bin/env bash
# multi-cluster-seed.sh — Install CRDs and deploy sample workloads into Kind clusters.
# Idempotent: safe to re-run.
set -euo pipefail

KUBECONFIG_A="${KUBECONFIG_A:-/tmp/kind-kube-assist-a.yaml}"
KUBECONFIG_B="${KUBECONFIG_B:-/tmp/kind-kube-assist-b.yaml}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
KUSTOMIZE="${PROJECT_ROOT}/bin/kustomize"

# Ensure kustomize binary exists.
if [[ ! -x "${KUSTOMIZE}" ]]; then
    echo "==> kustomize not found at ${KUSTOMIZE}, building..."
    make -C "${PROJECT_ROOT}" kustomize
fi

seed_cluster() {
    local label="$1"
    local kc="$2"

    echo "==> Seeding cluster '${label}' (kubeconfig=${kc})..."

    # Wait for nodes to be ready.
    echo "    Waiting for node readiness..."
    kubectl --kubeconfig="${kc}" wait --for=condition=Ready node --all --timeout=60s

    # Install CRDs.
    echo "    Installing CRDs..."
    "${KUSTOMIZE}" build "${PROJECT_ROOT}/config/crd" | kubectl --kubeconfig="${kc}" apply -f -

    # Create demo-app namespace (idempotent).
    echo "    Creating demo-app namespace..."
    kubectl --kubeconfig="${kc}" create namespace demo-app \
        --dry-run=client -o yaml | kubectl --kubeconfig="${kc}" apply -f -

    # Deploy sample workloads.
    echo "    Deploying sample workloads..."
    kubectl --kubeconfig="${kc}" apply -n demo-app -f "${SCRIPT_DIR}/multi-cluster-workloads.yaml"

    echo "    Cluster '${label}' seeded."
}

seed_cluster "a" "${KUBECONFIG_A}"
seed_cluster "b" "${KUBECONFIG_B}"

echo ""
echo "==> Seeding complete."
