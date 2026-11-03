# The `settings-image` CLI

_Design for phase 2 ([plan.md](plan.md)). The CLI is what the GitHub Action
([github-action.md](github-action.md)) runs, and what a person runs by hand. The bytes it writes
are [format-v2.md](format-v2.md)'s; this page is only the interface._

## 1. Commands

```bash
settings-image build <folder> [--oci-layout <dir>] [flags]
settings-image push  <folder> --repository <registry>/<path> [flags]
settings-image version
```

- **`build`** renders the folder and builds the image. It writes nothing anywhere unless
  `--oci-layout <dir>` is given, in which case it writes an
  [OCI image layout](https://github.com/opencontainers/image-spec/blob/main/image-layout.md)
  directory (oras-go's `oci.Store`) holding the image and its provenance, tagged as `push` would.
  A refusal is reported exactly as `push` reports it. This is the dry run, and how the conformance
  corpus is written.
- **`push`** does `build` and then pushes, in [format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order)'s
  order.
- **`version`** prints the build (exists).

## 2. Flags

| Flag | `build` | `push` | Meaning |
|---|---|---|---|
| `--repository <host/path>` | | required | where to push, without a tag: `ghcr.io/configbutler/card-reveal-config` |
| `--tag <tag>` (repeatable) | yes | yes | an extra tag. Must be a valid OCI tag; refused otherwise, never rewritten |
| `--branch-tag <branch>` | yes | yes | add the sanitised branch tag ([format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order)); pushed last |
| `--source <url>` | yes | yes | the repository URL for `org.opencontainers.image.source` and provenance; overrides detection (§3) |
| `--sensitive-kind <group/Kind>` (repeatable) | yes | yes | a kind under `resources/` that must be SOPS-encrypted like a `Secret` ([format-v2.md §4](format-v2.md#secrets)); `Kind` alone for the core group. A `Secret` must always be SOPS-encrypted. Whether a ConfigMap becomes files is not a flag: it is an annotation on the ConfigMap ([format-v2.md §5](format-v2.md#5-configmaps-become-files)) |
| `--no-provenance` | yes | yes | build no referrer and no `sha-` tag, and skip Git entirely (also allows a folder outside Git); the source then comes from `--source` or `GITHUB_*` only |
| `--allow-dirty` | yes | yes | build a folder with uncommitted changes; implies no provenance and no `sha-` tag |
| `--allow-shallow` | yes | yes | accept a shallow clone (§3); provenance then names HEAD and says so in a warning |
| `--plain-http` | | yes | for a local registry without TLS |
| `--username`, `--password-stdin` | | yes | registry credentials (§4) |
| `--registry-token-stdin` | | yes | a bearer token for the registry, sent unchanged (§4). Not built yet: [plan.md](plan.md) step 2g |
| `--timestamp-tag <branch>` | yes | yes | add `<branch>-<unix committedAt>-<sha7>`, a tag that never moves and sorts by commit time ([format-v2.md §10](format-v2.md#10-tags-and-pushing-in-order)); what Flux follows ([flux.md](flux.md)). Not built yet: step 2h |
| `--provenance-author none\|name\|full` | yes | yes | how much of the commit author goes into provenance ([format-v2.md §9](format-v2.md#9-provenance-a-referrer-not-part-of-the-image)); default `none`. Not built yet: step 2i |
| `--json` | yes | yes | print the result as JSON on stdout (§5) |

## 3. Git: where provenance and the `sha-` tag come from

The folder must be inside a Git work tree unless `--no-provenance` is set. The CLI shells out to
`git`; it does not link a Git library.

1. **Refuse a dirty folder:** `git status --porcelain -- <folder>/resources <folder>/raw` must be
   empty, unless `--allow-dirty`. Otherwise the image would hold bytes no commit holds, while the
   provenance names a commit.
2. **The commit that last changed the folder:**
   `git log -1 --format=%H%x00%an%x00%ae%x00%s%x00%cI -- <folder>/resources <folder>/raw`. Using
   this commit rather than HEAD means a rebuild on a later commit that did not touch the folder is a
   complete no-op: the same digest, the same referrer and the same `sha-` tag.

   Both commands look only at `resources/` and `raw/`, the parts that reach the image
   ([format-v2.md §2](format-v2.md#2-input-the-folder)). A known top-level file such as
   `.gittargetignore` or `README.md` is skipped, so editing it changes neither the digest nor the
   commit, and an uncommitted edit to it does not make the folder dirty.
3. **Refuse a shallow clone** (`git rev-parse --is-shallow-repository` is `true`) unless
   `--allow-shallow`. In a depth-1 clone, `git log -- <folder>` returns HEAD for every folder,
   because the boundary commit appears to add everything. The answer would be wrong without any
   error. The Action's documented checkout is `fetch-depth: 0`.
4. **Repository URL**, the first that applies:
   - `--source`;
   - `$GITHUB_SERVER_URL/$GITHUB_REPOSITORY` when both are set;
   - `git remote get-url origin`, normalised: userinfo removed, `git@host:o/r` becomes
     `https://host/o/r`, a trailing `.git` dropped;
   - none, in which case the annotation and the field are omitted.
5. **Ref:** `$GITHUB_REF` when set, otherwise `git symbolic-ref -q HEAD`, otherwise omitted.
6. **Path:** the folder relative to `git rev-parse --show-toplevel`, slash-separated.

## 4. Registry credentials

The CLI knows the generic forms of registry credential and nothing about any one registry. A
cloud's own token exchange (ECR, ACR, Artifact Registry) belongs to that cloud's login action,
which leaves its result in the Docker config.

| Credential | Sent as | For example |
|---|---|---|
| a username and a password | HTTP Basic, or exchanged at the registry's token service | ghcr.io, Docker Hub, Harbor, Quay; a token that a registry takes as a password |
| a **registry token** | `Authorization: Bearer <token>`, unchanged | zot accepting a GitHub Actions OIDC token; any registry that takes a bearer token as is |
| the Docker config and its credential helpers | whatever `docker login` stored | ECR, ACR and Artifact Registry through their login actions; `docker/login-action` |
| none | anonymous | a local registry |

In this order, the first that is given:

1. `--username` with `--password-stdin`, or `--registry-token-stdin`;
2. `SETTINGS_IMAGE_USERNAME` and `SETTINGS_IMAGE_PASSWORD`, or `SETTINGS_IMAGE_REGISTRY_TOKEN`,
   from the environment (what the Action sets);
3. the Docker config and its credential helpers, as `docker login` left them (oras-go's
   `credentials.NewStoreFromDocker`), so `docker/login-action` works. Its `registrytoken` field is
   a registry token, so writing a token there works today, without the flag;
4. anonymous.

A run uses one credential. A registry token together with `--username` or `--password-stdin` (or
their variables) is a usage error (exit 2), never a guess. In the Go API the token is
`RepositoryOptions.RegistryToken`.

**A registry token is sent as `Authorization: Bearer <token>` on every request to the registry's
own host, from the first request on**, whatever challenge the registry would answer with. That is
what `crane` does: go-containerregistry sends a registry token as Bearer even to a `Basic`
challenge. oras-go alone sends a token only to a `Bearer` challenge, so the CLI sets the header
itself, in a transport that knows the registry host.

**Never to another host.** A blob request may be redirected to storage (S3, a CDN); the token stops
at the registry. The transport compares the request's host and port with the registry's, and sends
nothing to any other.

A registry that refuses the credential (HTTP 401 or 403) exits 4 (§6), naming the registry and
never the credential.

_The registry token (`--registry-token-stdin`, `SETTINGS_IMAGE_REGISTRY_TOKEN`, `RegistryToken`) is
designed (2026-11-03), not built yet: [plan.md](plan.md) step 2g._

A credential is never printed, logged or put in an error. A 401 or 403 from the registry is its own
error, naming the registry and saying the credential was refused.

## 5. Output

Progress and warnings go to **stderr**. With `--json`, **stdout** carries exactly one JSON object:

```json
{
  "repository": "ghcr.io/configbutler/card-reveal-config",
  "digest": "sha256:80b52437…",
  "reference": "ghcr.io/configbutler/card-reveal-config@sha256:80b52437…",
  "tags": ["sha-0123456", "main"],
  "provenance": "sha256:8962065a…",
  "commit": "0123456789abcdef0123456789abcdef01234567",
  "skipped": [".gittargetignore"],
  "changed": true
}
```

- `provenance` and `commit` are `null` when there are none.
- `skipped` lists the files at the top of the folder that were left out
  ([format-v2.md §2](format-v2.md#2-input-the-folder)), each also named in a warning on stderr. It
  is `[]` when there are none.
- `changed` is `false` when, before the push, the image already existed in the repository **and**
  every tag already pointed at it. That is the "nothing to do" case, and the Action reports it.
- `build` gives the same object without `changed`, and with `repository` set to `--repository` if
  given.

Without `--json`, stdout carries only the `reference`, so `ref=$(settings-image push …)` works.

## 6. Exit codes

| Code | Meaning |
|---|---|
| 0 | built (and pushed), or nothing to do |
| 1 | an error that a retry might fix: network, registry, Git |
| 2 | usage: unknown command or flag, missing argument |
| 3 | **refused**: the folder breaks a rule of [format-v2.md §8](format-v2.md#8-refusals), or a tag is invalid, or the folder is dirty, outside Git (without `--no-provenance`), or the clone shallow. Retrying cannot help; the message names the path |
| 4 | **the registry refused the credential** (HTTP 401 or 403): a wrong or expired token, a missing trust rule, no push permission. Retrying cannot help; the message names the registry. _(Designed 2026-11-03, not built yet: [plan.md](plan.md) step 2g; until then this is exit 1)_ |

## 7. Not in the CLI

- **No decryption, ever, and no keys.** SOPS-encrypted files are checked, not decrypted.
- **No registry management.** Creating the repository, setting visibility and retention are the
  registry's business.
- **No watching.** Following a Git source continuously is the controller's job (phase 3).
