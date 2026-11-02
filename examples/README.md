# Examples

Each folder here is a settings folder: build one with `settings-image build examples/<name>`.
`task test-mount` pushes all of them, mounts them in a pod as image volumes, and checks every file.

| Folder | Shows | In the image |
|---|---|---|
| [configmap/](configmap/) | two ConfigMaps in one file, one with `binaryData` | `objects/app.yaml`, and each key as a plain file: `files/shop-settings/settings.yaml`, `files/branding/logo.png` (decoded) |
| [custom-resources/](custom-resources/) | custom resources in YAML and JSON, a nested folder, a file that is not KRM | everything under `objects/`, byte for byte. No `files/`: there is no ConfigMap |
| [pictures/](pictures/) | product pictures (PNG) and a user's avatar (SVG), referred to by path from custom resources | every file under `objects/`, byte for byte: `objects/products/vanilla.png` |
| [sops-secret/](sops-secret/) | a SOPS-encrypted `Secret`, next to a plain ConfigMap | the Secret under `objects/`, still encrypted; the ConfigMap's keys under `files/database/` |

## Pictures and other binary files

Any file in the folder goes into the image as it is, so a picture needs nothing special: commit it,
and refer to it by path from the settings. That beats `binaryData` in a ConfigMap (as in
[configmap/](configmap/)) for anything larger than a logo: no base64, and no 1 MiB object limit.
The whole folder is limited to 10 MiB.

## Custom resources need no CRD

The build does not validate against a schema. A `.yaml`, `.yml` or `.json` file must parse as a
stream of objects, and only `apiVersion`, `kind`, `metadata.name`, `data` and `binaryData` are
read: to find ConfigMaps, and to refuse Secrets that are not SOPS-encrypted. Everything else is carried byte for byte,
and the application decodes the kinds it knows.

## Encrypting a Secret with SOPS

A `Secret` must be encrypted with [SOPS](https://getsops.io). Any of SOPS's key-based rules works,
its default included (`sops encrypt` with no rule encrypts every value); the build checks that no
value the rule encrypts is in plaintext. The rules are in
[format-v1.md §6](../docs/format-v1.md#6-refusals). This example uses `encrypted_regex`, so
`metadata` stays readable:

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
