# Examples

Each folder here is a settings folder: build one with `settings-image build examples/<name>`.
`task test-mount` pushes all of them, mounts them in a pod as image volumes, and checks every file.

A settings folder has two folders ([format-v2.md](../docs/format-v2.md)):

- **`resources/`**: Kubernetes resources. Read and checked: a `Secret` must be SOPS-encrypted, and
  a `ConfigMap`'s keys also become plain files under `configmaps/<name>/`.
- **`raw/`**: anything else, byte for byte. Never read.

| Folder | Shows | In the image |
|---|---|---|
| [configmap/](configmap/) | three ConfigMaps in one file: one with `binaryData`, one annotated to stay a resource only | `resources/app.yaml`, and each key as a plain file: `configmaps/shop-settings/settings.yaml`, `configmaps/branding/logo.png` (decoded). No `configmaps/feature-flags/` |
| [custom-resources/](custom-resources/) | custom resources in YAML and JSON, a nested folder, a TOML file in `raw/` | everything byte for byte, at the same paths. No `configmaps/`: there is no ConfigMap |
| [pictures/](pictures/) | product pictures (PNG) and a user's avatar (SVG) in `raw/`, referred to by path from custom resources | every file byte for byte: `raw/products/vanilla.png` |
| [sops-secret/](sops-secret/) | a SOPS-encrypted `Secret`, next to a plain ConfigMap | the Secret under `resources/`, still encrypted; the ConfigMap's keys under `configmaps/database/` |

## Pictures and other files

Anything in `raw/` goes into the image as it is, so a picture needs nothing special: commit it, and
refer to it by its path in the image (`raw/products/vanilla.png`) from the settings. That beats
`binaryData` in a ConfigMap (as in [configmap/](configmap/)) for anything larger than a logo: no
base64, and no 1 MiB object limit. The whole folder is limited to 10 MiB.

The same goes for an application's own settings file: `raw/settings.yaml` is never parsed, so it
may be a list, a scalar, or anything the application reads.

## Custom resources need no CRD

The build does not validate against a schema. A file under `resources/` must parse as a stream of
YAML (or JSON) resources, each with `apiVersion`, `kind` and `metadata.name`, and no resource may
appear twice. Everything else is carried byte for byte, and the application decodes the kinds it
knows.

## Keeping a ConfigMap a resource

Every ConfigMap's keys become files by default. To ship one as a resource only:

```yaml
metadata:
  name: feature-flags
  annotations:
    settings-image.configbutler.ai/files: "false"
```

## Encrypting a Secret with SOPS

A `Secret` must be encrypted with [SOPS](https://getsops.io), and every value under `data` and
`stringData` must be ciphertext, whatever the SOPS rule says. Its `apiVersion`, `kind` and
`metadata` must stay readable, so encrypt the payload only, as [.sops.yaml](.sops.yaml) here does
with `encrypted_regex: ^(data|stringData)$`. The rules are in
[format-v2.md §4](../docs/format-v2.md#secrets).

```bash
cd examples
sops encrypt --in-place sops-secret/resources/database.secret.yaml   # uses .sops.yaml here
```

[.sops.yaml](.sops.yaml) sits beside the folders, not in one, so it is not packed. Its recipient is
`testdata/sops/age-test-key.txt`, a **test key** committed on purpose, so you can decrypt what a
pod mounted:

```bash
SOPS_AGE_KEY_FILE=testdata/sops/age-test-key.txt sops decrypt examples/sops-secret/resources/database.secret.yaml
```

The image never holds a key, and the build never decrypts anything. Decrypting is the reader's job,
with its own key.
