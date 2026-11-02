#!/bin/bash
# The mount test (task test-mount): push settings images to the k3d registry, mount them in a
# pod as volumes[].image by digest, and check what the pod sees. Needs task cluster first.
#
# A digest test proves the bytes are stable; this proves a kubelet accepts them, and that every
# file arrives as format-v1.md promises: byte for byte, 0644, read-only, not executable.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-settings-image}"
REGISTRY_NAME="${REGISTRY_NAME:-k3d-settings-image-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5050}"
CONTEXT="k3d-${CLUSTER_NAME}"
NAMESPACE="settings-image-mount-test"
PUSH_TO="host.docker.internal:${REGISTRY_PORT}"
PULL_FROM="${REGISTRY_NAME}:${REGISTRY_PORT}"
BIN="${BIN:-bin/settings-image}"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

kc() { kubectl --context "${CONTEXT}" -n "${NAMESPACE}" "$@"; }
in_pod() { kc exec settings-mount -- "$@"; }

failures=0
pass() { echo "  ok    $1"; }
fail() {
    echo "  FAIL  $1" >&2
    failures=$((failures + 1))
}
check() { # check <description> <command...>
    local what="$1"
    shift
    if "$@" >/dev/null 2>&1; then pass "${what}"; else fail "${what}"; fi
}
same_bytes() { # same_bytes <local file> <path in pod>
    cmp -s "$1" <(in_pod cat "$2")
}

push() { # push <name> <folder>: prints the pull reference
    local digest
    digest="$("${BIN}" push "$2" --repository "${PUSH_TO}/$1" --plain-http --no-provenance --json | jq -r .digest)"
    echo "${PULL_FROM}/$1@${digest}"
}

echo "Pushing to ${PUSH_TO}"
REF_CONFIGMAP="$(push examples/configmap examples/configmap)"
REF_CRS="$(push examples/custom-resources examples/custom-resources)"
REF_SOPS="$(push examples/sops-secret examples/sops-secret)"
REF_EDGE="$(push edge-cases testdata/mount/edge-cases)"
printf '  %s\n' "${REF_CONFIGMAP}" "${REF_CRS}" "${REF_SOPS}" "${REF_EDGE}"

kubectl --context "${CONTEXT}" delete namespace "${NAMESPACE}" --ignore-not-found --wait >/dev/null
kubectl --context "${CONTEXT}" create namespace "${NAMESPACE}" >/dev/null
kc apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: settings-mount
spec:
  restartPolicy: Never
  securityContext:
    runAsUser: 1000
    runAsGroup: 1000
    runAsNonRoot: true
  containers:
    - name: app
      image: busybox:1.36.1
      command: [sleep, "3600"]
      securityContext:
        readOnlyRootFilesystem: true
        allowPrivilegeEscalation: false
      volumeMounts:
        - { name: configmap, mountPath: /settings/configmap, readOnly: true }
        - { name: crs, mountPath: /settings/custom-resources, readOnly: true }
        - { name: sops, mountPath: /settings/sops-secret, readOnly: true }
        - { name: edge, mountPath: /settings/edge-cases, readOnly: true }
        # A ConfigMap's files/ directory as a subPath: what an application usually mounts.
        - { name: configmap, mountPath: /etc/shop, subPath: files/shop-settings, readOnly: true }
  volumes:
    - { name: configmap, image: { reference: "${REF_CONFIGMAP}", pullPolicy: IfNotPresent } }
    - { name: crs, image: { reference: "${REF_CRS}", pullPolicy: IfNotPresent } }
    - { name: sops, image: { reference: "${REF_SOPS}", pullPolicy: IfNotPresent } }
    - { name: edge, image: { reference: "${REF_EDGE}", pullPolicy: IfNotPresent } }
EOF
if ! kc wait --for=condition=Ready pod/settings-mount --timeout=120s >/dev/null; then
    kc describe pod settings-mount >&2
    exit 1
fi
echo "Pod is Ready with four image volumes"

echo "examples/configmap"
check "files/ view through a subPath" same_bytes <(printf 'maxScoops: 3\ncurrency: EUR\n') /etc/shop/settings.yaml
check "second key of the same ConfigMap" same_bytes <(printf 'Welcome to the ice cream shop!\n') \
    /settings/configmap/files/shop-settings/welcome.txt
check "binaryData decoded (logo.png)" same_bytes \
    <(sed -n 's/^  logo.png: //p' examples/configmap/app.yaml | base64 -d) /settings/configmap/files/branding/logo.png
check "objects/ byte for byte" same_bytes examples/configmap/app.yaml /settings/configmap/objects/app.yaml

echo "examples/custom-resources"
for f in orders.yaml opening-hours.json flavours/seasonal/pumpkin.yaml pricing.toml; do
    check "objects/${f}" same_bytes "examples/custom-resources/${f}" "/settings/custom-resources/objects/${f}"
done
check "no files/ view without a ConfigMap" bash -c "! kubectl --context ${CONTEXT} -n ${NAMESPACE} \
    exec settings-mount -- test -e /settings/custom-resources/files"

echo "examples/sops-secret"
in_pod cat /settings/sops-secret/objects/database.secret.yaml > "${WORK}/secret.yaml"
check "the sealed Secret byte for byte" cmp -s examples/sops-secret/database.secret.yaml "${WORK}/secret.yaml"
check "it decrypts with the test key (the MAC survived)" bash -c \
    "SOPS_AGE_KEY_FILE=testdata/sops/age-test-key.txt sops decrypt '${WORK}/secret.yaml' \
    | grep -q 'password: correct-horse-battery-staple'"
check "the plain ConfigMap beside it has its files/ view" same_bytes <(printf 'postgres.shop.svc') \
    /settings/sops-secret/files/database/host
check "the Secret has no files/ view" bash -c "! kubectl --context ${CONTEXT} -n ${NAMESPACE} \
    exec settings-mount -- test -e /settings/sops-secret/files/database/password"

echo "edge cases"
E=/settings/edge-cases
check "binaryData with every byte 0-255" same_bytes <(python3 -c 'import sys; sys.stdout.buffer.write(bytes(range(256)))') \
    "${E}/files/edge/all-bytes.bin"
check "a dotfile key (.env)" same_bytes <(printf 'MODE=edge\n') "${E}/files/edge/.env"
check "a non-ASCII file name (PAX header)" same_bytes <(printf 'ünïcode\n') "${E}/objects/ünïcode-naam.txt"
check "a file name with a space" same_bytes <(printf 'spaces\n') "${E}/objects/with space.txt"
long="a-very-long-directory-name-to-push-past-the-hundred-byte-ustar-name-field/and-then-some-more/deep.txt"
check "a path longer than 100 bytes" same_bytes <(printf 'deep\n') "${E}/objects/${long}"
check "an empty file" in_pod test -f "${E}/objects/empty.txt" -a ! -s "${E}/objects/empty.txt"
check "a committed 0755 script is 0644" bash -c "kubectl --context ${CONTEXT} -n ${NAMESPACE} \
    exec settings-mount -- stat -c %a ${E}/objects/run.sh | grep -qx 644"
check "and does not execute" bash -c "! kubectl --context ${CONTEXT} -n ${NAMESPACE} \
    exec settings-mount -- ${E}/objects/run.sh"

echo "every volume"
check "every file is 0644, every directory 0755" bash -c "test -z \"\$(kubectl --context ${CONTEXT} \
    -n ${NAMESPACE} exec settings-mount -- find /settings \\( -type f ! -perm 644 \\) -o \\( -type d ! -perm 755 \\))\""
check "the mount is read-only" bash -c "! kubectl --context ${CONTEXT} -n ${NAMESPACE} \
    exec settings-mount -- touch /settings/configmap/new"
check "uid 1000 reads everything" in_pod find /settings -type f -exec cat {} +

if [[ "${failures}" -gt 0 ]]; then
    echo "${failures} check(s) failed. The pod is left for inspection: kubectl --context ${CONTEXT} -n ${NAMESPACE} ..." >&2
    exit 1
fi
echo "All checks passed."
