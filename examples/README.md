# Examples

Each folder here is a settings folder: build one with `settings-image build examples/<name>`.
`task test-mount` pushes all of them, mounts them in a pod as image volumes, and checks every file.

| Folder | Shows | In the image |
|---|---|---|
| [configmap/](configmap/) | two ConfigMaps in one file, one with `binaryData` | `objects/app.yaml`, and each key as a plain file: `files/shop-settings/settings.yaml`, `files/branding/logo.png` (decoded) |
| [custom-resources/](custom-resources/) | custom resources in YAML and JSON, a nested folder, a file that is not KRM | everything under `objects/`, byte for byte. No `files/`: there is no ConfigMap |
| [sops-secret/](sops-secret/) | a `Secret` sealed with SOPS, next to a plain ConfigMap | the sealed Secret under `objects/`, still encrypted; the ConfigMap's keys under `files/database/` |

## Custom resources need no CRD

The build does not validate against a schema. A `.yaml`, `.yml` or `.json` file must parse as a
stream of objects, and only `apiVersion`, `kind`, `metadata.name`, `data` and `binaryData` are
read: to find ConfigMaps, and to refuse unsealed Secrets. Everything else is carried byte for byte,
and the application decodes the kinds it knows.

## Sealing a Secret

A `Secret` must be sealed with [SOPS](https://getsops.io), and only with an `encrypted_regex`, so
the build can check that no value under a matched key is left in plaintext. The rules are in
[format-v1.md §6](../docs/format-v1.md#6-refusals).

```bash
cd examples
sops encrypt --in-place sops-secret/database.secret.yaml   # uses .sops.yaml here
```

[.sops.yaml](.sops.yaml) sits beside the folders, not in one, so it is not packed. Its recipient is
`testdata/sops/age-test-key.txt`, a **test key** committed on purpose, so you can decrypt what a
pod mounted:

```bash
SOPS_AGE_KEY_FILE=testdata/sops/age-test-key.txt sops decrypt examples/sops-secret/database.secret.yaml
```

The image never holds a key, and the build never decrypts anything. Decrypting is the reader's job,
with its own key.
