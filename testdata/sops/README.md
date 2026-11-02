# SOPS fixtures

Written by the `sops` CLI (v3.13.3), one per SOPS rule, from the same small `Secret`. Each
decrypts with [age-test-key.txt](age-test-key.txt), a test key committed on purpose:

```bash
SOPS_AGE_KEY_FILE=testdata/sops/age-test-key.txt sops decrypt testdata/sops/default.yaml
```

| File | SOPS rule | Admitted |
|---|---|---|
| `default.yaml` | none: SOPS writes `unencrypted_suffix: _unencrypted` and encrypts every other value | yes |
| `encrypted-regex.yaml` | `encrypted_regex: ^(data\|stringData)$` | yes |
| `unencrypted-regex.yaml` | `unencrypted_regex` | yes |
| `encrypted-suffix.yaml` | `encrypted_suffix: Data` | yes |
| `multi-document.yaml` | `encrypted_regex`, two documents, the `sops` block in both | yes |
| `mac-only-encrypted.yaml` | `encrypted_regex` with `mac_only_encrypted` | no: the MAC leaves plaintext unprotected |
| `comment-regex.yaml` | `encrypted_comment_regex` (made with a `.sops.yaml` rule) | no: depends on comments |

Regenerating one gives new ciphertext (a new data key and IVs), so the tests check the rules, not
the bytes.
