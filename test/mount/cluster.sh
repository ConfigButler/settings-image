#!/bin/bash
# Creates, or reuses, the k3d cluster and registry that the mount test runs against
# (task cluster). Modelled on gitops-reverser's test/e2e/cluster/start-cluster.sh, without its
# audit webhook: one k3s node, a k3d registry the node pulls from over plain HTTP, and the
# kubeconfig rewritten to host.docker.internal for Docker-outside-of-Docker.
#
# The registry has two names: the devcontainer pushes to host.docker.internal:${REGISTRY_PORT},
# and a pod pulls the same image as ${REGISTRY_NAME}:${REGISTRY_PORT}.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-settings-image}"
REGISTRY_NAME="${REGISTRY_NAME:-k3d-settings-image-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5050}"
K3S_IMAGE="${K3S_IMAGE:-rancher/k3s:v1.37.1-k3s1}"
CONTEXT="k3d-${CLUSTER_NAME}"

registry_exists() {
    k3d registry list -o json 2>/dev/null | jq -e --arg n "${REGISTRY_NAME}" 'any(.[]; .name == $n)' >/dev/null
}

cluster_exists() {
    k3d cluster list -o json 2>/dev/null | jq -e --arg n "${CLUSTER_NAME}" 'any(.[]; .name == $n)' >/dev/null
}

rewrite_kubeconfig() {
    # k3d writes https://0.0.0.0:<port>, which this container cannot reach. The Docker host can.
    local server port
    server="$(kubectl config view --context "${CONTEXT}" --minify -o jsonpath='{.clusters[0].cluster.server}')"
    if [[ "${server}" =~ ^https://(0\.0\.0\.0|127\.0\.0\.1|localhost):([0-9]+)$ ]] \
        && getent hosts host.docker.internal >/dev/null 2>&1; then
        port="${BASH_REMATCH[2]}"
        kubectl config set-cluster "${CONTEXT}" --server="https://host.docker.internal:${port}" \
            --tls-server-name=localhost >/dev/null
        echo "kubeconfig: ${CONTEXT} -> https://host.docker.internal:${port}"
    fi
}

main() {
    mkdir -p "${HOME}/.kube"
    if registry_exists; then
        echo "Reusing registry ${REGISTRY_NAME}"
    else
        # k3d prefixes "k3d-" to the name it is given.
        k3d registry create "${REGISTRY_NAME#k3d-}" --port "0.0.0.0:${REGISTRY_PORT}"
    fi

    if cluster_exists; then
        echo "Reusing cluster ${CLUSTER_NAME}"
    else
        k3d cluster create "${CLUSTER_NAME}" \
            --image "${K3S_IMAGE}" \
            --servers 1 --agents 0 \
            --registry-use "${REGISTRY_NAME}:${REGISTRY_PORT}" \
            --k3s-arg "--disable=traefik@server:0" \
            --k3s-arg "--disable=servicelb@server:0" \
            --k3s-arg "--disable=metrics-server@server:0" \
            --kubeconfig-update-default \
            --kubeconfig-switch-context=false \
            --wait
    fi
    k3d kubeconfig merge "${CLUSTER_NAME}" --kubeconfig-merge-default --kubeconfig-switch-context=false >/dev/null
    rewrite_kubeconfig

    kubectl --context "${CONTEXT}" wait --for=condition=Ready node --all --timeout=120s
    kubectl --context "${CONTEXT}" version | sed 's/^/  /'
    echo "Cluster ready: kubectl --context ${CONTEXT}; registry: host.docker.internal:${REGISTRY_PORT}"
}

main "$@"
