# Changelog

## 0.1.0 (2026-11-02)


### ⚠ BREAKING CHANGES

* layout v2. A folder must put its files under resources/ or raw/; the image paths objects/ and files/ become resources/, raw/ and configmaps/; --no-files and the Action's files input are removed; SOPS's default rule (encrypt every value) is refused for resources, because the identity must stay readable.

### Features

* admit SOPS-sealed documents, add examples and a k3d mount test ([467ef7e](https://github.com/ConfigButler/settings-image/commit/467ef7e5031578cc8efd3748047915de6d89d3a7))
* align SOPS checks with getsops/sops, and add a pictures example ([0089b7b](https://github.com/ConfigButler/settings-image/commit/0089b7bb74f6f6d28ea8508d9e51f468af34bdbe))
* attach CLI binaries to releases, and show consumers a full CI setup ([f859d07](https://github.com/ConfigButler/settings-image/commit/f859d077821d6dc0a5f38494f12e7ae1d398dbb5))
* build and push settings images, with a composite GitHub Action ([2b60369](https://github.com/ConfigButler/settings-image/commit/2b603696e0a925a92fb15a4eef23e5290d6e4b49))
* layout v2, a settings folder says what each file is ([5bc760f](https://github.com/ConfigButler/settings-image/commit/5bc760f57b485e14530d60e4b357a9f6fd4e3bbb))


### Documentation

* plan, format v1, CLI, GitHub Action and the case for image volumes ([2110251](https://github.com/ConfigButler/settings-image/commit/21102512a8b459c8b55960e76249141f4d996af5))
