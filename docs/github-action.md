# The GitHub Action

_Design for phase 2 ([plan.md](plan.md)). The Action runs the CLI ([cli.md](cli.md)) in a
workflow: a commit that changes a settings folder becomes a new settings image._

## 1. What a user writes

```yaml
on:
  push:
    branches: [main]
    paths: [configs/card-reveal/**]

permissions:
  contents: read
  packages: write

# Publish in commit order: a later run must not finish before an earlier one.
concurrency:
  group: settings-image-card-reveal
  cancel-in-progress: false

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<sha> # pinned
        with:
          fetch-depth: 0 # the CLI refuses a shallow clone (cli.md §3)
      - id: settings
        uses: ConfigButler/settings-image@v0
        with:
          path: configs/card-reveal
          repository: ghcr.io/configbutler/card-reveal-config
      - run: echo "published ${{ steps.settings.outputs.reference }}"
```

On ghcr.io, nothing else is needed: the workflow's own `GITHUB_TOKEN` can push to a package in its
organisation.

**Another registry.** For a registry that accepts the workflow's GitHub OIDC token as a bearer
token (zot, with a rule on the token's `repository_id` and `ref` claims), the job asks for an ID
token and names the audience the registry expects:

```yaml
jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      id-token: write # the OIDC token
    steps:
      - uses: actions/checkout@<sha> # pinned
        with:
          fetch-depth: 0
      - uses: ConfigButler/settings-image@v0
        with:
          path: coffee # coffee/resources and coffee/raw
          repository: zot.example.com/acme/shop-settings
          oidc-audience: zot.example.com # the registry host: the convention
```

A registry with its own login action (ECR, ACR, Artifact Registry) runs that action first; the
Action then uses the Docker config it leaves. A registry with a username and a token takes
`username` and `password`. Only one credential is used per run ([cli.md §4](cli.md#4-registry-credentials)).

## 2. `action.yml`, at the repository root

The root is where the Marketplace requires it, and it lets a workflow say
`uses: ConfigButler/settings-image@v0`.

**Inputs:**

| Input | Default | Passed as |
|---|---|---|
| `path` | required | the folder |
| `repository` | required | `--repository` |
| `branch-tag` | `${{ github.ref_name }}` | `--branch-tag`. Set it to `''` for none |
| `tags` | `''` | one `--tag` per line |
| `sensitive-kinds` | `''` | one `--sensitive-kind` per line |
| `provenance` | `'true'` | `--no-provenance` when `'false'` |
| `provenance-author` | `none` | `--provenance-author`: `none`, `name` or `full` |
| `timestamp-tag` | `''` | `--timestamp-tag`, e.g. `${{ github.ref_name }}`: a sortable tag for Flux ([flux.md](flux.md)) |
| `username` | `${{ github.actor }}` | `SETTINGS_IMAGE_USERNAME`, only together with a password |
| `password` | `''` | `SETTINGS_IMAGE_PASSWORD`. When no credential input is set and `repository` is on `ghcr.io`, the Action uses `${{ github.token }}`. Otherwise, with no credential set, the Docker config applies, as `docker/login-action` left it |
| `registry-token` | `''` | `SETTINGS_IMAGE_REGISTRY_TOKEN`: a bearer token sent unchanged |
| `oidc-audience` | `''` | the Action requests the workflow's GitHub OIDC token for this audience, masks it, and passes it as the registry token. **Use the registry host** (`zot.example.com`), as Flux's RFC-0014 defaults it. The token is requested in the push step, after the Go build, right before the push: it lives minutes. Needs `permissions: id-token: write`; without it the step fails and says so |
| `plain-http` | `'false'` | `--plain-http` when `'true'` (a service-container registry) |

At most one of `password`, `registry-token` and `oidc-audience` may be set; two is an error.

**`github.token` goes to ghcr.io only.** The `password` default used to be `${{ github.token }}` for
every registry, so a workflow pushing elsewhere handed the repository's token to that registry. A
token is a secret of the place that issued it. With the new default nothing changes on ghcr.io, and
`password: ''` (the old way to choose the Docker config) still means the same.

_`registry-token`, `oidc-audience`, the ghcr.io-only default and the `commit` and `skipped`
outputs were designed on 2026-11-03 and are not built yet ([plan.md](plan.md), step 2g);
`timestamp-tag` is step 2h. `provenance-author` is built (step 2i)._

**Outputs:** `digest`, `reference`, `tags` (comma-separated), `changed`, `commit` (empty without
provenance) and `skipped` (comma-separated), read from the CLI's `--json` with `jq`, which is on
every hosted runner.

**`runs: using: composite`.** Every input reaches the script through `env:`, **never** through
`${{ }}` inside `run:`. A branch name or path is then data, not shell. (That is the usual injection
hole in composite actions.)

## 3. Where the binary comes from

**Phase 2a: built from the Action's own source.**

- `actions/setup-go` with `go-version-file: ${{ github.action_path }}/go.mod`, caching on the
  Action's `go.sum`.
- Then `go build -o "$RUNNER_TEMP/settings-image" ./cmd/settings-image`, in `github.action_path`.

The binary is then always exactly the ref the workflow pinned, whether a tag, a branch or a SHA, and
it needs no release to exist. The cost is a Go setup, about 10–20 s with a warm cache.

**Phase 2b: a released binary (not built yet).** Releases carry binaries (plan.md phase 2d,
`settings-image_<os>_<arch>` and `settings-image.sha256sums`). The Action is to download the one for
its own version and check it against that file.

- `action.yml` has an input `version`, whose default is the released version. release-please keeps
  that default current through its `extra-files` generic updater: the line carries a
  `# x-release-please-version` marker.
- `version: source` keeps 2a's behaviour, for testing an unreleased ref.

## 4. Versioning and the floating tag

- Releases are `vX.Y.Z`, cut by release-please.
- After each release, the release workflow moves a floating **`v0`** tag (later `v1`) to the
  release commit. That is the Actions convention, and it is what `@v0` resolves to.
- Moving a tag is a force-push of that **tag** only, never of a branch.
- Users who pin a SHA (recommended in security-minded repositories) are unaffected by the move.

## 5. Testing the Action in this repository

`.github/workflows/action-selftest.yml` runs the Action **from the checkout** (`uses: ./`), against
`testdata/selftest/` (one ConfigMap, one plain file):

- **On every pull request:** a `registry:2` service container on `localhost:5000`, and
  `plain-http: 'true'`. There are no tokens, so it is fork-safe. Afterwards `crane` reads the image
  back and checks:
  - the digest equals the Action's `digest` output;
  - the layer lists the expected files with mode `0644`;
  - the provenance referrer is listed under the fallback tag.

  The prototype reads the registry with `curl` and `jq`, which every hosted runner has, instead of
  installing `crane`.
- **A second run of the Action in the same job** must report `changed: false` with the same digest.
- **On `main` only:** the same, pushed to `ghcr.io/configbutler/settings-image-selftest` with
  `GITHUB_TOKEN`, so the real ghcr.io path stays green.

## 6. The Marketplace

The requirements: a public repository, `action.yml` at the root, one action per repository, and an
action `name` that is unique on the Marketplace.

| `action.yml` field | Value |
|---|---|
| `name` | `Push settings image` (unique as of 2026-11-02) |
| `description` | `Turn a folder of settings into a mountable OCI image and push it` |
| `branding` | a Feather icon (`package`) and a colour |

Listing it is a manual step in the GitHub UI after a release. It is not a phase gate.
