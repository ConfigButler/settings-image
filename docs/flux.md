# Roll out settings with Flux

_Design, 2026-11-03. The Flux side is standard Flux image automation and works today with any
image; what settings-image adds is a tag that never moves backwards ([plan.md](plan.md), step 2h,
not built yet). This page becomes a section of the README once step 2h is observed._

**A settings change becomes a Git commit in your cluster repository, made by Flux, and a rollout.
Nobody edits a digest by hand, and nothing rolls back without a commit saying so.**

```text
settings repo                 registry                      cluster repo              cluster
configs/shop  ──push──▶  shop-settings                ◀──watch── Flux ──commit──▶  Deployment
  (a commit)     Action    :main-1762115267-0123456          picks the            volumes[].image
                           :main-1762201667-89abcde ◀─────── highest tag           .reference = …@sha256:…
```

1. A commit changes `configs/shop`. The Action pushes the settings image with a **timestamp tag**,
   `main-<commit time>-<sha7>` ([format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order)).
2. Flux's `ImageRepository` sees the new tag; its `ImagePolicy` picks the highest number.
3. Flux's `ImageUpdateAutomation` writes `name:tag@sha256:…` into the Deployment's image volume
   and commits it. The digest wins over the tag, so the pod mounts exactly that image.
4. The Deployment rolls out. `git log` in the cluster repository says when each settings version
   went live, and `oras discover` on the digest says which settings commit built it.

## Why a timestamp tag, not `main`

Following the moving `main` tag works until someone presses **Re-run jobs** on an old workflow run.
The old commit is rebuilt, `main` moves back to its digest, and Flux dutifully rolls the cluster
back, with no commit that says why. A workflow's `concurrency` group orders new runs, not re-runs.

The timestamp tag comes from the commit that last changed the folder, so it is reproducible and
never moves. A re-run only re-pushes a tag that already exists. The highest still wins, so the
cluster only ever moves forward, and every step is a commit in Flux's history.

## What you write

In the settings repository's workflow:

```yaml
- uses: ConfigButler/settings-image@v0
  with:
    path: configs/shop
    repository: ghcr.io/acme/shop-settings
    timestamp-tag: ${{ github.ref_name }}
```

In the cluster repository (Flux 2.7 or later, image automation APIs `v1`):

```yaml
apiVersion: image.toolkit.fluxcd.io/v1
kind: ImageRepository
metadata:
  name: shop-settings
  namespace: flux-system
spec:
  image: ghcr.io/acme/shop-settings
  interval: 1m
---
apiVersion: image.toolkit.fluxcd.io/v1
kind: ImagePolicy
metadata:
  name: shop-settings
  namespace: flux-system
spec:
  imageRepositoryRef:
    name: shop-settings
  digestReflectionPolicy: IfNotPresent # the tag never moves, so its digest is read once
  filterTags:
    pattern: '^main-(?P<ts>[0-9]+)-[0-9a-f]{7}$'
    extract: '$ts'
  policy:
    numerical:
      order: asc
```

And the marker on the image volume, where `ImageUpdateAutomation` (the usual `Setters` strategy)
writes the new reference:

```yaml
volumes:
  - name: settings
    image:
      reference: ghcr.io/acme/shop-settings:main-1762115267-0123456@sha256:80b52437… # {"$imagepolicy": "flux-system:shop-settings"}
      pullPolicy: IfNotPresent
```

Mounting by digest stays the rule ([format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order)):
the tag in the reference is for people, the digest is what the kubelet pulls.

## Registry access

Flux reads the registry the way it reads any image: anonymously for a public image, a `secretRef`
pull secret, or the cloud's workload identity (`spec.provider`). A registry that settings-image
pushes to with a GitHub OIDC token ([cli.md §4](cli.md#4-registry-credentials)) still needs Flux
to have its own read access. Flux's `ImageRepository` ignores `spec.insecure` when it reflects a
digest, so the registry must speak TLS ([background.md](background.md)).

## Not yet

- **A guard on the branch tag** that refuses to move it to a commit that is not a descendant of the
  current one. It would make following `main` safe too, but it must read the current tag's
  provenance back from the registry and handle reverts and force-pushes. The timestamp tag comes
  first.
- **The controller** (plan phase 3) reads a Flux `GitRepository` and publishes from inside the
  cluster, in seconds instead of a runner's minute. The same `ImagePolicy` then follows it.
