# Plan

_What to build, in order. Each step ends in something **observed**, not only in a passing test
(see [AGENTS.md](../AGENTS.md)). Phases 1 and 2 are specified in full. Phases 3–5 are outlines,
and each needs its own design page before it is built._

The goal of phases 1–2: **a commit that changes a settings folder becomes a new, mountable
settings image on a registry**, through a GitHub Action. That is enough for a talk or a first
user. Nothing has to run in a cluster.

## Decisions already made

- **The format is [format-v2.md](format-v2.md)**, and its known answers (§11) are the acceptance
  test. v1's were computed with a private reference implementation; v2's with this repository's
  writer, checked against a registry with `crane`. Change a byte only with a new layout version.
- **One public Go package, `pkg/settingsimage`**, for everything a producer needs: render, build,
  provenance, push. The CLI is a thin layer over it. Other projects will import it, so its API is
  the one thing to design with care. Keep `internal/` for the CLI's plumbing.
- **Dependencies:** `oras.land/oras-go/v2` (pin v2.6.4 first; the known answers were computed
  with it), `github.com/opencontainers/image-spec`, `github.com/opencontainers/go-digest`, and
  `go.yaml.in/yaml/v3` to parse `resources/` (its nodes show duplicate keys). No client-go, no
  Git library.
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

1. **Render.** Turn a folder into a tree (`map[string][]byte`) with every refusal of
   [format-v2.md §2–§5, §8](format-v2.md#2-input-the-folder). Two entry points: `fs.FS` (a
   directory) and an archive reader (tar or tar+gzip, plus a root path, for a Flux artifact later).
   A refusal is a typed error carrying a path and a reason, testable with `errors.As`.
   - **Observe:** a test per refusal. A test that one folder, given as a directory and as an
     archive with shuffled entries, odd modes and mtimes, a `./` prefix and files outside the root,
     renders to the same tree.
2. **Build.** The tree becomes a layer, a config and a manifest in an oras-go content store
   ([format-v2.md §6, §7](format-v2.md#6-the-layer)).
   - **Observe:** both known-answer cases ([§11](format-v2.md#11-known-answers)) as golden tests on
     the diffID, the layer, the config, and the manifest's **exact bytes**. Also: every file is
     `0644` in the layer, and the manifest carries exactly the three annotations.
3. **Provenance.** The referrer of [format-v2.md §9](format-v2.md#9-provenance-a-referrer-not-part-of-the-image).
   - **Observe:** the known-answer referrer digest; and an empty field is omitted, not written as
     `""`.
4. **Tags and push.**
   - The branch-tag sanitiser, and the tag validator.
   - `Push` in the order of [format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order).
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
4. **2d, releases carry binaries** (done with v0.1.0, 2026-11-02).
   - Release-please cuts a **draft**; `publish-cli` runs `task dist` in the CI image
     (`linux/amd64`, `linux/arm64`, `darwin/arm64`, reproducible, plus
     `settings-image.sha256sums`), signs the checksums with cosign, attests SLSA provenance, and
     attaches it all; `publish-release` publishes the draft; `move-major-tag` moves `v0`. As in
     gitops-reverser, `release-pr.yml` refreshes the release PR without waiting for CI.
   - Not yet: the Action's `version` input that downloads the binary instead of building it
     ([github-action.md §3](github-action.md#3-where-the-binary-comes-from)). It still builds
     from its own source, which is right for every ref, at 10–20 s.
   - **Observed** on v0.1.0: the assets are on the release, `sha256sum -c` and `cosign
     verify-blob` pass on a downloaded binary, it prints `version=0.1.0`, and `v0` resolves to the
     release commit.
   - **SLSA attestations need a public repository** on the organisation's plan. The first v0.1.0
     run failed on exactly that while the repository was private, and left an empty draft;
     `workflow_dispatch` with `tag: v0.1.0` finished it, without attestations. That is the
     recovery for any draft a failed run leaves behind. Since the repository went public
     (2026-11-03) `publish-cli` always attests, so v0.1.0 is the only release without provenance.
   - **Immutable releases are enabled** (2026-11-03): a published release's assets and tag are
     locked, so a broken release is fixed by a new patch release, never in place. The workflow
     already attaches everything to the draft before publishing; `v0` is a plain tag, not a
     release, so it still moves.
   - **Next to observe:** the first release on the normal path, with no manual step (0.1.1,
     release PR #4): `gh attestation verify` passes on a downloaded binary, and `gh release
     verify` confirms the release is immutable.
5. **2e, the conformance corpus.** `testdata/conformance/<case>/` holds an `input/` folder, the
   output of `settings-image build --oci-layout`, and an `expected.json` (digest, tree, provenance
   or refusal). Required cases:
   - both known answers;
   - one for every refusal;
   - a nested tree, a `binaryData` ConfigMap, an annotated ConfigMap, and a `raw/` file.

   A Go test rebuilds every case and fails on any byte of drift. Readers in other languages test
   against this directory at a release tag.
   - **Observe:** the test passes, and fails when one input byte is changed.
6. **2f, known top-level files are skipped, not refused** (designed 2026-11-03,
   [format-v2.md §2](format-v2.md#2-input-the-folder)). Only `resources/` and `raw/` reach the
   image. A file beside them is skipped and named (a warning, `skipped` in the JSON) **only if it
   is on a closed list**: `.gittargetignore`, `.sops.yaml`, `.gitignore`, `.gitattributes`,
   `.gitkeep`, `.keep`, `*.md`, `*.markdown`, and `LICENSE`, `LICENCE`, `COPYING`, `NOTICE` (each
   also with `.txt`). That is gitops-reverser's own list of files it tolerates without managing them, plus its
   `.gittargetignore`. Any other top-level file or directory is still refused: a resource written
   one level too high (`coffee/menu.yaml` beside `resources/`) must stop the build, not publish an
   image without it. The dirty check and the last-changed commit look only at `resources/` and
   `raw/` ([cli.md §3](cli.md#3-git-where-provenance-and-the--tag-come-from)).
   - A refusal test becomes a skip test for each listed name; a test that an unlisted top-level
     file is still refused; a conformance case with a reverser-shaped folder.
   - **Built and observed** (2026-11-03). Each listed name has a skip test that compares the digest
     with the folder without it, and unlisted names (`menu.yaml`, `README.txt`, `README.MD`,
     `docs/README.md`, `resource/…`) are still refused. The example
     [examples/gitops-reverser/](../examples/gitops-reverser/) has a `.gittargetignore` and a
     `README.md` beside `resources/`; `task test-mount` mounts it and sees only `configmaps/` and
     `resources/`. Pushed from a scratch repository to the k3d registry, an uncommitted edit to the
     README and then a commit of it each gave `changed: false`, the same digest and the same
     commit; `crane export` lists no README. The conformance case waits for the corpus (2e).
7. **2g, any registry** (designed 2026-11-03, [cli.md §4](cli.md#4-registry-credentials),
   [github-action.md §2](github-action.md#2-actionyml-at-the-repository-root)). Generic credential
   forms, nothing per registry:
   - `--registry-token-stdin`, and the Action's `registry-token` and `oidc-audience` inputs. **A
     registry token is sent as `Authorization: Bearer` on every request to the registry's own host,
     from the first request on, whatever challenge the registry would answer with**, and never to
     any other host: a blob request may be redirected to storage, and the token must not follow.
     This is what `crane` does (go-containerregistry sends a registry token as Bearer even to a
     `Basic` challenge); oras-go alone only sends a token to a `Bearer` challenge, so the CLI adds
     the header itself, in a transport scoped to the registry host.
   - `oidc-audience` fetches the token in the push step, after the Go build, right before the
     push: the token lives minutes. The documented convention is **the registry host as the
     audience** (as Flux's RFC-0014 defaults it).
   - `github.token` is sent to ghcr.io only.
   - **A refused credential exits 4**, not 1: a 401 or 403 never fixes itself on a retry, so a
     person and the Action can tell misconfiguration from a network blip.
   - The Action gains the outputs `commit` and `skipped`.
   - **Observe:** a push to a local zot that has both a password file and bearer (OIDC) login on,
     with a bearer token, read back with `crane`; the token is not sent after a redirect to another
     host (a test registry that redirects blobs and logs headers); a wrong token exits 4; a
     workflow without `id-token: write` fails with its message; a push to a non-ghcr.io registry
     sends no `GITHUB_TOKEN`.
8. **2h, a sortable tag, and Flux image automation** (designed 2026-11-03,
   [format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order), [flux.md](flux.md)).
   `--timestamp-tag <branch>` (Action input `timestamp-tag`) adds
   `<branch>-<unix committedAt>-<sha7>`, from the commit that last changed the folder, so it is as
   reproducible as the `sha-` tag and never moves. Flux's `ImagePolicy` picks the highest with
   `numerical`.
   - Why: re-running an old workflow run ("Re-run jobs") rebuilds the old commit and moves the
     branch tag **back** to its digest; anything following the branch tag silently rolls back. The
     `concurrency` group orders new runs, not re-runs. With the timestamp tag, a re-run only
     re-pushes a tag that already exists, and the highest still wins.
   - Not now: a guard that refuses to move the branch tag to a commit that is not a descendant of
     the current one. It needs the current tag's provenance read back, and must handle reverts and
     force-pushes.
   - **Observe** in a k3d cluster with Flux: two commits give two timestamp tags; Flux's image
     automation commits the newer digest into the Deployment and the pod mounts it; re-running the
     older workflow run changes nothing in the cluster or in Git.
   - When observed: README gains a "Roll out with Flux" section, pointing to [flux.md](flux.md).
9. **2i, provenance names no author by default** (designed 2026-11-03,
   [format-v2.md §9](format-v2.md#9-provenance-a-referrer-not-part-of-the-image)). A referrer is
   public wherever the image is, so it must not copy a commit author's email out of a repository,
   least of all a private one. `--provenance-author none|name|full` (Action `provenance-author`),
   default `none`. It changes the referrer's bytes only, never the image digest.
   - Recompute the referrer known answer (§11) for the default; keep the `full` one as a second
     case.
   - **Observe:** a pushed referrer, read with `oras discover` and `oras pull`, has no `author`
     by default, `{"name": …}` with `name`, and both fields with `full`.
   - **Built and observed** (2026-11-03). The default's known answer is referrer
     `sha256:04f47a28…`, blob `sha256:301f5aef…` (224 bytes, checked with `sha256sum`); `full`
     keeps `sha256:1ba0abbf…`. Pushed to the k3d registry from a scratch repository in each mode:
     one image digest for all three, and `oras discover` + `oras pull` read back no `author`,
     `{"name":"Ada"}`, and `{"email":…,"name":"Ada"}`.

**Done when** a workflow in another repository uses `ConfigButler/settings-image@v0` and an image
appears on a registry for each commit that touches its folder. That repository's maintainer adds
that workflow, not this repository. Measure the commit-to-image time; expect 30–60 s, mostly
runner start-up.

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
[format-v2.md §4](format-v2.md#secrets). Built ahead of order on 2026-11-02, with the example in
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

- **The repository is public** (since 2026-11-03, after v0.1.0). The Action can be used from any
  repository; listing it on the Marketplace ([github-action.md §6](github-action.md#6-the-marketplace))
  is a manual step on a release page, still to do.
- **Changes are committed straight to `main`** ([AGENTS.md](../AGENTS.md)).
- **Layout v2: say what each file is** ([review nr2](reviews/2026-11-02-nr2.md)). A settings folder
  has `resources/` (Kubernetes resources, read and checked) and `raw/` (anything, never read).
  ConfigMaps become files unless annotated; Secrets are resources and must be SOPS-encrypted with
  their payload as ciphertext. No index file: a reader lists `resources/`. v1 had no users and its
  writer is gone. Schema validation is not for now.
