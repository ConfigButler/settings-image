#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Build one settings folder with every tool that can put files in a registry, and measure:
#   - is the digest the same when you build again, on another machine, after chmod +x?
#   - is it an image (has rootfs.diff_ids) or an artifact?
#   - can a non-root container read the files when the result is mounted as an image?
#
# Usage: compare/run.sh [settings-folder]     (default: examples/custom-resources)
# Needs: docker 28+ (--mount type=image, containerd image store), crane, jq, and whichever of
# settings-image, imgpkg, oras, flux are installed; a missing tool is reported, not fatal.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
folder=$(cd "${1:-$here/../examples/custom-resources}" && pwd)
port=${COMPARE_PORT:-5099}
registry_image=registry:2.8.3
busybox=busybox:1.36.1

# The tools push to $push, the Docker daemon pulls from $pull: the same registry. In a
# devcontainer that uses the host's daemon, "localhost" is a different machine to each of them.
pull=localhost:$port
if [ -n "${COMPARE_PUSH:-}" ]; then
  push=$COMPARE_PUSH
elif [ -f /.dockerenv ] && getent hosts host.docker.internal >/dev/null; then
  push=host.docker.internal:$port
else
  push=$pull
fi

for need in docker crane jq; do
  command -v "$need" >/dev/null || { echo "needs $need" >&2; exit 1; }
done
for d in resources raw; do
  [ -d "$folder/$d" ] || { echo "$folder has no $d/ (the Dockerfiles copy both)" >&2; exit 1; }
done

work=$(mktemp -d)
registry=compare-registry-$$
cleanup() {
  docker rm -f "$registry" >/dev/null 2>&1 || true
  docker image ls --format '{{.Repository}}:{{.Tag}}' | grep "^$pull/" | xargs -r docker image rm -f >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

docker run -d --rm --name "$registry" -p "$port:5000" "$registry_image" >/dev/null
for _ in $(seq 30); do curl -fs "http://$push/v2/" >/dev/null && break; sleep 0.5; done
docker pull -q "$busybox" >/dev/null

# Three copies of the same folder. Only resources/ and raw/, so every tool gets the same input.
#   a  a normal checkout: umask 022, mtimes now
#   b  "another machine": umask 077 (files 0600, dirs 0700) and every mtime in 2030
#   c  a, with one file in raw/ made executable (a script committed with +x)
(umask 022 && mkdir "$work/a" && cp -r "$folder/resources" "$folder/raw" "$work/a/")
(umask 077 && mkdir "$work/b" && cp -r "$folder/resources" "$folder/raw" "$work/b/")
find "$work/b" -exec touch -d '2030-01-01T12:00:00Z' {} +
cp -a "$work/a" "$work/c"
chmod +x "$(find "$work/c/raw" -type f | sort | head -1)"
mapfile -t files < <(cd "$work/a" && find resources raw -type f | sort)

# --- one function per tool: build <dir> into <name>:<tag> ------------------------------------

build_settings_image() {
  settings-image push "$1" --repository "$push/$2" --tag "$3" --plain-http --no-provenance >/dev/null
}
build_imgpkg() { imgpkg push -i "$push/$2:$3" -f "$1" --registry-insecure >/dev/null; }
build_imgpkg_pp() { imgpkg push -i "$push/$2:$3" -f "$1" --preserve-permissions --registry-insecure >/dev/null; }
build_crane_plain() {
  tar -C "$1" -czf "$work/layer.tgz" resources raw
  crane append -f "$work/layer.tgz" -t "$push/$2:$3" --insecure >/dev/null 2>&1
}
build_crane_det() {
  tar -C "$1" --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner \
    --mode='u=rwX,go=rX' -cf - resources raw | gzip -n -9 >"$work/layer.tgz"
  crane append -f "$work/layer.tgz" -t "$push/$2:$3" --insecure >/dev/null 2>&1
}
build_docker_default() {
  # --no-cache: what a fresh CI runner does. A cache hit would hand back the previous image.
  docker build -q --no-cache -f "$here/Dockerfile.default" -t "$pull/$2:$3" "$1" >/dev/null
  docker push -q "$pull/$2:$3" >/dev/null
}
build_docker_repro() {
  SOURCE_DATE_EPOCH=0 docker buildx build -q --no-cache --provenance=false \
    -f "$here/Dockerfile.reproducible" \
    --output "type=image,name=$pull/$2:$3,push=true,rewrite-timestamp=true" "$1" >/dev/null
}
build_oras() { (cd "$1" && oras push --plain-http "$push/$2:$3" resources raw >/dev/null); }
build_flux() {
  flux push artifact "oci://$push/$2:$3" --path="$1" --source=compare --revision=compare \
    --insecure-registry >/dev/null 2>&1
}

# name | needs | function | label
tools=(
  "settings-image|settings-image|build_settings_image|settings-image push"
  "imgpkg|imgpkg|build_imgpkg|imgpkg push"
  "imgpkg-pp|imgpkg|build_imgpkg_pp|imgpkg push --preserve-permissions"
  "crane-plain|crane|build_crane_plain|crane append, plain tar -czf"
  "crane-det|crane|build_crane_det|crane append, deterministic tar"
  "docker-default|docker|build_docker_default|docker build, Dockerfile.default"
  "docker-repro|docker|build_docker_repro|docker buildx, Dockerfile.reproducible"
  "oras|oras|build_oras|oras push"
  "flux|flux|build_flux|flux push artifact"
)

digest() { crane digest "$push/$1" --insecure 2>/dev/null || echo failed; }

# An image has a config with rootfs.diff_ids; containerd needs them to unpack the layers.
shape() {
  if crane config "$push/$1" --insecure 2>/dev/null | jq -e '.rootfs.diff_ids | length > 0' >/dev/null; then
    echo image
  else
    echo "artifact ($(crane manifest "$push/$1" --insecure | jq -r '.config.mediaType'))"
  fi
}

# Mount the result as an image, as uid 1000 (what a restricted pod runs as), and read every file.
reads() {
  docker pull -q "$pull/$1" >/dev/null 2>&1 || { echo "no: pull refused"; return; }
  docker run --rm --user 1000 --mount "type=image,source=$pull/$1,target=/s" "$busybox" sh -c '
    [ -n "$(ls -A /s 2>/dev/null)" ] || { echo "no: mounts EMPTY, no error"; exit; }
    for f in "$@"; do cat "/s/$f" >/dev/null 2>&1 || { echo "no: Permission denied"; exit; }; done
    echo yes' sh "${files[@]}" 2>&1 | tail -1
}

same() { [ "$1" = "$2" ] && echo same || echo "**differs**"; }

echo "Folder: $folder"
echo "Tools push to $push; Docker pulls from $pull"
if command -v settings-image >/dev/null; then
  echo "settings-image: $(command -v settings-image), $(settings-image version 2>&1 | head -1)"
fi
echo
echo "| Tool | digest of a | a, built again | b: umask 077, mtime 2030 | c: chmod +x | Shape | uid 1000 reads b |"
echo "|---|---|---|---|---|---|---|"
for t in "${tools[@]}"; do
  IFS='|' read -r name needs fn label <<<"$t"
  if ! command -v "$needs" >/dev/null; then
    echo "| $label | $needs not installed | | | | | |"
    continue
  fi
  for tag in a b c a2; do
    # Timestamps in manifests have one-second resolution: make sure the clock moved.
    [ "$tag" = a2 ] && sleep 1.1
    "$fn" "$work/${tag%2}" "$name" "$tag" || echo "build failed: $label ($tag)" >&2
  done
  a=$(digest "$name:a")
  echo "| $label | \`${a:7:12}\` | $(same "$a" "$(digest "$name:a2")") | $(same "$a" "$(digest "$name:b")") \
| $(same "$a" "$(digest "$name:c")") | $(shape "$name:a") | $(reads "$name:b") |"
done
