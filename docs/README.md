# Docs

Start with the [README](../README.md): what you need, the rules, and how to push and mount an
image.

## Using it

| Page | Read it when |
|---|---|
| [cli.md](cli.md) | you run `settings-image` by hand or in a CI other than GitHub Actions: every flag, credentials, the `--json` output, exit codes |
| [github-action.md](github-action.md) | you publish from GitHub Actions: every input and output, other registries |
| [flux.md](flux.md) | you want new settings to roll out by themselves, with Flux image automation |
| [alternatives.md](alternatives.md) | you wonder why this exists: images versus artifacts, what Docker, ORAS and Flux produce, and the other ways settings reach a pod |

## Working on it

| Page | Read it when |
|---|---|
| [plan.md](plan.md) | what is done, and what to build next |
| [format-v2.md](format-v2.md) | you write or read image bytes. The contract, with known-answer digests |
| [background.md](background.md) | you wonder why a rule exists: what image volumes need, and what was measured |
