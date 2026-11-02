# Plan

_What to build, in order. Each step ends in something **observed**, not only in a passing test
(see [AGENTS.md](../AGENTS.md)). Phases 1 and 2 are specified in full. Phases 3–5 are outlines,
and each needs its own design page before it is built._

The goal of phases 1–2: **a commit that changes a settings folder becomes a new, mountable
settings image on a registry**, through a GitHub Action. That is enough for a talk or a first
user. Nothing has to run in a cluster.

## Decisions already made

- **The format is [format-v1.md](format-v1.md)**, and its known answers (§9) are the acceptance
  test. They were computed with a private reference implementation that this repository replaces.
  Match its bytes; do not improve on them.
- **One public Go package, `pkg/settingsimage`**, for everything a producer needs: render, build,
  provenance, push. The CLI is a thin layer over it. Other projects will import it, so its API is
  the one thing to design with care. Keep `internal/` for the CLI's plumbing.
- **Dependencies:** `oras.land/oras-go/v2` (pin v2.6.4 first; the known answers were computed
  with it), `github.com/opencontainers/image-spec`, `github.com/opencontainers/go-digest`, and
  `k8s.io/apimachinery` for its YAML-or-JSON stream decoder only. No client-go, no Git library.
- **The CLI shells out to `git`** ([cli.md §3](cli.md#3-git-where-provenance-and-the-sha--tag-come-from)).
- **The Action is composite**, and builds the CLI from its own source until releases carry binaries
  ([github-action.md §3](github-action.md#3-where-the-binary-comes-from)).

## Do not

- Put anything per-commit or per-run in the image: no time, no commit, no tool version. That breaks
  "same folder, same digest", and so every consumer's rollout.
- Change a byte of the format without a new layout version.
- Decrypt anything, hold a key, or log a credential or a settings value.
- Interpolate `${{ }}` into a `run:` script in a workflow or in `action.yml`; pass it through
  `env:`.

## Phase 1: the format package

1. **Render.** Turn a folder into a tree (`map[string][]byte`) with both views, and every refusal
   of [format-v1.md §2, §3, §6](format-v1.md#2-input-the-folder). Two entry points: `fs.FS` (a
   directory) and an archive reader (tar or tar+gzip, plus a root path, for a Flux artifact later).
   A refusal is a typed error carrying a path and a reason, testable with `errors.As`.
   - **Observe:** a test per refusal. A test that one folder, given as a directory and as an
     archive with shuffled entries, odd modes and mtimes, a `./` prefix and files outside the root,
     renders to the same tree.
2. **Build.** The tree becomes a layer, a config and a manifest in an oras-go content store
   ([format-v1.md §4, §5](format-v1.md#4-the-layer)).
   - **Observe:** both known-answer cases ([§9](format-v1.md#9-known-answers)) as golden tests on
     the diffID, the layer, the config, and the manifest's **exact bytes**. Also: every file is
     `0644` in the layer, and the manifest carries exactly the three annotations.
3. **Provenance.** The referrer of [format-v1.md §7](format-v1.md#7-provenance-a-referrer-not-part-of-the-image).
   - **Observe:** the known-answer referrer digest; and an empty field is omitted, not written as
     `""`.
4. **Tags and push.**
   - The branch-tag sanitiser, and the tag validator.
   - `Push` in the order of [format-v1.md §8](format-v1.md#8-tags-and-pushing-in-order).
   - A remote with oras-go's retry client and `SkipReferrersGC = true`.
   - Credentials from a static pair or the Docker config.
   - A distinct error for 401/403.
   - **Observe:** a unit test that the moving tag is set last (an in-memory target that records
     calls). Then live: push to a throwaway registry and read it back with the other tools.

     ```bash
     docker run -d --rm --name reg -p 5000:5000 registry:2
     # docker-outside-of-docker: the devcontainer reaches it as host.docker.internal
     crane manifest --insecure host.docker.internal:5000/selftest@<digest>
     crane export --insecure host.docker.internal:5000/selftest@<digest> - | tar tv
     oras discover --plain-http host.docker.internal:5000/selftest@<digest>
     ```

     Do the same against zot (`ghcr.io/project-zot/zot-linux-amd64`), which has the native
     referrers API: no fallback tag must appear there.

**Done when** `task test` and `task lint` pass, both known answers match, and the image read back
from both registries has the expected files, modes, annotations and referrer.

## Phase 2: the CLI and the GitHub Action (the first user)

1. **2a, the CLI:** `build` and `push`, exactly as [cli.md](cli.md).
   - **Observe** each of these, by hand, against the throwaway registry:
     - in a scratch Git repository, a push of a folder prints the reference;
     - a second push prints `changed: false` and the same digest;
     - a commit to _another_ folder, then a push, gives the same digest and the same `sha-` tag;
     - a dirty folder, a shallow clone (`git clone --depth 1 file://…`) and a `Secret` each exit
       `3` with the path named;
     - `build --oci-layout out/` writes a layout that `oras` can read
       (`oras manifest fetch --oci-layout out:<tag>`).
2. **2b, `action.yml`:** a composite action at the repository root, as
   [github-action.md §2–§3](github-action.md#2-actionyml-at-the-repository-root) (phase 2a there:
   built from source).
3. **2c, the self-test workflow:** [github-action.md §5](github-action.md#5-testing-the-action-in-this-repository),
   with `testdata/selftest/`.
   - **Observe:** a green run on a pull request (the service registry) and on `main` (ghcr.io).
     The second invocation in the job reports `changed: false`.
4. **2d, releases carry binaries.**
   - `release.yml` gains a job after release-please that builds `linux/amd64`, `linux/arm64` and
     `darwin/arm64` with `task build` (`GOOS`/`GOARCH`), writes `checksums.txt`, and attaches all of
     them to the release.
   - A second job moves the floating `v0` tag.
   - The Action's `version` input and its release-please marker, as
     [github-action.md §3–§4](github-action.md#3-where-the-binary-comes-from).
   - **Observe:** after a release, the self-test passes using the downloaded binary, and `@v0`
     resolves to the release commit.
5. **2e, the conformance corpus.** `testdata/conformance/<case>/` holds an `input/` folder, the
   output of `settings-image build --oci-layout`, and an `expected.json` (digest, tree, provenance
   or refusal). Required cases:
   - both known answers;
   - one for every refusal;
   - a nested tree, a `binaryData` ConfigMap, and a non-KRM file.

   A Go test rebuilds every case and fails on any byte of drift. Readers in other languages test
   against this directory at a release tag.
   - **Observe:** the test passes, and fails when one input byte is changed.

**Done when** a workflow in another repository uses `ConfigButler/settings-image@v0` and an image
appears on ghcr.io for each commit that touches its folder. The first one is planned in
`ConfigButler/k8s-trail` (`configs/card-reveal`). That repository's maintainer adds that workflow,
not this repository. Measure the commit-to-image time; expect 30–60 s, mostly runner start-up.

## Phase 3: the controller (outline)

The same package, run continuously in a cluster: a custom resource that names a Flux
`GitRepository` and a folder, and publishes each fetched revision that changes the folder. That
takes seconds instead of a runner's minute, and pushes to an in-cluster registry. It needs a design
page first. The questions to answer:

- the API group and kind;
- registry credentials (Flux's `OCIRepository` auth fields are the model);
- commit metadata (a depth-1 fetch of its own, or the `GitRepository`'s);
- the hole it needs in Flux's NetworkPolicy, to download artifacts from source-controller;
- **digest parity with the CLI** for the same folder. Flux's source-controller drops some files by
  default (its ignore patterns), so a folder can differ between a checkout and a Flux artifact.
  Find out which, and make the controller and the CLI agree, or document where they cannot.

## Phase 4: SOPS-encrypted files (prototype done)

Admit SOPS-encrypted documents and refuse plaintext where a value must be secret. The rules are in
[format-v1.md §6](format-v1.md#6-refusals). Built ahead of order on 2026-11-02, with the example in
[examples/sops-secret/](../examples/sops-secret/) and the test key in `testdata/sops/`; `task
test-mount` decrypts the mounted file.

- The checks are pure and need no SOPS library.
- The fixtures are made once with the `sops` and `age` CLIs and committed. Add both CLIs to the
  devcontainer's `ci` stage, pinned like the other tools.
- `--sensitive-kind group/Kind` (repeatable) on the CLI.
- **Observe:** a pulled SOPS-encrypted file decrypts with `sops decrypt` and the test key; that is what
  proves it was carried byte for byte.

## Phase 5: readers (outline)

A reader library in another language pins a release of this repository and tests against
`testdata/conformance/`. A reader refuses an image whose `ai.configbutler.settings-image.layout`
it does not know. The first is a .NET library. It lives in its own repository, and nothing about
it is built here.

## Decided by the maintainer (2026-11-02)

- **The repository goes public once the first version works** (phase 2). Until then the Action can
  only be used from inside the ConfigButler organisation, and the Marketplace waits.
- **Changes are committed straight to `main`** ([AGENTS.md](../AGENTS.md)).
- **v1 stays limited.** Settings are KRM first. The v1 parse refusal for `.yaml`/`.yml`/`.json`
  files that do not decode stays. Widening to other shapes is a later layout, if a user needs it.
