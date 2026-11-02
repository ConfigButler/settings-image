# AGENTS.md

Guidance for any coding agent working in this repository.

settings-image builds a folder of settings into a content-addressed OCI image that a kubelet can
mount as `volumes[].image`, and pushes it. Read [README.md](README.md) first, then
[docs/plan.md](docs/plan.md) for what to build next. [docs/format-v1.md](docs/format-v1.md) is the
contract: its known-answer digests are the acceptance test for every byte the code writes.

## Definition of done

Do not say something is "done", "works" or "fixed" until the gate passed:

```bash
task test    # 0 failures
task lint    # golangci-lint, go mod tidy, hadolint, actionlint, markdownlint: 0 issues
```

For anything that builds or pushes an image, also **observe it**: push to a registry and read it
back with the *other* tool (`crane` for what we wrote with oras-go, and the reverse), and mount it
in a pod when the change touches the image's shape. A digest test proves the bytes are stable; it
does not prove a kubelet accepts them.

When you finish, say which of these you ran and what you saw. A step you skipped is reported as
skipped, never as passed.

## How changes reach `main`

**Commit straight to `main` and push**; no feature branch or pull request is needed. The gate
above still applies before you push: CI runs after the push, not before it.

- **Conventional Commits** (`feat:`, `fix:`, `docs:`, `chore:` …). release-please reads them to
  pick the next version and write the changelog, so the prefix is not decoration. A PR title is
  checked against the same list.
- **Never rewrite `main`'s history.** A release-please PR is merged as a PR, never hand-edited.

## How the repository works

- **Everything is a `task`** ([Taskfile.yml](Taskfile.yml)). CI runs `docker run <ci-image> task
  <something>`, so a red job reproduces locally with the same command, and changing what CI checks
  means editing the Taskfile, not a workflow.
- **The CI image is the devcontainer's `ci` stage** ([.devcontainer/Dockerfile](.devcontainer/Dockerfile)).
  A tool that `task lint` or `task test` needs goes in that stage; `dev` is for the editor only.
  Tool versions are pinned in its ENV block, beside the URL to check for a newer one.
- **There is ONE version**, in [.release-please-manifest.json](.release-please-manifest.json).
  `task build` bakes it into the binary (`internal/buildinfo`); nothing reads it from a tag.
- **Go is pinned to gitops-reverser's line** (`golang:1.27.3`, `GOTOOLCHAIN=local`), so code can
  move between the two repositories without a toolchain mismatch.
- Every Go file starts with `// SPDX-License-Identifier: Apache-2.0` (golangci-lint's `goheader`).
