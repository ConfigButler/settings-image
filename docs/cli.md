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
| `--json` | yes | yes | print the result as JSON on stdout (§5) |

## 3. Git: where provenance and the `sha-` tag come from

The folder must be inside a Git work tree unless `--no-provenance` is set. The CLI shells out to
`git`; it does not link a Git library.

1. **Refuse a dirty folder:** `git status --porcelain -- <folder>` must be empty, unless
   `--allow-dirty`. Otherwise the image would hold bytes no commit holds, while the provenance names
   a commit.
2. **The commit that last changed the folder:**
   `git log -1 --format=%H%x00%an%x00%ae%x00%s%x00%cI -- <folder>`. Using this commit rather than
   HEAD means a rebuild on a later commit that did not touch the folder is a complete no-op: the
   same digest, the same referrer and the same `sha-` tag.
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

In this order:

1. `--username` with `--password-stdin`;
2. `SETTINGS_IMAGE_USERNAME` and `SETTINGS_IMAGE_PASSWORD` from the environment (what the Action
   sets);
3. the Docker config and its credential helpers, as `docker login` left them (oras-go's
   `credentials.NewStoreFromDocker`), so `docker/login-action` works;
4. anonymous.

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
  "changed": true
}
```

- `provenance` and `commit` are `null` when there are none.
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

## 7. Not in the CLI

- **No decryption, ever, and no keys.** SOPS-encrypted files are checked, not decrypted.
- **No registry management.** Creating the repository, setting visibility and retention are the
  registry's business.
- **No watching.** Following a Git source continuously is the controller's job (phase 3).
