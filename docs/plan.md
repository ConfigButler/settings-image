# Plan

_What to build next, in order. A step is done when its result was **observed** (a push read back
with another tool, a mount in a cluster), not only when a test passes; see
[AGENTS.md](../AGENTS.md). Phases 3 and 5 are outlines, and each needs its own design page before
it is built._

## Decisions

- **The format is [format-v2.md](format-v2.md)**, and its known answers (§11) are the acceptance
  test. Change a byte only with a new layout version.
- **One public Go package, `pkg/settingsimage`**, for everything a producer needs: render, build,
  provenance, push. The CLI is a thin layer over it. Other projects import it, so its API is the
  one thing to design with care. `internal/` is the CLI's plumbing.
- **Dependencies:** `oras.land/oras-go/v2`, `github.com/opencontainers/image-spec`,
  `github.com/opencontainers/go-digest`, and `go.yaml.in/yaml/v3` (its nodes show duplicate keys).
  No client-go, no Git library: the CLI shells out to `git`.
- **Layout v2 says what each file is.** `resources/` holds Kubernetes resources, read and checked;
  `raw/` holds anything, never read. ConfigMaps also become files unless annotated; a Secret must be
  SOPS-encrypted. Only a closed list of known files (a README, a licence, Git and SOPS settings) may
  sit beside the two folders. No index file: a reader lists `resources/`.
- **The Action is composite** and builds the CLI from its own source, which is right for every
  ref, at 10–20 s.
- **The repository is public**, and changes are committed straight to `main`.

## Do not

- Put anything per-commit or per-run in the image: no time, no commit, no tool version. That breaks
  "same folder, same digest", and so every consumer's rollout.
- Change a byte of the format without a new layout version.
- Decrypt anything, hold a key, or log a credential or a settings value.
- Interpolate `${{ }}` into a `run:` script in a workflow or in `action.yml`; pass it through
  `env:`.

## Done

Each was observed as described; the commit messages have the details.

- **Phase 1, the format package** (2026-11-02): render (a directory or a tar archive), build,
  provenance, tags and push. Both known answers match; images read back with `crane` and `oras`
  from `registry:2` and zot (native referrers, no fallback tag).
- **2a–2c, the CLI, the Action and its self-test** (2026-11-02): `build` and `push` as
  [cli.md](cli.md); a composite `action.yml`; a self-test workflow against a service registry
  (every run) and ghcr.io (`main`).
- **2d, releases carry binaries** (v0.1.0, 2026-11-02): reproducible binaries for linux/amd64,
  linux/arm64 and darwin/arm64, a cosign-signed checksums file, SLSA provenance (from 2026-11-03,
  when the repository went public), immutable releases, and a `v0` tag that moves. A draft left by
  a failed run is finished with `workflow_dispatch` and `tag: vX.Y.Z`.
- **Phase 4, SOPS-encrypted Secrets** (2026-11-02): plaintext refused, ciphertext carried byte for
  byte; `task test-mount` decrypts the mounted file with the test key.
- **2f, known top-level files are skipped** (2026-11-03): mounted in k3d; an edit to the README
  alone gives `changed: false`.
- **2g, any registry** (2026-11-03): `--registry-token-stdin` and the Action's `registry-token` /
  `oidc-audience`, sent as Bearer to the registry's own origin only; exit 4 on 401/403;
  `github.token` to ghcr.io only. Pushed to zot with bearer auth, read back with `crane`; the
  Action's script run against zot with a fake OIDC endpoint.
- **2h, a timestamp tag that never moves** (2026-11-03): a re-run of an old commit moves `main`
  back, but the highest timestamp tag stays on the newest commit.
- **Hardening the package API** (2026-11-03): `Build(Tree)` checks every path itself (`"/a"` used
  to loop forever, `"../a"` went into the layer); a file is read only up to the size limit; an
  empty directory beside `resources/` and `raw/`, or an archive's directory entry at a file's path,
  is refused.
- **2i, provenance names no author by default** (2026-11-03): one image digest in all three modes;
  `oras pull` of the referrer shows no author, a name, or both.

## Next

1. **2e, the conformance corpus.** `testdata/conformance/<case>/` holds an `input/` folder, the
   output of `settings-image build --oci-layout`, and an `expected.json` (digest, tree, provenance
   or refusal). Cases: both known answers, one for every refusal, a nested tree, a `binaryData`
   ConfigMap, an annotated ConfigMap, a `raw/` file, and a folder with skipped top-level files. A
   Go test rebuilds every case and fails on any byte of drift; readers in other languages test
   against this directory at a release tag.
   - **Observe:** the test passes, and fails when one input byte is changed.
2. **Flux, in a cluster.** In k3d with Flux: two commits give two timestamp tags; image automation
   commits the newer digest into the Deployment and the pod mounts it; re-running the older
   workflow run changes nothing in the cluster or in Git. Then the README's Flux section loses its
   "not yet observed" note.
3. **2g, the parts not observed yet:** a real GitHub OIDC token from a workflow, against a zot that
   trusts GitHub's issuer; zot with a password file and bearer login on at once.
4. **The first release with no manual step** (the next release-please PR): `gh attestation verify`
   passes on a downloaded binary, and `gh release verify` confirms the release is immutable.
5. **A production example of consuming a SOPS-encrypted Secret:** who decrypts, and where the key
   lives.
6. **The Action downloads a released binary** with a `version` input, instead of building it
   ([github-action.md §3](github-action.md#3-where-the-binary-comes-from)). Only when the 10–20 s
   build matters.
7. **List the Action on the Marketplace** ([github-action.md §6](github-action.md#6-the-marketplace)):
   a manual step on the next release's draft; the release workflow waits for it when the `release`
   environment has a required reviewer.

**Phase 2 is done when** a workflow in another repository uses `ConfigButler/settings-image@v0`
and an image appears on a registry for each commit that touches its folder. Measure the
commit-to-image time; expect 30–60 s, mostly runner start-up.

## Later

### Phase 3: the controller (outline)

The same package, run continuously in a cluster: a custom resource that names a Flux
`GitRepository` and a folder, and publishes each fetched revision that changes the folder. That
takes seconds instead of a runner's minute, and pushes to an in-cluster registry. The design page
must answer:

- the API group and kind;
- registry credentials (Flux's `OCIRepository` auth fields are the model);
- commit metadata (a depth-1 fetch of its own, or the `GitRepository`'s);
- the hole it needs in Flux's NetworkPolicy, to download artifacts from source-controller;
- **digest parity with the CLI** for the same folder. Flux's source-controller drops some files by
  default (its ignore patterns), so a folder can differ between a checkout and a Flux artifact.

### Phase 5: readers (outline)

A reader library in another language pins a release of this repository and tests against
`testdata/conformance/`. A reader refuses an image whose `ai.configbutler.settings-image.layout`
it does not know. The first is a .NET library, in its own repository.

### Not planned yet

- A guard that refuses to move the branch tag to a commit that is not a descendant of the current
  one. It needs the current tag's provenance read back, and must handle reverts and force-pushes.
  The timestamp tag covers the need first.
