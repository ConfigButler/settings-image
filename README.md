# settings-image

Build a folder of settings into a small, reproducible OCI image, and mount it next to your
application with a Kubernetes [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).

> **Status: prototype, v0.1.0.** `settings-image build` and `push` work and match the format's
> known answers, a composite GitHub Action runs them (`ConfigButler/settings-image@v0`), and each
> release carries CLI binaries with a cosign-signed checksums file. See [docs/plan.md](docs/plan.md).

## Why now: Kubernetes can mount an image as a volume

```yaml
spec:
  containers:
    - name: app
      volumeMounts:
        - { name: settings, mountPath: /etc/app, readOnly: true }
  volumes:
    - name: settings
      image:
        reference: ghcr.io/acme/app-settings@sha256:80b52437…   # by digest, always
```

| Kubernetes | Image volumes |
|---|---|
| v1.31 (August 2024) | alpha, behind a feature gate |
| v1.33 (April 2025) | beta, still off by default; `subPath` support |
| v1.35 | **on by default** |
| v1.36 | **stable** |

That is what this tool is for. Until image volumes, the way to ship settings as an image was an init
container that copied files out of it. That needs an image that can *run*: a base image with a shell,
to scan and patch. Now the kubelet mounts the files itself, read-only, from an image that contains
nothing but your settings.

What you get:

- The settings that run are **declared in the pod spec, by digest**.
- A change is **an ordinary rolling update**, and **`kubectl rollout undo` rolls the settings back**.
- **Nothing in the image executes.**

## Why a tool, when Docker, ORAS and Flux already push to registries

Because the volume has to be a **real image**: an image config whose `rootfs.diff_ids` lists its
layers, so the container runtime can unpack it. Generic OCI artifacts are not that:

- a plain `oras push` is refused when the kubelet pulls it;
- a Flux artifact (`flux push artifact`) mounts as an **empty directory, with no error**;
- a `FROM scratch` Dockerfile does give an image, but a new digest on every build.

The full comparison is in [docs/alternatives.md](docs/alternatives.md).

settings-image is being built to produce the real thing, and to add what settings need:

- **The same folder gives the same digest.** A commit that touches other files rolls no pods, and a
  rollback lands on the old digest.
- **It refuses bad input before anything is pushed**: an empty folder, a link that leaves the
  folder, a Kubernetes `Secret` in `resources/` that is not SOPS-encrypted.
- **Provenance:** the commit and its author, attached to the image as an OCI referrer.
- **A GitHub Action**, so a commit that changes the folder becomes a new image.

A settings folder says what each file is:

```text
configs/shop/
  resources/   Kubernetes resources: read and checked; a ConfigMap's keys also become plain files
  raw/         anything else: copied byte for byte, never read
```

The image holds `resources/` and `raw/` as they are in Git, plus `configmaps/<name>/<key>` for every
ConfigMap (unless annotated `settings-image.configbutler.ai/files: "false"`). The contract is
[docs/format-v2.md](docs/format-v2.md).

## Use it in your CI

### 1. Lay out the folder

```text
configs/shop/
  resources/app.yaml        a ConfigMap: its keys become configmaps/shop-settings/<key>
  raw/pricing.toml          anything else, as is
```

Check it locally with `settings-image build configs/shop`: it prints the digest, or refuses bad
input with exit code 3 and names the file.

### 2. Publish on every commit that changes it (GitHub Actions)

```yaml
# .github/workflows/shop-settings.yml
name: Shop settings

on:
  push:
    branches: [main]
    paths: [configs/shop/**]

permissions:
  contents: read
  packages: write # push to ghcr.io with the workflow's own token

# Publish in commit order: a later run must not finish before an earlier one.
concurrency:
  group: shop-settings
  cancel-in-progress: false

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0 # provenance names the commit that last changed the folder
      - id: settings
        uses: ConfigButler/settings-image@v0 # or pin a release SHA
        with:
          path: configs/shop
          repository: ghcr.io/${{ github.repository_owner }}/shop-settings
      - run: echo "Mount ${{ steps.settings.outputs.reference }}" >> "$GITHUB_STEP_SUMMARY"
```

A commit that touches other folders runs nothing, and a rebuild of the same folder gives the same
digest (`changed: false`). Each image gets a `sha-<commit>` tag, a branch tag (`main`), and the
commit and its author as an OCI referrer. All inputs and outputs:
[docs/github-action.md](docs/github-action.md).

### Any other CI

Download the CLI from a [release](https://github.com/ConfigButler/settings-image/releases)
(linux/amd64, linux/arm64, darwin/arm64), check it, and push:

```bash
VERSION=v0.1.0
base="https://github.com/ConfigButler/settings-image/releases/download/${VERSION}"
curl -fsSLO "${base}/settings-image_linux_amd64"
curl -fsSL "${base}/settings-image.sha256sums" | sha256sum -c --ignore-missing
install -m 0755 settings-image_linux_amd64 /usr/local/bin/settings-image

echo "${REGISTRY_PASSWORD}" | settings-image push configs/shop \
  --repository registry.example.com/acme/shop-settings \
  --username ci --password-stdin --branch-tag "${CI_COMMIT_BRANCH}"
```

It needs a full clone (not shallow) and a clean folder; the flags are in [docs/cli.md](docs/cli.md).

### 3. Mount it

```yaml
spec:
  containers:
    - name: shop
      volumeMounts:
        - { name: settings, mountPath: /etc/shop, readOnly: true, subPath: configmaps/shop-settings }
        - { name: settings, mountPath: /etc/shop-raw, readOnly: true, subPath: raw }
  volumes:
    - name: settings
      image:
        reference: ghcr.io/acme/shop-settings@sha256:80b52437… # the digest the run printed
```

Always by digest: the new digest goes into the Deployment like any image bump (by a PR, or by Flux
image automation following the branch tag), and `kubectl rollout undo` rolls the settings back.

## Docs

[docs/](docs/README.md): the plan, the image format (with known-answer digests), the CLI, the GitHub
Action, the alternatives, and the measurements behind each rule.

## Develop

Open the repository in its devcontainer. Everything is a [Task](https://taskfile.dev):

```bash
task test     # unit tests
task lint     # Go, go.mod, Dockerfiles, workflows, markdown
task build    # bin/settings-image, with its version baked in
task test-mount  # a k3d cluster: mount the examples as image volumes and check every file
```

[examples/](examples/) has settings folders to try: ConfigMaps, custom resources, pictures, and a
SOPS-encrypted Secret.

## License

[Apache 2.0](LICENSE).
