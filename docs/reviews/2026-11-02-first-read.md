# Review: first read of the design (2026-11-02)

_Questions raised by a first read of [plan.md](../plan.md), [format-v1.md](../format-v1.md),
[cli.md](../cli.md) and [github-action.md](../github-action.md), before any code existed. The
maintainer asked for a working prototype first. So each question has a **decision for now**, taken
to get there, and any of them can be revisited. Where a decision changes a design page, that page
was updated too._

## Design questions

### 1. Build from the working tree or from Git?

`git status --porcelain -- <folder>` does not list ignored files. So a local `.env`, `*.swp` or
`.DS_Store` under the folder goes into `objects/`, and a laptop build of a commit can differ from
CI's build of the same commit. The alternatives are building from `git ls-files` or the commit's
tree, or also refusing ignored files.

**Decision (maintainer):** pack whatever is in the folder. Weird content in the folder is included.
Revisit together with the Flux parity question of phase 3.

### 2. The Action's default credentials override `docker login`

`password` defaults to `github.token`, and the Action passes it as `SETTINGS_IMAGE_PASSWORD`, which
the CLI prefers over the Docker config. A user who runs `docker/login-action` for another registry
would send a GitHub token there.

**Decision:** the Action passes the `username`/`password` pair only when `password` is not empty.
Its default stays `github.token`. A user who logs in with `docker/login-action` sets
`password: ''`. Documented in [github-action.md §2](../github-action.md#2-actionyml-at-the-repository-root).

### 3. `changed` when there are no tags

"Every tag already pointed at this digest" is vacuously true with no tags, so a first push would
say `changed: false`.

**Decision:** `changed` is `false` only when the image manifest already existed in the repository
**and** every tag already pointed at it.

### 4. `pull_request` runs

On a pull request, `github.ref_name` is `<n>/merge` and the checkout is a synthetic merge commit
that never exists upstream, so provenance names a commit nobody can find.

**Decision:** not handled in the prototype. The documented workflow triggers on `push` only.

### 5. The referrer depends on more than the commit

The blob also holds `ref` and `repository`. The same commit built from two branches attaches two
referrers.

**Decision:** keep `ref`. format-v1.md §7 now says the blob depends on the commit, the ref, the
repository and the path.

### 6. Provenance in the conformance corpus

The CLI reads commit metadata only from `git`, so a committed corpus case cannot reproduce a
`sha-` tag or a referrer without a real repository.

**Decision:** the corpus (phase 2e) is not part of the prototype. Known answers are golden tests
that call `pkg/settingsimage` directly, with a fixed `Provenance` value.

### 7. The `pkg/settingsimage` API

**Decision:** a small API, revisited once a second importer exists:

- `Render(fsys fs.FS, opts RenderOptions) (Tree, error)` and
  `RenderArchive(r io.Reader, root string, opts RenderOptions) (Tree, error)`;
- `Build(ctx, Tree, BuildOptions) (*Image, error)`, which writes the layer, config and manifest
  into an in-memory store and returns their descriptors;
- `(*Image).AttachProvenance(ctx, Provenance) error`;
- `(*Image).Push(ctx, oras.Target, tags PushTags) (PushResult, error)`, with any `oras.Target`, so
  the CLI's `--oci-layout` (oras-go `oci.Store`) and a remote repository go through the same code;
- `NewRepository(ref, RepositoryOptions)` for the remote: retry client, `SkipReferrersGC`,
  credentials;
- `BranchTag(string) string` and `ValidateTag(string) error`;
- `*RefusalError{Path, Reason}`, matched with `errors.As`.

## Gaps in the format

| # | Question | Decision for now |
|---|---|---|
| 8 | What does the 10 MiB limit count? | The input files, summed. `files/` is derived and not counted |
| 9 | What is a ConfigMap? | `apiVersion: v1` and `kind: ConfigMap`. `metadata.namespace` is ignored, so two ConfigMaps of one name collide and are refused |
| 10 | A non-string `data` value, invalid `binaryData` base64 | Refused, as the apiserver would |
| 10 | `kind: List` / `ConfigMapList` | Not expanded. Its items go to `objects/` only (maintainer) |
| 11 | Two archive entries for one path | Refused |
| 11 | A `null` document, a comments-only file | Skipped; the file still goes to `objects/` |
| 11 | Duplicate keys in YAML | Whatever `NewYAMLOrJSONDecoder` does (the last one wins) |
| 11 | Unicode normalisation of paths | None. Paths are the bytes as given |
| 12 | Which tags move, and the push order | `sha-` first, then `--tag`s in the order given, then the branch tag. Only the branch tag is treated as moving |
| 13 | "Not a Git repository" without `--no-provenance` | Exit 3: a retry cannot fix it |
| 13 | `--no-provenance` | No Git at all: no `sha-` tag, the source comes from `--source` or the `GITHUB_*` variables only |

## Inconsistencies fixed in the docs

- format-v1.md's intro placed the conformance corpus in phase 1. It is phase 2e.
- format-v1.md §5 pointed at §8 for the `layout` annotation. It now points at plan.md phase 5.
- `go.mod` said `go 1.27.1`; the image and the known answers use 1.27.3. Raised to 1.27.3.
- `committedAt` must be UTC, while cli.md's `%cI` gives the committer's offset. The CLI converts it.
- The annotation is `ai.configbutler.settings-image.layout` while the media types are
  `vnd.configbutler.settings-image.…`. The known answers fix both, so they stay as they are.

## Process

- **Pushing:** not answered yet. The prototype was left uncommitted for the maintainer to review.
- **A pod mount:** the devcontainer has Docker, `crane` and `oras`, but no cluster. Mounting in a
  pod is skipped for the prototype and reported as skipped.

## Smaller decisions taken while building

- **Hard links on disk are not detected.** `fs.FS` does not expose a link count, and a hard link
  inside the folder cannot reach outside it. In an archive, a hard-link entry is refused.
- **The provenance `path` of a folder at the repository root is `.`**, not omitted.
- **The referrer's `subject` is the bare descriptor** (media type, digest, size). oras-go's
  `PackManifest` returns the image descriptor with the manifest's annotations attached; passing
  that as the subject gives a 945-byte referrer instead of format-v1.md's 748.
- **`--allow-dirty` always means no provenance**, also when the folder happens to be clean, as
  cli.md says ("implies").
- **The self-test reads the registry with `curl` and `jq`** instead of installing `crane` on the
  runner.

## What the prototype was observed to do (2026-11-02)

- Both known answers of format-v1.md §9 match on every byte, the provenance referrer included.
- Against `registry:2`: a first push reports `changed: true`, a second `changed: false`, and a
  commit to another folder changes nothing (same digest, `sha-` tag and referrer). `crane export`
  shows every file `0644`, a committed `0755` script included. `oras discover` lists the referrer,
  stored under the `sha256-…` fallback tag. A revert back to an older digest attached a second
  referrer without the `DELETE`/405 failure.
- Against zot: no fallback tag; the referrer is listed through the native API.
- `build --oci-layout` writes a layout that `oras manifest fetch --oci-layout` and
  `oras discover --oci-layout` read.
- The Action's push script, run locally with `RUNNER_TEMP` and `GITHUB_OUTPUT`, wrote the outputs
  and reported `changed=false` on its second run. **The Action has not run on GitHub yet**, and
  nothing was pushed to ghcr.io.

## Second round: KRM, SOPS and a cluster (2026-11-02)

- **KRM without CRDs (maintainer):** the build does not validate against schemas, and does not
  need to for now. A custom resource is carried byte for byte; only the fields that find
  ConfigMaps and Secrets are read. Validation against CRDs could be an optional lint later.
- **SOPS-encrypted files (format-v1.md §6; first called "sealed documents", see below) are built
  now**, ahead of phase 4's order, because the
  maintainer wants SOPS supported. They stay layout `v1`: admitting more input changes no byte of
  an image v1 already built. `--sensitive-kind` came with them. The `sops` and `age` CLIs are in
  the devcontainer's `ci` stage; age has no checksum file, so its sha256 is pinned in the
  Dockerfile.
- **The test key is committed** (`testdata/sops/age-test-key.txt`), marked as a test key, so
  anyone can decrypt the example and the fixtures.
- **A k3d cluster for mount tests**, modelled on gitops-reverser's `start-cluster.sh` without its
  audit webhook: `rancher/k3s:v1.37.1-k3s1`, one node, a k3d registry. `task test-mount` mounts the
  three examples and `testdata/mount/edge-cases` in one pod (uid 1000, read-only root filesystem),
  and checks 24 things. All passed on its first run. The mounts were `ro` but not `noexec`, which
  is why the forced `0644` matters: a committed `0755` script failed with `permission denied`.
- **The self-test passed on GitHub**, against the service registry and against ghcr.io, with the
  same digest on both. setup-go's cache never restored with `uses: ./` (a path ending in `/./`),
  which is fixed by resolving the action's path first.
- **release-please proposed 1.0.0.** Settled in the third round.

## Third round: aligned with SOPS and Sealed Secrets sources (2026-11-02)

The maintainer pointed out that "sealed" is Sealed Secrets' word, and asked for the wording to be
checked against both projects' sources (getsops/sops and bitnami-labs/sealed-secrets, cloned in
`external/`).

- **Wording.** Sealed Secrets owns "seal", "sealed" and "unseal" (`kubeseal`, the `SealedSecret`
  kind; "sealed" 98 times in its README). SOPS says "encrypt", "decrypt" and "encrypted file".
  Everything here now says **SOPS-encrypted**, and "sealed" only names Sealed Secrets.
- **A `SealedSecret` is an ordinary custom resource** in v1: carried byte for byte, not checked.
  Supporting it properly (say, requiring `spec.encryptedData`) is a possible later step; it is
  safe to publish by design, being encrypted for one cluster's controller.
- **The checks now follow SOPS's source, which changed the rules:**

| Topic | Before | Now | Source |
|---|---|---|---|
| A file with no rule (`sops encrypt` alone) | refused: SOPS writes `unencrypted_suffix: _unencrypted` | admitted, and checked | `stores/stores.go`, `DefaultUnencryptedSuffix` |
| `unencrypted_regex`, `encrypted_suffix`, `unencrypted_suffix` | refused | admitted: key-based, so checkable the same way | `Tree.shouldBeEncrypted` in `sops.go` |
| `encrypted_comment_regex`, `unencrypted_comment_regex` | refused | still refused: they depend on comments, which decoding drops | same |
| `mac_only_encrypted` | refused | still refused: the MAC leaves plaintext values unprotected | `Tree.Encrypt` |
| Two rules at once | the first was used | refused, as SOPS refuses it | `stores/stores.go` |
| `""` and `null` under an encrypted key | refused as plaintext | admitted: SOPS never encrypts them | `aes/cipher.go` `isEmpty`, `walkValue` |
| A multi-document file | checked per document | checked per file: the first document's `sops` block governs all | `stores/metadata.go` `ExtractMetadata` |
| The ciphertext format | prefix `ENC[AES256_GCM,` | SOPS's own regex, `data`, `iv`, `tag` and `type` included | `aes/cipher.go` `encre` |

- **Fixtures** for every rule were made with the `sops` CLI (`testdata/sops/`), and each decrypts
  with the test key. A comment-rule fixture shows why it stays refused: whether `token` is
  encrypted depends on a `# sops:enc` comment.
- **Pictures** (maintainer): `examples/pictures/` holds product pictures and an avatar, referred to
  by path from custom resources, and `task test-mount` checks them byte for byte in the pod.
- **Versions** (maintainer): like gitops-reverser, pre-1.0 releases bump the minor version
  (`bump-minor-pre-major`, already set). That only applies once a 0.x release exists, so
  `initial-version: 0.1.0` was added: the first release is 0.1.0, not 1.0.0.
