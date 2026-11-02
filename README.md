# settings-image

Build a folder of settings into a small, content-addressed OCI image, and mount it next to your
application with a Kubernetes [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).

> **Status: bootstrapping.** The repository has its toolchain, CI and release process. The CLI
> only answers `version` so far.

## Why an image

An image volume mounts the files of an OCI image read-only into a pod, and a new digest is an
ordinary rolling update: the configuration that runs is declared in the pod spec, and
`kubectl rollout undo` rolls it back. The image needs no base image, no shell and no entrypoint.
It is one layer holding your files.

It does have to be a **real image**: an image config whose `rootfs.diff_ids` lists the layer.
Tools that push generic OCI artifacts do not produce one. A plain `oras push` is refused when the
kubelet pulls it, and a Flux artifact mounts as an **empty directory, with no error**.

settings-image is being built to produce the real thing, and to add what settings need:

- **The same folder gives the same digest.** A commit that touches other files rolls no pods, and a
  rollback lands on the old digest.
- **Refusals before anything is pushed**: an empty folder, a link that leaves the folder, a
  Kubernetes `Secret`.
- **Provenance**: the commit and its author, attached to the image as an OCI referrer.

It understands Kubernetes objects (KRM) first, and is not limited to them.

## Develop

Open the repository in its devcontainer. Everything is a [Task](https://taskfile.dev):

```bash
task test     # unit tests
task lint     # Go, go.mod, Dockerfiles, workflows, markdown
task build    # bin/settings-image, with its version baked in
```

## License

[Apache 2.0](LICENSE).
