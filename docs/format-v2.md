# The settings image format, layout v2

_The contract between everything that writes a settings image and everything that reads one. A
writer in this repository and a reader in another language are held to the same bytes by
[§11's known answers](#11-known-answers) and, later, by the conformance corpus
([plan.md](plan.md) phase 2e)._

Any change to the bytes described here is a **new layout version**, with one exception: an
optional provenance field may be added (§9).

Layout v1 decided what a file was from its extension. It parsed every `.yaml`, `.yml` and `.json`
file as a Kubernetes resource, so an application's own YAML could be refused, and the Secret check
covered a boundary nobody could see. v2 lets the author say what a file is, by the folder it is in
([review nr2](reviews/2026-11-02-nr2.md)). v1 was never used outside this repository and has no
writer any more.

## 1. In one paragraph

A settings image is a real OCI **image** (not an artifact) with exactly one layer. The settings
folder has two folders: `resources/`, for Kubernetes resources, which are read and checked; and
`raw/`, for any other file, which is never read. Both go into the layer byte for byte, at the same
paths, and every `ConfigMap`'s keys also appear as plain files under `configmaps/<name>/<key>`.
Everything that changes per commit (who, which commit, when) stays **out** of the image and rides
beside it as an OCI **referrer**. So the same folder always gives the same digest, whatever commit,
machine or tool built it.

## 2. Input: the folder

```text
configs/shop/
  resources/                 Kubernetes resources: read, checked, ConfigMaps turned into files
    app.yaml
    database.secret.yaml     a SOPS-encrypted Secret
    orders.yaml              custom resources: no CRD needed
  raw/                       anything else: copied byte for byte, never read
    pricing.toml
    settings.yaml            any YAML, a list at the top level included
    products/vanilla.png
```

The input is one folder, read as a set of `relative path → bytes`. Paths are slash-separated and
relative to the folder.

| Folder | What goes in | What the writer does |
|---|---|---|
| `resources/` | Kubernetes resources: ConfigMaps, Secrets, custom resources | parses every file, refuses what is unsafe or ambiguous (§4), turns ConfigMaps into files (§5) |
| `raw/` | anything | copies it. **No content check at all**: not for Secrets, not for SOPS, not for syntax |

- **Included:** every regular file under `resources/` or `raw/`, at any depth. Either may be
  absent.
- **Skipped:** directories (they are implied by the files), any path with a component named
  `.git`, and **a known file at the top of the folder**, beside `resources/` and `raw/`. The list
  is closed:
  - `.gittargetignore`, `.sops.yaml`, `.gitignore`, `.gitattributes`, `.gitkeep`, `.keep`;
  - a name ending in `.md` or `.markdown`;
  - `LICENSE`, `LICENCE`, `COPYING`, `NOTICE`, each also with `.txt`.

  It is gitops-reverser's own list of files it tolerates without managing them, plus its
  `.gittargetignore`, so a folder the reverser writes into as `resources/` can keep its marker
  and README beside it. A skipped file is named in the writer's output (a warning, and `skipped`
  in the CLI's JSON).
- **Refused:** any other file or directory at the top of the folder, and everything else in §8. A
  resource written one level too high (`menu.yaml` beside `resources/`) or a typo such as
  `resource/` must stop the build, not publish an image without them. A file under `resources/`
  or `raw/` is never left out, and never guessed at. A refusal stops the build, and nothing is
  pushed.

_Skipping the known top-level files was added on 2026-11-03 ([plan.md](plan.md), step 2f). It
needed no new layout version: every folder accepted before gives the same tree and the same
digest. In the Go API, `Rendered.Skipped` lists them._

The rules that protect the image itself (§8: links, escaping paths, size) apply everywhere, `raw/`
included. They are about building a safe image, not about what a file means.

Two inputs must give the same set: a directory on disk, and a tar (or tar+gzip) archive whose
entries sit under a root path (a Flux artifact is one). Archive entry names may start with `./`.

## 3. The tree

```text
resources/app.yaml                      byte for byte
resources/database.secret.yaml          byte for byte, still encrypted
resources/orders.yaml                   byte for byte
raw/pricing.toml                        byte for byte
raw/settings.yaml                       byte for byte
raw/products/vanilla.png                byte for byte
configmaps/shop-settings/settings.yaml  derived: one file per ConfigMap key (§5)
```

| Path | Content |
|---|---|
| `resources/<rel>` | every input file under `resources/`, **byte for byte**. Nothing is re-serialised, so a SOPS-encrypted file keeps its MAC valid |
| `raw/<rel>` | every input file under `raw/`, byte for byte |
| `configmaps/<name>/<key>` | for each ConfigMap with files (§5): one file per `data` key (the string's bytes), and one per `binaryData` key (base64-decoded) |

What is in Git under `resources/` and `raw/` is what is mounted under `resources/` and `raw/`.
Settings that refer to another file do so by its path in the image: `picture: raw/products/vanilla.png`.
To list the resources, a reader lists `resources/`.

## 4. `resources/`

Every file under `resources/` must have the extension `.yaml`, `.yml` or `.json` (any case), and
parse as a stream of YAML documents (JSON is YAML, so a `.json` file holds one object). A null or
empty document is skipped, so a trailing `---` or a comments-only file is fine. Every other
document must be a resource:

| Rule | Refused when |
|---|---|
| A resource is a mapping | the document is a list or a scalar |
| A resource has an identity | `apiVersion`, `kind` or `metadata.name` is missing, not a string, or empty; or `metadata` is not a mapping; or `metadata.namespace` is present and not a string |
| The identity is readable | any of those four is SOPS ciphertext (`ENC[AES256_GCM,…]`). The writer must be able to name every resource, a Secret included |
| Keys are unique | a mapping, at any depth, has a key twice. `kind: Secret` followed by `kind: ConfigMap` means different things to different readers |
| The YAML is plain | an anchor, an alias or a merge key (`<<`) is used: each lets one key stand for another |
| Resources are unique | two resources share an identity: group, kind, namespace and name. The group is `apiVersion` without its version (`apps/v1` → `apps`, `v1` → the core group), so one object written at two versions is a duplicate. An absent namespace and `default` are different: the writer cannot know where a resource will be applied |
| No lists | the document is a core-group kind ending in `List` (`List`, `SecretList`, `ConfigMapList`), or `<Kind>List` of a sensitive kind's group. Put each item in its own document; then every item is checked |

Not checked: schemas. A custom resource needs no CRD, and a ConfigMap is not held to the
apiserver's 1 MiB limit or its name rules. Schema checks may come later as an optional lint that
never changes the bytes.

### Secrets

A `Secret` (core group) belongs in `resources/`, and there it must be SOPS-encrypted with its
payload actually encrypted:

- it is in a SOPS-encrypted file (below);
- **and, whatever that file's SOPS rule says**, every value under `data` and `stringData` is
  `ENC[AES256_GCM,…]`. An empty string and `null` are exempt: SOPS never encrypts them.

A writer may take a list of **sensitive kinds** (`group/Kind`, or `Kind` for the core group). A
resource of a sensitive kind is held to the same, for every field outside `apiVersion`, `kind`,
`metadata` and `sops`: the writer cannot know which fields of a custom kind are the secret ones.

Because the identity must stay readable, SOPS's default (encrypt every value) is refused: it
encrypts `kind` and `metadata.name` too. Encrypt the payload only:

```yaml
# .sops.yaml, beside the settings folder (not in it)
creation_rules:
  - encrypted_regex: ^(data|stringData)$
```

What this checks is that the payload is **ciphertext-shaped**. Without a key the writer cannot tell
whether the MAC is valid, the recipients are right, or anything decrypts. And it checks
`resources/` only: a Secret under `raw/` is not read, by design.

A [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) `SealedSecret`
(`bitnami.com/v1alpha1`) is a different mechanism: encrypted for one cluster's controller, which
unseals it into a `Secret`. It is an ordinary custom resource here.

### SOPS-encrypted files

A file under `resources/` with a top-level `sops` key is [SOPS](https://getsops.io)-encrypted. The
checks follow SOPS's own source (getsops/sops), which treats the file, not the document, as the
unit: it writes the same `sops` block into every document and reads it from the first. Such a file
is admitted only when:

- the first document has the `sops` block, as a mapping, with a MAC (`mac: ENC[AES256_GCM,…]`);
- it uses at most one key-based rule: `encrypted_regex`, `unencrypted_regex`, `encrypted_suffix`
  or `unencrypted_suffix`. None means SOPS's default, `unencrypted_suffix: _unencrypted`;
- every value that rule encrypts is `ENC[AES256_GCM,data:…,iv:…,tag:…,type:…]`, with the rule
  applied to the value's key path exactly as SOPS does (`Tree.shouldBeEncrypted`). An empty
  string and `null` are exempt;
- at least one value is encrypted.

Refused by name: `encrypted_comment_regex` and `unencrypted_comment_regex`, because what they
encrypt depends on comments, which decoding does not keep; and `mac_only_encrypted`, because its
MAC leaves the plaintext values unprotected.

The writer never decrypts anything: the file is carried byte for byte, so it decrypts after the
pull.

## 5. ConfigMaps become files

By default, every `ConfigMap` (core group) under `resources/` also appears as plain files:

```text
configmaps/<metadata.name>/<key>
```

- one file per `data` key (the string's bytes), and one per `binaryData` key (base64-decoded);
- the ConfigMap itself stays under `resources/` too, byte for byte;
- `<name>` and `<key>` must match `^[-._a-zA-Z0-9]+$` (the apiserver's own rule for a ConfigMap
  key) and must not be `.` or `..`. A `data` value must be a string, and a `binaryData` value valid
  base64.

**To keep a ConfigMap a resource only**, annotate it:

```yaml
metadata:
  name: feature-flags
  annotations:
    settings-image.configbutler.ai/files: "false"
```

`"true"` (the default) and `"false"` are the only values. The choice lives in the ConfigMap, next
to its data, and shows up in review.

Refused, each with a message that suggests the annotation:

- two ConfigMaps with files and the same name. `metadata.namespace` is not part of the path, so
  `shop-settings` in two namespaces would share `configmaps/shop-settings/`; their keys never mix;
- a ConfigMap with files in a SOPS-encrypted file: one value per file cannot carry the MAC.

An application that mounted a `configMap` volume gets the same files from an image volume with
`subPath: configmaps/<name>`.

## 6. The layer

One uncompressed tar, then gzip. Built so that the same tree always gives the same bytes:

- **Entries:** every file of the tree, plus **every parent directory, explicitly**. A file
  `raw/a/b` gives the entries `raw`, `raw/a` and `raw/a/b`.
- **Order:** sorted by path, byte order, with directory names compared _without_ their trailing
  slash (Go's `sort.Strings` on the plain paths).
- **Headers** (Go's `archive/tar`): `Format: tar.FormatUSTAR`, falling back to `tar.FormatPAX` only
  for a path USTAR cannot encode. `ModTime` is the Unix epoch. Uid and gid are 0, with no user or
  group names.
  - A directory: `Typeflag: TypeDir`, `Name: "<path>/"`, `Mode: 0o755`.
  - A file: `Typeflag: TypeReg`, `Mode: 0o644`, `Size` set.
- **Why 0644 is forced:** an image volume is mounted read-only but **not** `noexec`. The forced
  mode stops a committed file from being executable in the pod. An interpreter can still read a
  script; nothing here runs one.
- **gzip:** Go's `compress/gzip` at `gzip.BestCompression`, with an empty header (no name, no
  modification time). The level and the library are pinned: a different one gives different bytes,
  and so a new digest for every consumer.

The layer's **diffID** is the sha256 of the uncompressed tar. Its **digest** is the sha256 of the
gzip.

## 7. Config and manifest

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
| `ai.configbutler.settings-image.layout` | `v2` | how a reader knows what it holds ([plan.md](plan.md) phase 5) |
| `org.opencontainers.image.source` | the source repository's URL, credentials removed (`https://github.com/<owner>/<repo>`). Omitted when unknown | ghcr.io links the package to that repository through it |

**Nothing else goes in the manifest.** Every annotation is constant per source, so content
addressing holds. "Same folder, same digest" therefore means the same files and the same source
URL; re-encrypting a file with SOPS changes its bytes, and so the digest.

## 8. Refusals

A refusal is permanent for its input: building the same bytes again cannot succeed. It names the
offending path and a reason.

**Everywhere**, `raw/` included:

| Input | Why |
|---|---|
| a file outside `resources/` and `raw/`, unless it is a known top-level file (§2) | say what it is: a resource, or a raw file. A resource one level too high, or a misspelt `resource/`, must not publish an image without it |
| a symlink or hard link | an image volume must not reach outside its folder |
| any other non-regular file | not settings |
| a path that escapes the folder or archive (`..`, absolute) | not settings |
| more than **10 MiB** in total, uncompressed, summed over the input files | settings are small; a large tree is something else |
| an **empty** folder: no file under `resources/` or `raw/` (skipped top-level files do not count) | publishing it would roll every consumer to "no settings" |
| two archive entries for one path, or a path that is both a file and a directory | not a folder |

**Under `resources/`:** a file that is not `.yaml`, `.yml` or `.json`, or does not parse; every
rule of §4's table; a Secret or sensitive kind that breaks §4's encryption rules; a SOPS file that
breaks §4's SOPS rules; and every refusal of §5.

## 9. Provenance: a referrer, not part of the image

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
| `author` | that commit's author, **only when asked for**: `{"name": …}` with `--provenance-author name`, `{"email": …, "name": …}` with `full`. The default, `none`, omits the field. A referrer is public wherever the image is, and must not copy an email out of a repository, least of all a private one. _(Default since 2026-11-03, [plan.md](plan.md) step 2i; before that it was always written in full.)_ |
| `subject` | that commit's subject line |
| `committedAt` | that commit's committer date, RFC 3339, UTC |

A reader ignores fields it does not know, so a later writer can add one (the controller will add
the Flux artifact digest it built from) without a new layout. The referrer records a claim by
whoever pushed it; it does not authenticate them.

Because the blob depends only on the commit, the ref, the repository and the path, a rebuild
attaches the **identical** referrer, not a second one. The same commit built from two branches
attaches two. A revert back to an older digest attaches a second referrer to that digest, so
`oras discover` lists both commits that produced it.

**On a registry without the referrers API** (ghcr.io; **verified**), oras-go stores referrers under
the fallback tag `sha256-<hex of the image digest>`. A writer must set oras-go's
`Repository.SkipReferrersGC = true`. Without it, attaching a second referrer makes oras-go delete
the superseded fallback index, and a registry that refuses `DELETE` (`registry:2` answers 405)
then fails the whole push, although the new index was already written.

## 10. Tags, and pushing in order

- `sha-<first 7 of the commit>`: the commit that last changed the folder. Immutable in practice.
- A **branch tag**: the branch name lowercased, every character outside `[a-z0-9_.-]` replaced by
  `-`, leading `.` and `-` removed, at most 128 characters, and `branch` if nothing is left. So
  `feature/Card` becomes `feature-card`.
- A **timestamp tag**, only when asked for (`--timestamp-tag <branch>`):
  `<branch>-<committedAt as Unix seconds>-<first 7 of the commit>`, for the commit that last
  changed the folder, e.g. `main-1762115267-0123456`. The branch is sanitised as for the branch
  tag, and cut to 109 characters so the tag stays within 128. It is as reproducible as the `sha-`
  tag and never moves, and its number sorts: Flux's `ImagePolicy` follows it with `numerical`
  ([flux.md](flux.md)). It needs Git, so it is refused with `--no-provenance` or `--allow-dirty`.
  _(Designed 2026-11-03, not built yet: [plan.md](plan.md) step 2h.)_
- Any tag the caller names, which must already be a valid OCI tag
  (`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`). The writer refuses an invalid one rather than rewrite it.

Push order: the image, then its provenance referrer, then the tags, **moving tags last** (the
`sha-` and timestamp tags before the branch tag). Whoever follows a tag then never sees a digest
without its provenance.

**A moving tag can move backwards.** Re-running an old workflow run rebuilds the old commit, and
the branch tag goes back to that commit's digest. A consumer that must only ever move forward
follows the timestamp tag, where the highest wins and a re-run only re-pushes a tag that exists.
Every step is idempotent: an existing blob is skipped, and a tag is overwritten with the same value.

**Consumers mount by digest, never by tag.** An image volume is resolved at each _container_
start. With a tag and `pullPolicy: Always`, a crash restart picks up new settings inside a running
pod, with no rollout and no record.

## 11. Known answers

Computed with this repository's writer (oras-go v2.6.4, image-spec v1.1.1, Go 1.27.3) on
2026-11-02, and read back from a registry with `crane`. A writer that disagrees on any of these
bytes is wrong, or has made a new layout version.

**Case `configmap`.** The folder holds one file, `resources/configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: shop-settings
data:
  settings.yaml: |
    maxScoops: 3
```

Built with `org.opencontainers.image.source: https://github.com/ConfigButler/k8s-trail`.

| | Value |
|---|---|
| tree | `configmaps/shop-settings/settings.yaml` (`maxScoops: 3\n`), `resources/configmap.yaml` |
| diffID | `sha256:05e74870873a10c8a89950c4a7fd2bd55ac48aaaa311060c4da95f0eb56ba5ce` |
| layer | `sha256:8a76316fa9647d30e23e8b1d2cd2004d426a13136a2df028766efd609fa61705`, 257 bytes |
| config | `sha256:2880ab7eaf378f9129049468d3e8c515eb108720d64169c35f297c35ef675169`, 151 bytes |
| **manifest** | `sha256:80b524374fdf0db1088308fe2941e6399c50b8d268891a4cd9e2b4e1775c88cd`, 598 bytes |

The manifest's exact bytes:

```json
{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:2880ab7eaf378f9129049468d3e8c515eb108720d64169c35f297c35ef675169","size":151},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:8a76316fa9647d30e23e8b1d2cd2004d426a13136a2df028766efd609fa61705","size":257}],"annotations":{"ai.configbutler.settings-image.layout":"v2","org.opencontainers.image.created":"1970-01-01T00:00:00Z","org.opencontainers.image.source":"https://github.com/ConfigButler/k8s-trail"}}
```

Its provenance, for commit `0123456789abcdef0123456789abcdef01234567` on `refs/heads/main` by
`Ada <ada@example.com>`, subject `feat: three scoops`, committed `2026-11-02T20:41:07Z`, with
`path: configs/shop`. By default (`--provenance-author none`, computed 2026-11-03):

| | Value |
|---|---|
| blob | `sha256:301f5aef07c2f6d6ae5ed4cfc675f2bd3aaae2a4362a0c106fcd62c2691e80a8`, 224 bytes |
| **referrer manifest** | `sha256:04f47a28115c599f2ef7adeef1a98fb43ff93b2be690a90ad0d775d1fac4a488`, 748 bytes |

```json
{"commit":"0123456789abcdef0123456789abcdef01234567","committedAt":"2026-11-02T20:41:07Z","path":"configs/shop","ref":"refs/heads/main","repository":"https://github.com/ConfigButler/k8s-trail","subject":"feat: three scoops"}
```

With `--provenance-author full`:

| | Value |
|---|---|
| blob | `sha256:ce56f1d41b61539fe3ee169c1c23ff7edf39a70cc81d948789cf55d7a351bdae`, 274 bytes |
| **referrer manifest** | `sha256:1ba0abbf99d335ce87cdc1c188679d4aa51b6f44c0956287c83fc1baa5a934cb`, 748 bytes |

```json
{"author":{"email":"ada@example.com","name":"Ada"},"commit":"0123456789abcdef0123456789abcdef01234567","committedAt":"2026-11-02T20:41:07Z","path":"configs/shop","ref":"refs/heads/main","repository":"https://github.com/ConfigButler/k8s-trail","subject":"feat: three scoops"}
```

**Case `nested`.** The folder holds `resources/order.yaml` and `raw/sub/dir/notes.txt`
(`plain text\n`), with the same source annotation:

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
| tree | `raw/sub/dir/notes.txt`, `resources/order.yaml` (no `configmaps/`: there is no ConfigMap) |
| diffID | `sha256:5898df62870752913ecb4012abf516466a47cfb84f197b53fd09c1a26283f557` |
| layer | `sha256:efca61f147c9e742bd7618c9f284a2c5669d71442e5a02e9ddcfa899b8e9832c`, 274 bytes |
| config | `sha256:4cf20694c2ae0fd5cb398148251aded045c87ef51233732ae077661a27d4b982`, 151 bytes |
| **manifest** | `sha256:6101f3d18cd146af08b06eab924a2fa4ecc411e09c3e7a0883118e9b9b9e101b`, 598 bytes |

Every file content above ends with a single newline, and has no trailing spaces.
