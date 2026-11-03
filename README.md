# settings-image

Build a folder of settings into a small, reproducible OCI image, and mount it next to your
application with a Kubernetes [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).

```text
configs/shop/                                    /etc/shop/ in the pod, read-only
  resources/app.yaml  (a ConfigMap)   ──push──▶    configmaps/shop-settings/settings.yaml
  raw/pricing.toml                                 raw/pricing.toml
                                                   resources/app.yaml
```

- **The settings that run are declared in the pod spec, by digest.** A change is an ordinary
  rolling update, and `kubectl rollout undo` rolls the settings back.
- **The same files give the same digest**, on a laptop or in CI, so a commit that touches other
  files rolls no pods. (The source repository URL is recorded in the image too: build with the same
  `--source`, or none, to compare.)
- **Nothing in the image executes**: no base image, no shell, just your files. Nothing is applied
  to the cluster either: a Deployment or a Secret in the folder stays a file in the image.

> **Status: v0.1.** The CLI and the GitHub Action work and are used in this repository's own
> tests; the format may still change before v1. See [docs/plan.md](docs/plan.md).

## What you need to mount it in Kubernetes

- **Kubernetes 1.35 or later**, where image volumes are on by default (stable in 1.36). On 1.33 and
  1.34 they are beta: turn on the `ImageVolume` feature gate on the API server and every kubelet.
- **containerd 2.1 or later, or CRI-O**, on the nodes.
- A registry the nodes can pull from. A private image needs `imagePullSecrets` in the pod, as
  any image does. On ghcr.io, a package pushed from a private repository **starts private**.

## The rules, in short

settings-image is opinionated. Each rule has a reason, and each is checked before anything is
pushed: a broken folder exits 3 and names the file.

1. **Two folders.** `resources/` holds Kubernetes objects (YAML or JSON with `apiVersion`, `kind`
   and `metadata.name`). `raw/` holds anything else, copied byte for byte and never read. Your
   application's own YAML goes in `raw/`. Resources are **packaged, never applied**: your
   application reads them; settings-image never talks to Kubernetes.
2. **A ConfigMap's keys also become files**, at `configmaps/<name>/<key>`, so a container can
   mount them like a `configMap` volume. Annotate it `settings-image.configbutler.ai/files: "false"`
   to keep it a resource only.
3. **A `Secret` must be SOPS-encrypted.** A registry is not a secret store. It **stays encrypted**
   in the image and in the mounted file: settings-image never decrypts and never holds a key.
   Whatever reads it decrypts it with [SOPS](https://getsops.io) and a key you deliver separately,
   for example your application, or an init container that has the age key from an ordinary
   Kubernetes Secret ([examples/](examples/README.md#encrypting-a-secret-with-sops) shows the
   decrypt).
4. **Nothing else at the top of the folder**, except well-known files such as `README.md`,
   `LICENSE` or `.gitignore`, which are skipped. Anything else stops the build: a resource one
   level too high should not silently go missing.
5. **The folder must be committed, in a full clone.** The image gets a `sha-<commit>` tag and a
   provenance record of the commit that last changed the folder. Outside Git, or to try things,
   add `--no-provenance`.

Every file in the image is `0644`, owned by root, with a fixed timestamp. The complete contract is
[docs/format-v2.md](docs/format-v2.md).

## Try it locally

Install the CLI from a [release](https://github.com/ConfigButler/settings-image/releases). The
binaries are `settings-image_linux_amd64`, `settings-image_linux_arm64` and
`settings-image_darwin_arm64`; set `asset` to yours. On Linux amd64:

```bash
VERSION=v0.1.0
asset=settings-image_linux_amd64
base="https://github.com/ConfigButler/settings-image/releases/download/${VERSION}"
curl -fsSLO "${base}/${asset}"
curl -fsSL "${base}/settings-image.sha256sums" | sha256sum -c --ignore-missing   # macOS: shasum -a 256 -c --ignore-missing
mkdir -p ~/.local/bin && install -m 0755 "${asset}" ~/.local/bin/settings-image  # or sudo install … /usr/local/bin/
```

Or, with Go: `go install github.com/ConfigButler/settings-image/cmd/settings-image@latest`.

Build an example. This needs nothing but the folder, and prints the image's digest:

```bash
git clone https://github.com/ConfigButler/settings-image && cd settings-image
settings-image build examples/configmap --no-provenance
# sha256:e788a6fe…
```

Then push it to a throwaway registry and look at what a pod would see. This needs Docker, and
[crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane#installation) to read
the image back:

```bash
docker run -d --name registry -p 5000:5000 registry:2

reference=$(settings-image push examples/configmap \
  --repository localhost:5000/shop-settings --plain-http --no-provenance)
echo "${reference}"
# localhost:5000/shop-settings@sha256:e788a6fe…

crane export --insecure "${reference}" - | tar t
# configmaps
# configmaps/branding
# configmaps/branding/colour
# configmaps/branding/logo.png
# configmaps/shop-settings
# configmaps/shop-settings/settings.yaml
# configmaps/shop-settings/welcome.txt
# resources
# resources/app.yaml
```

[examples/](examples/) has more folders to try: custom resources, pictures, and a SOPS-encrypted
Secret. `docker rm -f registry` cleans up.

## Kubernetes-aware, not Kubernetes-only

settings-image is a **settings publisher**, not a general folder-to-image tool. It knows
Kubernetes objects: it checks what is in `resources/`, turns ConfigMaps into files, insists that a
Secret is encrypted, and refuses a folder that looks wrong (more than 10 MiB, an unknown file
beside the two folders). For an arbitrary pile of files, a `FROM scratch` Dockerfile is the
simpler tool; a folder with only `raw/` skips every Kubernetes check.

**Kubernetes is not required to read the result.** It is a plain OCI image, so anything that pulls
images can get the files out. With the `reference` from above:

```bash
mkdir -p settings && crane export --insecure "${reference}" - | tar x -C settings   # any machine
docker pull "${reference}" && docker run --rm \
  --mount "type=image,source=${reference},target=/settings" busybox:1.36.1 ls -R /settings
```

(`--mount type=image` is experimental in Docker, and needs the image pulled first; checked on
Docker 29.2.) The image has no entrypoint and nothing in it runs, so it is only ever a source of
files.

## Mount it

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: shop
spec:
  containers:
    - name: shop
      image: registry.example.com/acme/shop:1.4.2
      volumeMounts:
        # The whole image.
        - { name: settings, mountPath: /settings, readOnly: true }
        # Or one ConfigMap's files, as a configMap volume would give them.
        - { name: settings, mountPath: /etc/shop, readOnly: true, subPath: configmaps/shop-settings }
  volumes:
    - name: settings
      image:
        reference: ghcr.io/acme/shop-settings@sha256:e788a6fe… # always by digest
        pullPolicy: IfNotPresent
```

**Mount by digest, never by tag.** A tag is re-resolved at every container start, so a crash
restart can pick up new settings inside a running pod, and two replicas can disagree. The new
digest goes into the Deployment like any image bump: by a pull request, by your deploy tool, or by
Flux image automation ([docs/flux.md](docs/flux.md)). `subPath` must name a directory, not a file.

Your application should **fail on a missing or invalid settings file instead of starting with
defaults**: a pod that stays un-ready stops the rolling update, and the old pods keep serving.

## Publish from CI

### GitHub Actions

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
          fetch-depth: 0 # a full clone: provenance names the commit that last changed the folder
      - id: settings
        uses: ConfigButler/settings-image@v0 # or pin a release SHA
        with:
          path: configs/shop
          repository: ghcr.io/${{ github.repository_owner }}/shop-settings
      - run: echo "Mount ${{ steps.settings.outputs.reference }}" >> "$GITHUB_STEP_SUMMARY"
```

Each image gets a `sha-<commit>` tag and a branch tag (`main`). A run that changes nothing reports
`changed: false`. Other registries (a username and password, a bearer token, the GitHub OIDC token,
or whatever `docker/login-action` left), and every input and output:
[docs/github-action.md](docs/github-action.md).

### Any other CI

```bash
echo "${REGISTRY_PASSWORD}" | settings-image push configs/shop \
  --repository registry.example.com/acme/shop-settings \
  --username ci --password-stdin --branch-tag "${CI_COMMIT_BRANCH}"
```

`--registry-token-stdin` sends a bearer token instead, and without either the Docker config is
used, as `docker login` left it. All flags, the `--json` output and the exit codes:
[docs/cli.md](docs/cli.md).

## When it refuses

| Message (abridged) | What to do |
|---|---|
| `not inside a Git work tree (use --no-provenance)` | run it in a Git checkout, or add `--no-provenance` |
| `the folder has uncommitted changes` | commit first, or add `--allow-dirty` (no provenance, no `sha-` tag) |
| `a shallow clone cannot tell which commit last changed …` | `fetch-depth: 0` in `actions/checkout`, or `--allow-shallow` |
| `app.yaml: not under resources/ or raw/` | move it into `resources/` (a Kubernetes object) or `raw/` (anything else) |
| `resources/app.yaml: a resource without metadata` | it is not a Kubernetes object: move it to `raw/` |
| `Secret s is not SOPS-encrypted` | encrypt it with SOPS, with its payload as ciphertext ([examples/sops-secret/](examples/sops-secret/)) |
| `the folder is empty` | put at least one file under `resources/` or `raw/` |
| `registry … refused the credential` (exit 4) | a wrong or expired credential, or no push permission; retrying will not help |

Exit codes: 0 done, 1 a retry may help (network, registry), 2 usage, 3 refused, 4 credential
refused.

## Why a tool, and not `docker build` or `oras push`

Because the volume has to be a **real image**: an image config whose `rootfs.diff_ids` lists its
layers, so the container runtime can unpack it.

- A plain `oras push` is refused when the kubelet pulls it.
- A Flux artifact (`flux push artifact`) mounts as an **empty directory, with no error**.
- A `FROM scratch` Dockerfile does give an image, but a new digest on every build, no checks and no
  provenance.

The full comparison, with what was measured: [docs/alternatives.md](docs/alternatives.md).

## Docs

[docs/](docs/README.md): the CLI and the Action in full, rolling out with Flux, the image format,
and the measurements behind each rule.

## Develop

Open the repository in its devcontainer. Everything is a [Task](https://taskfile.dev):

```bash
task test        # unit tests
task lint        # Go, go.mod, Dockerfiles, workflows, markdown
task build       # bin/settings-image, with its version baked in
task test-mount  # a k3d cluster: mount the examples as image volumes and check every file
```

[AGENTS.md](AGENTS.md) has the definition of done and how changes reach `main`.

## License

[Apache 2.0](LICENSE).
