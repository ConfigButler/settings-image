# Changelog

## [0.2.1](https://github.com/ConfigButler/settings-image/compare/v0.2.0...v0.2.1) (2026-11-03)


### Miscellaneous Chores

* release 0.2.1 ([d386ab6](https://github.com/ConfigButler/settings-image/commit/d386ab62678bb8c18f2eeab8517fb81ca28d3309))

## [0.2.0](https://github.com/ConfigButler/settings-image/compare/v0.1.0...v0.2.0) (2026-11-03)


### Features

* add a timestamp tag that never moves, for Flux image automation ([4bd6d43](https://github.com/ConfigButler/settings-image/commit/4bd6d430ca97a6811b563217a0f7bff5a505f252))
* name no commit author in provenance by default ([f00d31f](https://github.com/ConfigButler/settings-image/commit/f00d31fb20f93127aeaf9f4105108dcf4205c9fb))
* push to any registry with a bearer token, and exit 4 on a refused credential ([9cbe2cc](https://github.com/ConfigButler/settings-image/commit/9cbe2cc5e565a9613dc37392ccca366711042f22))
* skip known top-level files beside resources/ and raw/ ([62a3262](https://github.com/ConfigButler/settings-image/commit/62a3262675918389b93b069422d9c1d80406621b))


### Bug Fixes

* check every path at Build, read only up to the size limit, refuse stray directories ([d207382](https://github.com/ConfigButler/settings-image/commit/d207382f3a7f27aabd238651205a407c56a43b9e))
* the Action failed to load: an expression in an input description ([d88fcd8](https://github.com/ConfigButler/settings-image/commit/d88fcd8bbee9cc85cfdd3e5ee588e83f04927499))


### Documentation

* a README for someone who just wants to push and mount, and less history ([b0e793e](https://github.com/ConfigButler/settings-image/commit/b0e793ed4c5e3c029b4f3a5d8dc09337d5a8104a))
* a walkthrough that copy-pastes, and say what happens to resources and Secrets ([a229844](https://github.com/ConfigButler/settings-image/commit/a22984451bbd469251429a0c67075f5298089c3f))
* design skipping top-level files and pushing to any registry ([c940497](https://github.com/ConfigButler/settings-image/commit/c9404975ff7a8cf495858f515e39ddac616126d4))
* plan a sortable tag for Flux, private-by-default provenance, and token fixes ([73ff226](https://github.com/ConfigButler/settings-image/commit/73ff2268369ab0951ac018e72a004e779a276080))
* the README says plainly that this is a Kubernetes-aware settings ([d207382](https://github.com/ConfigButler/settings-image/commit/d207382f3a7f27aabd238651205a407c56a43b9e))
* v0.1.0 is out, with binaries; record how its release was finished ([bc3fd24](https://github.com/ConfigButler/settings-image/commit/bc3fd24c6cd9f6f55ff1a79b60b810ed06e08dc2))

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
