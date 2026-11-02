# Background: what an image volume needs, and what was measured

_What the format ([format-v2.md](format-v2.md)) rests on. Everything marked **verified** was run on
a k3s 1.36 cluster during a spike on 2026-10, unless it says otherwise. Read this before changing
anything about how the image is built._

## 1. The idea this serves

An application's settings deserve the same treatment as its code. They are typed and reviewed,
every change is attributed and versioned, and they are published as an **immutable artifact**,
delivered **by digest** and rolled back like a release. settings-image is the step that turns a
folder in Git into that artifact.

Kubernetes [image volumes](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
([KEP-4639](https://www.kubernetes.dev/resources/keps/4639/)) make the artifact mountable. A pod
names an image in `volumes[].image.reference`, and the kubelet mounts its files read-only. A new
digest in the pod spec is an ordinary rolling update, and `rollout undo` restores the old settings.

## 2. Only a real image mounts

The full comparison, including Docker, the CRI-O exception and the non-image ways settings reach a
pod, is [alternatives.md](alternatives.md).

| Built with | Mounted as `volumes[].image` |
|---|---|
| an image config with `rootfs.diff_ids`, one `tar+gzip` layer (this format) | **mounts** |
| the same, with an uncompressed `tar` layer | mounts |
| `crane append` (Docker media types) | mounts |
| `oras push` of a file or directory (empty config, no `diff_ids`) | `ErrImagePull … mismatched image rootfs and manifest layers` |
| a Flux artifact (`flux push artifact`; its config has no `rootfs`) | `Running`, with an **empty** directory and no error or event |

So "a real image" means an image config whose `rootfs.diff_ids` matches the layers. It does not
mean a base image, a shell or an entrypoint: nothing in a settings image ever runs.

A `FROM scratch` plus `COPY` Dockerfile would also give a mountable image (not tested). What it
would not give is §3's stable digest, §5's refusals, or provenance.

## 3. Facts the format depends on

| Fact | Consequence in the format |
|---|---|
| Two builds of one tree give one digest, through `touch`, `chmod` and a reversed copy order (**verified**). One changed byte gives a new digest | mtimes, modes and order are normalised (§6 of the format) |
| oras-go's `PackManifest` stamps `org.opencontainers.image.created = time.Now()` when absent (**verified**: two digests one second apart) | `created` is pinned to the epoch |
| A digest reference does no platform match: an arm64-only manifest mounted on an amd64 node (**verified**). An index costs 151 B of config and 596 B of manifest per extra platform | one manifest, `architecture: amd64` |
| The mount is `ro` but **not `noexec`**: a `0755` script in the image ran (**verified**) | file modes forced to `0644` |
| `subPath` on an image volume must be a directory: a file `subPath` fails with `ImageVolumeMountFailed` (**verified**) | `configmaps/<name>/` is a directory a container can mount with `subPath` |
| uid 1000 with a read-only root filesystem reads everything (dirs `0755 0:0`, files `0644 0:0`) (**verified**) | uid/gid 0 is fine |
| A tag is re-resolved at each container start. With `pullPolicy: Always`, a crash restart picked up new content inside a running pod, and two replicas served different values (**verified**) | consumers mount by digest, never by tag |
| A missing digest: `ImagePullBackOff`, old pods keep serving, and the rollout completes by itself once the image exists (**verified**) | a failed push cannot take an application down |

## 4. Registries

| Registry | Referrers | Notes |
|---|---|---|
| `registry:2` | fallback tag `sha256-<hex>` | `/referrers/` is 404. It refuses `DELETE` with 405, hence `SkipReferrersGC` |
| zot v2.1.5 | native referrers API | no fallback tag |
| ghcr.io | fallback tag (**verified**) | the referrer and its fallback index each appear as a package _version_. Superseded indexes stay as untagged versions, and a retention policy must allow for them |

The image digest is identical on all three, so content addressing holds across registries.

**ghcr.io credentials** (**verified**):

- A GitHub **App** installation token cannot push to ghcr.io, whatever its permissions.
- Inside GitHub Actions, the workflow's own `GITHUB_TOKEN` with `packages: write` can push to a
  package in its own organisation.
- A package created by a push from a private repository starts private.

**Flux follows a moving tag** with an `ImagePolicy` (`filterTags.pattern: '^main$'`,
`digestReflectionPolicy: Always`): it emits `name:tag@sha256:…`, and the digest wins. It ignores
`ImageRepository.spec.insecure` there, so a registry Flux reflects from must speak TLS.

## 5. Timings (spike, single runs)

| | small tree (458 B, 0.5 KB layer) | ~1 MB tree (197 KB layer) |
|---|---|---|
| pack + push, local registry, cold / warm | 52 ms / 11 ms | 149 ms / 80 ms |
| pack + push to ghcr.io, cold / warm / with provenance | 3.8 s / 1.5 s / 4.0 s | not measured |
| pod start with the image volume, cold / warm | 1.08 s / 0.81 s | 1.40 s / 0.86 s |
| pod start with an `emptyDir`, for comparison | 0.58 s | 0.58 s |

## 6. What a consuming application must do

- Mount by digest. The deployer, not the app, decides which digest runs.
- Mount `configmaps/<name>/` as a directory `subPath`, or read `resources/` and decode the kinds
  it knows, and `raw/` for anything else.
- **Fail on a missing settings file instead of starting with defaults.** A bad configuration should
  keep the new pod un-ready, so the rolling update leaves the old pod serving.
- Read provenance from the referrer if it wants to show who changed what. Settings load fine
  without it.
