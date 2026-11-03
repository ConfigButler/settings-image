# Compare: one folder, every tool

[docs/alternatives.md](../docs/alternatives.md) explains why settings-image exists. This folder
**measures** it: [run.sh](run.sh) builds one settings folder with every tool that can put files in
a registry, and checks three things for each result:

1. **Is the digest stable?** Built again; built on "another machine"; with an executable bit.
2. **Is it an image or an artifact?** Only an image (a config with `rootfs.diff_ids`) can be
   unpacked by a container runtime.
3. **Can a non-root container read the files** when the result is mounted as an image? Most pods
   run as non-root; the `restricted` Pod Security Standard requires it.

```bash
task compare                            # this checkout's settings-image, on examples/custom-resources
task compare -- path/to/your/folder     # any folder with resources/ and raw/
compare/run.sh                          # the same, with whatever settings-image is on PATH
```

It starts a throwaway `registry:2.8.3` on port 5099 (`COMPARE_PORT`), takes about 30 seconds, and
cleans up after itself. It needs Docker 28 or later with the containerd image store
(`--mount type=image` and `rewrite-timestamp`), `crane` and `jq`. A tool that is not installed
(`settings-image`, `imgpkg`, `oras`, `flux`) gets a row that says so. The devcontainer's `dev`
stage has all of them.

## The three copies

The script copies `resources/` and `raw/` three times, so every tool gets exactly the same input:

| Copy | What it stands for |
|---|---|
| `a` | a normal checkout: `umask 022`, mtimes now |
| `b` | another machine or CI runner: `umask 077` (files `0600`, directories `0700`) and every mtime in 2030 |
| `c` | `a`, with one file in `raw/` made executable: a script committed with `+x` |

"`a`, built again" builds `a` a second time, at least one second later, without a build cache, as a
fresh CI runner would.

## Results

Measured 2026-11-03: settings-image v0.2.1, imgpkg v0.49.0, crane 0.22.1, oras 1.3.6, flux 2.9.6,
Docker 29.9.2 (BuildKit v0.34.0, buildx 0.38.1). The mount is Docker's `--mount type=image`, which
stands in for an image volume: **not** a kubelet. `task test-mount` covers settings-image on k3s.

| Tool | digest of a | a, built again | b: umask 077, mtime 2030 | c: chmod +x | Shape | uid 1000 reads b |
|---|---|---|---|---|---|---|
| settings-image push | `7f04bece856e` | same | same | same | image | yes |
| imgpkg push | `067422cd3402` | same | same | **differs** | image | no: Permission denied |
| imgpkg push --preserve-permissions | `bd5422c36b43` | same | **differs** | **differs** | image | no: Permission denied |
| crane append, plain tar -czf | (varies) | same | **differs** | **differs** | image | yes |
| crane append, deterministic tar | `bc49b6e902a2` | same | same | **differs** | image | yes |
| docker build, Dockerfile.default | (varies) | **differs** | **differs** | **differs** | image | no: Permission denied |
| docker buildx, Dockerfile.reproducible | `2a83b82ec3fe` | same | same | **differs** | image | yes |
| oras push | (varies) | **differs** | **differs** | **differs** | artifact (`vnd.oci.empty.v1+json`) | no: pull refused |
| flux push artifact | (varies) | **differs** | **differs** | **differs** | artifact (`vnd.cncf.flux.config.v1+json`) | no: mounts EMPTY, no error |

Only settings-image has **same, same, same, image, yes** in one row.

## What each row teaches

**settings-image.** Order, mtimes, owners and modes are pinned (`0644` and `0755`, whatever the
checkout had), so all three copies give one digest. The `+x` is dropped on purpose: nothing in a
settings image runs. The format is [docs/format-v2.md](../docs/format-v2.md).

**imgpkg** (Carvel, since 2020) is reproducible by default, the closest prior art. But it keeps only
the owner's bits (`mode & 0700`), so files are `0600` and directories `0700`: a non-root pod gets
**Permission denied**. `--preserve-permissions` makes copy `a` readable, but then the digest
follows the builder's umask, and copy `b` is unreadable again. It was built for `imgpkg pull`, where
the files land in the puller's own directory, not for mounting.

**crane append** builds a real image from any tarball, so the question is the tarball. A plain
`tar -czf` carries mtimes, owners and modes. With the right GNU tar flags it is reproducible:

```bash
tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner --mode='u=rwX,go=rX' \
  -cf - resources raw | gzip -n -9 > layer.tgz
crane append -f layer.tgz -t registry.example/settings:v1
```

Six flags, and forgetting one still gives an image that mounts: you find out only when two digests
differ. GNU tar headers also differ from settings-image's, so the digests never match each other.

**docker build, [Dockerfile.default](Dockerfile.default)**: the `FROM scratch` + `COPY` that most
image-volume guides show.

```dockerfile
FROM scratch
COPY resources /resources
COPY raw /raw
```

It mounts. But every clean build is a new digest (the config's `created`, the mtimes, and a
provenance attestation that records the build), and the files keep the checkout's permissions:
copy `b` mounts as **Permission denied** for a non-root pod, with no warning at build time. A
build-cache hit hides the first problem on a laptop; a fresh CI runner shows it.

**docker buildx, [Dockerfile.reproducible](Dockerfile.reproducible)**: BuildKit **can** do it, with
five things right:

```dockerfile
# syntax=docker/dockerfile:1.20
FROM scratch
COPY --chmod=u=rwX,go=rX resources /resources
COPY --chmod=u=rwX,go=rX raw /raw
```

```bash
SOURCE_DATE_EPOCH=0 docker buildx build --provenance=false \
  -f Dockerfile.reproducible \
  --output type=image,name=registry.example/settings:v1,push=true,rewrite-timestamp=true .
```

`SOURCE_DATE_EPOCH` and `rewrite-timestamp` alone are **not** enough: the timestamps are fixed, but
the modes still follow the builder's umask (we measured that first). The symbolic `--chmod` fixes
it, where an octal `--chmod=0644` also makes the directories `0644`: measured, Permission denied
for uid 1000 below the top directory. Not
tested: whether the digest is stable across BuildKit versions.

**oras push** and **flux push artifact** make **artifacts**, not images: no `rootfs.diff_ids`.
Docker refuses to pull the ORAS one. The Flux one is worse: it pulls, mounts, and the directory is
**empty**, with no error, exactly what containerd does in a pod
([alternatives.md §2](../docs/alternatives.md#2-what-each-tool-produces-and-what-mounts)). Both
write the push time into the manifest (`org.opencontainers.image.created`); Flux has a
`--reproducible` flag for that, which does not change the shape.

## What this shows

- **settings-image is the only tool that gets every column right by default**: a real image, the
  same digest for the same files, and files a non-root pod can read. No flags to remember.
- **The other image builders need you to get it right.** crane and BuildKit reach the same row only
  with every flag in place, and nothing tells you when one is missing. imgpkg is reproducible, but
  its files are unreadable to a non-root pod.
- **The tools that sound right for "files in a registry" make artifacts**: ORAS is refused, and
  Flux mounts an empty directory without a word.

On top of that row, settings-image checks the settings (stray files refused, a Secret must be
SOPS-encrypted, ConfigMaps become files) and records provenance. This script measures the image;
those are covered by `task test` and `task test-mount`.
