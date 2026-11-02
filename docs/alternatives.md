# Images, artifacts, and the other ways settings reach a pod

_The first question anyone asks: why build a tool, when Docker, ORAS and Flux already push things
to registries? This page is the answer. Claims are marked **verified** (run on a k3s 1.36 cluster
with containerd, spike of 2026-10) or **not tested** (from documentation or reasoning)._

## 1. An image is not an artifact

Both live in an OCI registry, behind an OCI manifest. They differ in what the manifest points at.

| | **Image** | **Artifact** |
|---|---|---|
| manifest `config` | an **image config** (`application/vnd.oci.image.config.v1+json`) with `rootfs.diff_ids`: one per layer, the digest of the _uncompressed_ layer | usually the **empty** config (`application/vnd.oci.empty.v1+json`, `{}`), or a tool's own type |
| layers | filesystem changesets: `tar`, `tar+gzip`, `tar+zstd` | anything: a file, a tarball, a Helm chart, a signature |
| `artifactType` | none | names what it is (`application/vnd.cncf.flux.config.v1+json`, …) |
| who reads it | a container runtime, which **unpacks** the layers into a filesystem | the tool that wrote it |

A Kubernetes **image volume** asks the container runtime to do what it does for any container:
pull, unpack, mount. containerd unpacks through its snapshotter, which checks every layer against
`rootfs.diff_ids`. **Without an image config, there is nothing to unpack.** That one requirement
is why settings-image exists.

## 2. What each tool produces, and what mounts

On containerd (k3s 1.36), referenced by digest:

| Producer | What it makes | As `volumes[].image` |
|---|---|---|
| **settings-image** (this format) | image config with `diff_ids`, one deterministic `tar+gzip` layer | **mounts** (**verified**) |
| the same, uncompressed `tar` layer | image | mounts (**verified**) |
| `crane append` onto an empty base | image, Docker media types | mounts (**verified**), but its tar headers carry GNU format and the digest differs from ours |
| `oras push file.yaml` / `oras push dir/` | artifact, empty config, no `diff_ids` | **refused**: `ErrImagePull … failed to unpack image on snapshotter overlayfs: mismatched image rootfs and manifest layers` (**verified**) |
| `flux push artifact` | artifact, Flux's config type, no `rootfs` | **`Running` with an empty directory**, no event, no error (**verified**). The worst outcome: the app starts with no settings and nobody is told |
| `docker build` with `FROM scratch` and `COPY` | image | should mount (**not tested**). Not reproducible by default: timestamps and build metadata change the digest on every build. BuildKit can do better with `SOURCE_DATE_EPOCH` and `rewrite-timestamp=true` |
| `oras push --config <a real image config>` with a hand-made tar layer | image, assembled by hand | could mount (**not tested**). You would compute the `diff_id` yourself, and ORAS's directory push wraps files in a top-level folder |

**CRI-O is different.** It can mount OCI **artifacts**, not only images: the Kubernetes
documentation's own example mounts `quay.io/crio/artifact:v2` (**not tested**). KEP-4639 leaves the
media types a runtime accepts up to the runtime. A real image is the shape **both** runtimes
mount, which is why the format is an image.

## 3. How settings reach a pod without an image volume

| Way | How it works | What it costs |
|---|---|---|
| **ConfigMap / Secret volume** | the kubelet projects a Kubernetes object as files | 1 MiB per object; mutable in place (the running config is whatever the object says _now_); no provenance; nothing to promote between clusters except by re-applying |
| **Bake it into the app image** | `COPY settings/ /etc/app/` in the app's Dockerfile | every settings change is a new app build and release; one image per environment or customer |
| **Init container copies from a "config image"** | an init container from the config image runs `cp -r /config/. /shared/` into an `emptyDir` | the config image must be **executable**: a base image with a shell and `cp`, so it needs CVE scanning and patching. It is the pattern image volumes were made to replace |
| **git-sync sidecar** | a sidecar clones the repository and keeps pulling | a Git credential in every pod; what runs is whatever the branch says now, declared nowhere; no rollback by digest |
| **App pulls at runtime** (an SDK, a config service, ORAS in-process) | the app fetches its own settings | the app needs network access and a credential; replicas can differ for a moment; no promotion gate. Right for one service with many tenants' settings, wrong as the default |
| **A CSI driver that mounts images** (e.g. warm-metal's container-image-csi-driver) | a node plugin pulls an image and mounts it | prior art for exactly this (**not tested**): an extra privileged daemon on every node, outside the kubelet's own image handling |
| **Flux applies the KRM** (`Kustomization`, `OCIRepository`) | Flux writes the objects into the cluster | right for objects an _operator_ acts on. Flux's OCI artifact is a source for Flux, not something a pod can mount (§2) |

An image volume is the first way to get these four at once:

- the settings are **immutable** and **declared in the pod spec by digest**;
- a change is **an ordinary rolling update**, and **rollback is `kubectl rollout undo`**;
- **nothing in the image runs**, so there is nothing to scan or patch;
- there is **no extra component**: the kubelet and the runtime do it.

## 4. When image volumes became usable

| Kubernetes | Image volumes |
|---|---|
| **v1.31** (August 2024) | **alpha**, behind the `ImageVolume` feature gate. CRI-O only |
| **v1.33** (April 2025) | **beta, still off by default**. `subPath` and `subPathExpr` added. containerd 2.1 ships alpha support |
| v1.34 | the requirement that the mount be `noexec` is dropped. That is why this format forces file modes to `0644` |
| **v1.35** | beta, **on by default** |
| **v1.36** | **stable (GA)**, with production support in both CRI-O and containerd (2.1 or later) |

So the feature exists since 1.31, but it became something you could rely on in a normal cluster only
at 1.35/1.36. Most of the existing guidance still shows a `FROM scratch` Dockerfile. As far as we
found (2026-11-02), no tool was dedicated to producing **reproducible, provenance-carrying settings
images** for it. That gap is this repository.

## Sources

- [KEP-4639: OCI VolumeSource](https://www.kubernetes.dev/resources/keps/4639/): stages,
  runtime criteria, the dropped `noexec` requirement.
- [Kubernetes v1.33: Image Volumes graduate to beta](https://kubernetes.io/blog/2025/04/29/kubernetes-v1-33-image-volume-beta/):
  off by default in 1.33, `subPath`, containerd 2.1 alpha.
- [Use an Image Volume With a Pod](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/):
  "Stable since Kubernetes v1.36; enabled by default"; the CRI-O artifact example.
- [background.md](background.md): the measurements behind the **verified** rows.
