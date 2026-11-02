# The settings image format, layout v1

_The contract between everything that writes a settings image and everything that reads one. A
writer in this repository and a reader in another language are held to the same bytes by
[§9's known answers](#9-known-answers) and, later, by the conformance corpus
([plan.md](plan.md) phase 2e)._

Any change to the bytes described here is a **new layout version**, with one exception: an
optional provenance field may be added (§7).

## 1. In one paragraph

A settings image is a real OCI **image** (not an artifact) with exactly one layer. The layer holds
the folder's files under `objects/`, and every `ConfigMap`'s keys as plain files under
`files/<configmap>/<key>`. Everything that changes per commit (who, which commit, when) stays
**out** of the image and rides beside it as an OCI **referrer**. So the same folder always gives
the same digest, whatever commit, machine or tool built it.

## 2. Input: the folder

The input is one folder, read as a set of `relative path → bytes`. Paths are slash-separated and
relative to the folder.

- **Included:** every regular file under the folder, at any depth.
- **Skipped:** directories (they are implied by the files), and any path with a component named
  `.git`.
- **Refused:** see §6. A refusal stops the build, and nothing is pushed.

Two inputs must give the same set: a directory on disk, and a tar (or tar+gzip) archive whose
entries sit under a root path (a Flux artifact is one). Archive entry names may start with `./`.

## 3. The tree: `objects/` and `files/`

Two views, both on by default. A writer may switch `files/` off.

| Path | Content |
|---|---|
| `objects/<rel>` | every input file, **byte for byte**. Nothing is re-serialised, so a sealed (SOPS) document keeps its MAC valid |
| `files/<name>/<key>` | for each `ConfigMap` document named `<name>`: one file per `data` key (the string's bytes), and one per `binaryData` key (base64-decoded) |

A `ConfigMap` is a document with `apiVersion: v1` and `kind: ConfigMap`; `metadata.namespace` is
ignored. A `kind: List` is not expanded. A `null` document is skipped.

Which files are parsed: those whose extension, lowercased, is `.yaml`, `.yml` or `.json`. Each is
read as a stream of YAML or JSON documents (Kubernetes' `NewYAMLOrJSONDecoder` semantics), and
each document's `kind`, `metadata.name`, `data` and `binaryData` are read. Every other file goes to
`objects/` unread.

`files/` rules: `<name>` and `<key>` must match `^[-._a-zA-Z0-9]+$` (the apiserver's own rule for
a ConfigMap key) and must not be `.` or `..`. Two documents writing the same `files/` path are
refused.

## 4. The layer

One uncompressed tar, then gzip. Built so that the same tree always gives the same bytes:

- **Entries:** every file of the tree, plus **every parent directory, explicitly**. A file
  `files/a/b` gives the entries `files`, `files/a` and `files/a/b`.
- **Order:** sorted by path, byte order, with directory names compared _without_ their trailing
  slash (Go's `sort.Strings` on the plain paths).
- **Headers** (Go's `archive/tar`): `Format: tar.FormatUSTAR`, falling back to `tar.FormatPAX` only
  for a path USTAR cannot encode. `ModTime` is the Unix epoch. Uid and gid are 0, with no user or
  group names.
  - A directory: `Typeflag: TypeDir`, `Name: "<path>/"`, `Mode: 0o755`.
  - A file: `Typeflag: TypeReg`, `Mode: 0o644`, `Size` set.
- **Why 0644 is forced:** an image volume is mounted read-only but **not** `noexec`. The forced
  mode is the only thing that stops a committed file from being executable in the pod.
- **gzip:** Go's `compress/gzip` at `gzip.BestCompression`, with an empty header (no name, no
  modification time). The level and the library are pinned: a different one gives different bytes,
  and so a new digest for every consumer.

The layer's **diffID** is the sha256 of the uncompressed tar. Its **digest** is the sha256 of the
gzip.

## 5. Config and manifest

**Config**, media type `application/vnd.oci.image.config.v1+json`, exactly these bytes (no
whitespace):

```json
{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":["<diffID>"]}}
```

This is the smallest config containerd will unpack. `rootfs.diff_ids` is the field that makes
this an image rather than an artifact. Without it, a kubelet refuses the image (`mismatched image
rootfs and manifest layers`) or, with Flux's config shape, mounts an empty directory.
`architecture: amd64` is a formality: containerd does no platform match on a manifest referenced
by digest, so one manifest serves arm64 nodes too (**verified**, spike P0).

**Manifest**: an OCI image manifest (`application/vnd.oci.image.manifest.v1+json`), packed as
oras-go v2's `PackManifest` with `PackManifestVersion1_1` and **no** `artifactType`. It has one
layer (`application/vnd.oci.image.layer.v1.tar+gzip`), and these annotations:

| Annotation | Value | Why |
|---|---|---|
| `org.opencontainers.image.created` | `1970-01-01T00:00:00Z` | oras-go stamps `time.Now()` when it is absent, and every build would then get a new digest |
| `ai.configbutler.settings-image.layout` | `v1` | how a reader knows what it holds ([plan.md](plan.md) phase 5) |
| `org.opencontainers.image.source` | the source repository's URL, credentials removed (`https://github.com/<owner>/<repo>`). Omitted when unknown | ghcr.io links the package to that repository through it |

**Nothing else goes in the manifest.** Every annotation is constant per source, so content
addressing holds.

## 6. Refusals

A refusal is permanent for its input: building the same bytes again cannot succeed. It names the
offending path and a reason. Every one of these is refused:

| Input | Why |
|---|---|
| a symlink or hard link | an image volume must not reach outside its folder |
| any other non-regular file | not settings |
| a path that escapes the folder or archive (`..`, absolute) | not settings |
| more than **10 MiB** in total, uncompressed, summed over the input files | settings are small; a large tree is something else |
| an **empty** result | publishing it would roll every consumer to "no settings" |
| a `.yaml`/`.yml`/`.json` file that does not parse | an unparseable file is where a Secret would hide |
| two archive entries for one path, or a path that is both a file and a directory | not a folder |
| a document of `kind: Secret` that is not sealed (below) | a registry is not a secret store |
| an unsafe `files/` name or key, or two documents writing one `files/` path | §3 |
| a `ConfigMap` `data` value that is not a string, or `binaryData` that is not base64 | the apiserver refuses it too |

Known limit of v1: a JSON or YAML file whose top level is not a mapping (a list, a scalar) does not
decode, and is refused. Plain non-KRM settings files with those extensions hit this. A later layout
may relax it.

**Sealed documents (SOPS).** A document with a top-level `sops:` key is admitted only when it is
sealed this way, and refused otherwise:

- `sops.encrypted_regex` is set, and every value under a key it matches, at any depth, is
  `ENC[AES256_GCM,…]`;
- at least one value is encrypted;
- `sops.mac` is present (`ENC[AES256_GCM,…]`);
- no other SOPS rule is set (`encrypted_suffix`, `unencrypted_suffix`, `unencrypted_regex`,
  `encrypted_comment_regex`, `unencrypted_comment_regex`, `mac_only_encrypted`). Each is refused
  by name: only `encrypted_regex` can be checked to have left nothing in plaintext.

A sealed `Secret` is admitted. A kind on a writer-supplied "sensitive kinds" list (`group/Kind`, or
`Kind` for the core group) that arrives unsealed is refused. A sealed document has no `files/`
view, and with `files/` on, a sealed `ConfigMap` is refused, because one value per file cannot
carry its MAC. The writer never decrypts anything: the document is carried byte for byte under
`objects/`, so it decrypts after the pull. Admitting sealed documents changes no byte of any image
that v1 already built, so it is still layout `v1`.

## 7. Provenance: a referrer, not part of the image

Per-commit facts live in an OCI referrer whose `subject` is the image manifest:

- **Manifest:** packed with oras-go `PackManifestVersion1_1`, with
  `artifactType: application/vnd.configbutler.settings-image.provenance.v1+json` and the empty
  config (`application/vnd.oci.empty.v1+json`, `{}`).
  - Its one layer has the media type `application/vnd.configbutler.settings-image.provenance.layer.v1+json`.
  - Its only annotation is `org.opencontainers.image.created: 1970-01-01T00:00:00Z`.
- **Blob:** compact JSON, **keys sorted**, no whitespace. Absent values are omitted, never written
  empty.

| Field | Meaning |
|---|---|
| `repository` | as `org.opencontainers.image.source` |
| `path` | the folder, relative to the repository root |
| `commit` | full SHA of the commit that **last changed the folder**, not HEAD |
| `ref` | the ref it was built from, e.g. `refs/heads/main` |
| `author` | `{"email": …, "name": …}` of that commit |
| `subject` | that commit's subject line |
| `committedAt` | that commit's committer date, RFC 3339, UTC |

A reader ignores fields it does not know, so a later writer can add one (the controller will add
the Flux artifact digest it built from) without a new layout.

Because the blob depends only on the commit, the ref, the repository and the path, a rebuild
attaches the **identical** referrer, not a second one. The same commit built from two branches
attaches two. A revert back to an older digest attaches a second referrer to that digest, so
`oras discover` lists both commits that produced it.

**On a registry without the referrers API** (ghcr.io; **verified**), oras-go stores referrers under
the fallback tag `sha256-<hex of the image digest>`. A writer must set oras-go's
`Repository.SkipReferrersGC = true`. Without it, attaching a second referrer makes oras-go delete
the superseded fallback index, and a registry that refuses `DELETE` (`registry:2` answers 405)
then fails the whole push, although the new index was already written.

## 8. Tags, and pushing in order

- `sha-<first 7 of the commit>`: the commit that last changed the folder. Immutable in practice.
- A **branch tag**: the branch name lowercased, every character outside `[a-z0-9_.-]` replaced by
  `-`, leading `.` and `-` removed, at most 128 characters, and `branch` if nothing is left. So
  `feature/Card` becomes `feature-card`.
- Any tag the caller names, which must already be a valid OCI tag
  (`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`). The writer refuses an invalid one rather than rewrite it.

Push order: the image, then its provenance referrer, then the tags, **moving tags last**. Whoever
follows a moving tag (Flux's `ImagePolicy`) then never sees a digest without its provenance.
Every step is idempotent: an existing blob is skipped, and a tag is overwritten with the same value.

**Consumers mount by digest, never by tag.** An image volume is resolved at each _container_
start. With a tag and `pullPolicy: Always`, a crash restart picks up new settings inside a running
pod, with no rollout and no record.

## 9. Known answers

Computed with the reference implementation (oras-go v2.6.4, image-spec v1.1.1, Go 1.27.3) on
2026-11-02. A writer that disagrees on any of these bytes is wrong, or has made a new layout
version.

**Case `configmap`.** The folder holds one file, `configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: shop-settings
data:
  settings.yaml: |
    maxScoops: 3
```

Built with both views, and `org.opencontainers.image.source: https://github.com/ConfigButler/k8s-trail`.

| | Value |
|---|---|
| tree | `files/shop-settings/settings.yaml` (`maxScoops: 3\n`), `objects/configmap.yaml` |
| diffID | `sha256:a754f5cb0b39e974c519c04b2bbb3663a2bad2c003f6cdd43aca29c199db9575` |
| layer | `sha256:d85cf97c694d719efd32a6d8b4b73d67ad464cb222cb747ee69e18bdbbbb7d4a`, 254 bytes |
| config | `sha256:f75f1a93569b199706fc140e4b1f735279e124d1bc56c013ef9ae10303fa0766`, 151 bytes |
| **manifest** | `sha256:c5edc2c3ef1c35d2a810d55d6ce161a30bf5a9e3337ff560a7e3ae6eabf6516d`, 598 bytes |

The manifest's exact bytes:

```json
{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:f75f1a93569b199706fc140e4b1f735279e124d1bc56c013ef9ae10303fa0766","size":151},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:d85cf97c694d719efd32a6d8b4b73d67ad464cb222cb747ee69e18bdbbbb7d4a","size":254}],"annotations":{"ai.configbutler.settings-image.layout":"v1","org.opencontainers.image.created":"1970-01-01T00:00:00Z","org.opencontainers.image.source":"https://github.com/ConfigButler/k8s-trail"}}
```

Its provenance, for commit `0123456789abcdef0123456789abcdef01234567` on `refs/heads/main` by
`Ada <ada@example.com>`, subject `feat: three scoops`, committed `2026-11-02T20:41:07Z`, with
`path: configs/shop`:

| | Value |
|---|---|
| blob | `sha256:ce56f1d41b61539fe3ee169c1c23ff7edf39a70cc81d948789cf55d7a351bdae`, 274 bytes |
| **referrer manifest** | `sha256:8962065a29e9443d5e12b28da691cd13c85799980b12fed477507b1bce20d682`, 748 bytes |

```json
{"author":{"email":"ada@example.com","name":"Ada"},"commit":"0123456789abcdef0123456789abcdef01234567","committedAt":"2026-11-02T20:41:07Z","path":"configs/shop","ref":"refs/heads/main","repository":"https://github.com/ConfigButler/k8s-trail","subject":"feat: three scoops"}
```

**Case `nested`.** The folder holds `order.yaml` and `sub/dir/notes.txt` (`plain text\n`), with
the same source annotation:

```yaml
apiVersion: shop.configbutler.ai/v1
kind: IceCreamOrder
metadata:
  name: vanilla
spec:
  flavour: vanilla
```

| | Value |
|---|---|
| tree | `objects/order.yaml`, `objects/sub/dir/notes.txt` (no `files/`: there is no ConfigMap) |
| diffID | `sha256:3be4c2cddd800adbbde12d51f6c8edb8736e3560209faf8f605d5ae11be1a0c5` |
| layer | `sha256:b05f2fb88911ea5f7e96824ad041f59f28c9eb7183c7efda95303fa2832436c3`, 266 bytes |
| config | `sha256:c6ddd5af0df19340fac591cf1abff9b4073c1220ac5afeda16b12959d3449bc2`, 151 bytes |
| **manifest** | `sha256:68b9a5c104a4bbdf9ffcc3762f0aa3a63c9519766d48430ce3e53d157fa694db`, 598 bytes |

Every file content above ends with a single newline, and has no trailing spaces.
