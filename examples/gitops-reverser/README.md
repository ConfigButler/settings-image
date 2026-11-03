# Coffee settings

gitops-reverser writes the cluster's coffee resources into `resources/`. This README and the
`.gittargetignore` marker beside it are skipped by settings-image: they are named in its output,
and never reach the image.
