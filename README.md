# settings-image

Build a folder of settings into a small, reproducible OCI image, and mount it next to your
application with a Kubernetes [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).

> **Status: prototype.** `settings-image build` and `push` work and match the format's known
> answers, and a composite GitHub Action runs them. Releases do not carry binaries yet. See
> [docs/plan.md](docs/plan.md).

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
        reference: ghcr.io/acme/app-settings@sha256:c5edc2c3…   # by digest, always
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
  folder, a Kubernetes `Secret`.
- **Provenance:** the commit and its author, attached to the image as an OCI referrer.
- **A GitHub Action**, so a commit that changes the folder becomes a new image.

It understands Kubernetes objects (KRM) first: a `ConfigMap`'s keys also appear as plain files.

## Use

```bash
# Build without pushing: prints the digest, refuses bad input with exit code 3.
settings-image build configs/shop

# Push, with provenance from Git and a moving branch tag set last.
settings-image push configs/shop --repository ghcr.io/acme/shop-settings --branch-tag main
```

In a workflow (the Action builds the CLI from its own source; check out with `fetch-depth: 0`):

```yaml
- uses: ConfigButler/settings-image@main # @v0 once released
  with:
    path: configs/shop
    repository: ghcr.io/acme/shop-settings
```

The flags are in [docs/cli.md](docs/cli.md), the inputs in [docs/github-action.md](docs/github-action.md).

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

[examples/](examples/) has settings folders to try: ConfigMaps, custom resources, and a
SOPS-encrypted Secret.

## License

[Apache 2.0](LICENSE).
