# Multi-stage Dockerfile for settings-image.
# Stage 1 (ci): every tool `task lint` and `task test` need. CI runs those tasks inside this
# stage, so a tool that exists only in `dev` passes locally and is missing in CI.
# Stage 2 (dev): the ci stage plus editor and debugging tools, for the devcontainer.
#
# Pinned by digest; Dependabot's docker ecosystem keeps version + digest current together.
# Same Go line as gitops-reverser, so the two repositories can share code without a toolchain
# mismatch (the golang image sets GOTOOLCHAIN=local: this Go is the only Go there is).
FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS ci

ENV DEBIAN_FRONTEND=noninteractive

# pipefail, so a failed `curl ... | tar` aborts the build instead of installing a truncated binary.
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Essential packages, the Docker CLI (docker-outside-of-docker) and gh.
RUN apt-get update \
    && apt-get -y install --no-install-recommends \
    git \
    curl \
    ca-certificates \
    less \
    jq \
    yq \
    sudo \
    acl \
    gnupg \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
       > /etc/apt/sources.list.d/docker.list \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
       -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod a+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get -y install --no-install-recommends docker-ce-cli docker-buildx-plugin gh \
    && apt-get autoremove -y \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Tool versions, centralized. amd64 only.
#
# Finding the latest versions:
# TASK_VERSION -> https://github.com/go-task/task/releases
# GOLANGCI_LINT_VERSION -> https://github.com/golangci/golangci-lint/releases
# ACTIONLINT_VERSION -> https://github.com/rhysd/actionlint/releases
# HADOLINT_VERSION -> https://github.com/hadolint/hadolint/releases
# ORAS_VERSION -> https://github.com/oras-project/oras/releases
# CRANE_VERSION -> https://github.com/google/go-containerregistry/releases
#   oras pushes and inspects the images this repository builds; crane is the independent second
#   reader that checks them. Two tools on purpose: one reads back what the other did not write.
# KUBECTL_VERSION -> https://github.com/kubernetes/kubernetes/releases
# K3D_VERSION -> https://github.com/k3d-io/k3d/releases
#   kubectl and k3d run the image-volume test cluster (task cluster, task test-mount), with the
#   same k3s line as gitops-reverser.
# SOPS_VERSION -> https://github.com/getsops/sops/releases
# AGE_VERSION -> https://github.com/FiloSottile/age/releases (no checksum file: AGE_SHA256 is
#   the sha256 of age-${AGE_VERSION}-linux-amd64.tar.gz, recorded on first download)
#   sops and age make the SOPS fixtures and decrypt what the test cluster mounted.
# NODE_MAJOR -> https://github.com/nodejs/node/releases (a current LTS line)
# MARKDOWNLINT_CLI2_VERSION -> https://www.npmjs.com/package/markdownlint-cli2?activeTab=versions
ENV PATH="/go/bin:/usr/local/go/bin:${PATH}" \
    TASK_VERSION=v3.54.0 \
    GOLANGCI_LINT_VERSION=v2.15.0 \
    ACTIONLINT_VERSION=1.7.12 \
    HADOLINT_VERSION=2.15.1 \
    ORAS_VERSION=1.3.6 \
    CRANE_VERSION=v0.22.1 \
    KUBECTL_VERSION=v1.37.1 \
    K3D_VERSION=v5.9.0 \
    SOPS_VERSION=v3.13.3 \
    AGE_VERSION=v1.3.2 \
    AGE_SHA256=cbe24006683f8eb669266162894b9a522a1af52f2665fbc63a4bb032ed26ac10 \
    NODE_MAJOR=24 \
    MARKDOWNLINT_CLI2_VERSION=0.24.0

RUN test "$(dpkg --print-architecture)" = "amd64" \
    || (echo "This devcontainer currently supports amd64 only." && exit 1)

# Install Task
RUN curl -fsSL "https://github.com/go-task/task/releases/download/${TASK_VERSION}/task_linux_amd64.tar.gz" \
    | tar -xzO task > /usr/local/bin/task \
    && chmod +x /usr/local/bin/task

# Install golangci-lint
RUN curl -sSfL https://golangci-lint.run/install.sh \
    | sh -s -- -b /usr/local/bin ${GOLANGCI_LINT_VERSION}

# Install actionlint (GitHub Actions workflows)
RUN asset="actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz" \
    && base="https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}" \
    && tmpdir="$(mktemp -d)" \
    && curl -fsSL "${base}/${asset}" -o "${tmpdir}/${asset}" \
    && curl -fsSL "${base}/actionlint_${ACTIONLINT_VERSION}_checksums.txt" -o "${tmpdir}/checksums.txt" \
    && cd "${tmpdir}" \
    && grep "  ${asset}$" checksums.txt | sha256sum -c - \
    && tar -xzf "${asset}" actionlint \
    && install -m 0755 actionlint /usr/local/bin/actionlint \
    && rm -rf "${tmpdir}"

# Install hadolint (Dockerfiles). Its release ships one combined checksums.sha256 in
# `sha256sum *filename` format (space-asterisk).
RUN asset="hadolint-linux-x86_64" \
    && base="https://github.com/hadolint/hadolint/releases/download/v${HADOLINT_VERSION}" \
    && tmpdir="$(mktemp -d)" \
    && curl -fsSL "${base}/${asset}" -o "${tmpdir}/${asset}" \
    && curl -fsSL "${base}/checksums.sha256" -o "${tmpdir}/checksums.sha256" \
    && cd "${tmpdir}" \
    && grep " \*${asset}$" checksums.sha256 | sha256sum -c - \
    && install -m 0755 "${asset}" /usr/local/bin/hadolint \
    && rm -rf "${tmpdir}"

# Install oras and crane (checksum-verified).
RUN tmpdir="$(mktemp -d)" && cd "${tmpdir}" \
    && asset="oras_${ORAS_VERSION}_linux_amd64.tar.gz" \
    && base="https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}" \
    && curl -fsSL "${base}/${asset}" -o "${asset}" \
    && curl -fsSL "${base}/oras_${ORAS_VERSION}_checksums.txt" -o oras-checksums.txt \
    && grep "  ${asset}$" oras-checksums.txt | sha256sum -c - \
    && tar -xzf "${asset}" oras \
    && install -m 0755 oras /usr/local/bin/oras \
    && asset="go-containerregistry_Linux_x86_64.tar.gz" \
    && base="https://github.com/google/go-containerregistry/releases/download/${CRANE_VERSION}" \
    && curl -fsSL "${base}/${asset}" -o "${asset}" \
    && curl -fsSL "${base}/checksums.txt" -o crane-checksums.txt \
    && grep "  ${asset}$" crane-checksums.txt | sha256sum -c - \
    && tar -xzf "${asset}" crane \
    && install -m 0755 crane /usr/local/bin/crane \
    && cd / && rm -rf "${tmpdir}"

# Install kubectl, k3d, sops and age (checksum-verified).
RUN tmpdir="$(mktemp -d)" && cd "${tmpdir}" \
    && curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o kubectl \
    && echo "$(curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl.sha256")  kubectl" \
       | sha256sum -c - \
    && install -m 0755 kubectl /usr/local/bin/kubectl \
    && base="https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}" \
    && curl -fsSL "${base}/k3d-linux-amd64" -o k3d-linux-amd64 \
    && curl -fsSL "${base}/checksums.txt" -o k3d-checksums.txt \
    && grep "  _dist/k3d-linux-amd64$" k3d-checksums.txt | sed 's|_dist/||' | sha256sum -c - \
    && install -m 0755 k3d-linux-amd64 /usr/local/bin/k3d \
    && asset="sops-${SOPS_VERSION}.linux.amd64" \
    && base="https://github.com/getsops/sops/releases/download/${SOPS_VERSION}" \
    && curl -fsSL "${base}/${asset}" -o "${asset}" \
    && curl -fsSL "${base}/sops-${SOPS_VERSION}.checksums.txt" -o sops-checksums.txt \
    && grep "  ${asset}$" sops-checksums.txt | sha256sum -c - \
    && install -m 0755 "${asset}" /usr/local/bin/sops \
    && asset="age-${AGE_VERSION}-linux-amd64.tar.gz" \
    && curl -fsSL "https://github.com/FiloSottile/age/releases/download/${AGE_VERSION}/${asset}" -o "${asset}" \
    && echo "${AGE_SHA256}  ${asset}" | sha256sum -c - \
    && tar -xzf "${asset}" \
    && install -m 0755 age/age age/age-keygen /usr/local/bin/ \
    && cd / && rm -rf "${tmpdir}"

# Install Node.js, in the ci stage because markdownlint-cli2 is an npm package and `task lint`
# runs it in CI.
RUN curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
       | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && chmod a+r /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" \
       > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update \
    && apt-get -y install --no-install-recommends nodejs \
    && apt-get autoremove -y \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g "markdownlint-cli2@${MARKDOWNLINT_CLI2_VERSION}" \
    && npm cache clean --force

WORKDIR /workspaces

# A godev group shares /go between root (image build) and vscode (development).
# This MUST happen before any `go mod download` or `go install`.
RUN groupadd --gid 2000 godev \
    && chgrp -R godev /go \
    && chmod -R 2775 /go \
    && setfacl -d -m g:godev:rwx /go \
    && setfacl -d -m u::rwx /go \
    && setfacl -d -m o::rx /go

# Warm golangci-lint's cache on an empty module, so the first `task lint` does not download it.
RUN mkdir -p /tmp/golangci-init && cd /tmp/golangci-init \
    && go mod init example.com/init \
    && printf 'package main\n\nfunc main() {}\n' > main.go \
    && golangci-lint run --timeout=5m || true \
    && cd / && rm -rf /tmp/golangci-init \
    && go clean -cache

# Login shells get Go on PATH and the optional, gitignored repo-root .env.
# The single-quoted lines are written verbatim into the profile scripts; no build-time expansion.
# hadolint ignore=SC2016
RUN printf '%s\n' \
    'export PATH="/go/bin:/usr/local/go/bin:${PATH}"' \
    > /etc/profile.d/go-toolchain-path.sh \
    && chmod 0644 /etc/profile.d/go-toolchain-path.sh \
    && printf '%s\n' \
    'workspace_dir="${PROJECT_PATH:-}"' \
    'if [ -n "${workspace_dir}" ] && [ -f "${workspace_dir}/.env" ]; then' \
    '  set -a' \
    '  . "${workspace_dir}/.env"' \
    '  set +a' \
    'fi' \
    > /etc/profile.d/workspace-dotenv.sh \
    && chmod 0644 /etc/profile.d/workspace-dotenv.sh

# Pre-download modules last, so a go.mod change does not reinstall the tools above.
# go.sum* because a module without dependencies has no go.sum.
COPY go.mod go.sum* ./
RUN go mod download

ENV DEBIAN_FRONTEND=dialog

CMD ["/bin/bash"]

# Stage 2: the development container (https://github.com/devcontainers/spec)
FROM ci AS dev

USER root

ENV DEBIAN_FRONTEND=noninteractive

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Dev-only tool versions: editor and debugging, never used by CI.
#
# DLV_VERSION -> https://github.com/go-delve/delve/releases
# GOPLS_VERSION -> https://github.com/golang/tools/releases
# STATICCHECK_VERSION -> https://github.com/dominikh/go-tools/releases
# CODEX_VERSION -> https://www.npmjs.com/package/@openai/codex?activeTab=versions (the `latest`
#   dist-tag; the others are alpha and per-platform builds)
# CLAUDE_CODE_VERSION -> https://www.npmjs.com/package/@anthropic-ai/claude-code?activeTab=versions
#   (the `stable` dist-tag). DISABLE_AUTOUPDATER keeps it on this pin: the vscode user cannot
#   write the root-owned global npm prefix anyway, so a self-update could only fail.
ENV DLV_VERSION=v1.27.2 \
    GOPLS_VERSION=v0.24.0 \
    STATICCHECK_VERSION=v0.8.2 \
    CODEX_VERSION=0.166.0 \
    CLAUDE_CODE_VERSION=2.1.309 \
    DISABLE_AUTOUPDATER=1

RUN apt-get update \
    && apt-get -y install --no-install-recommends bash-completion \
    && apt-get autoremove -y \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Bash completion, and Task completions, in interactive shells.
# hadolint ignore=SC2016
RUN printf '%s\n' \
    '' \
    'if ! shopt -oq posix; then' \
    '  if [ -f /usr/share/bash-completion/bash_completion ]; then' \
    '    . /usr/share/bash-completion/bash_completion' \
    '  elif [ -f /etc/bash_completion ]; then' \
    '    . /etc/bash_completion' \
    '  fi' \
    '  if command -v task >/dev/null 2>&1 && ! complete -p task >/dev/null 2>&1; then' \
    '    eval "$(task --completion bash)"' \
    '  fi' \
    'fi' \
    >> /etc/bash.bashrc

# Delve, gopls and staticcheck for the editor. `task lint` already runs staticcheck's analyzers
# through golangci-lint; this binary only powers live diagnostics.
RUN go install github.com/go-delve/delve/cmd/dlv@${DLV_VERSION} \
    && go install golang.org/x/tools/gopls@${GOPLS_VERSION} \
    && go install honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION} \
    && go clean -cache

# The Codex and Claude Code CLIs. Their logins live in ~/.codex and ~/.claude (+ ~/.claude.json),
# which devcontainer.json keeps on volumes, so they survive a rebuild.
RUN npm install -g "@openai/codex@${CODEX_VERSION}" "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
    && npm cache clean --force

# A non-root vscode user in the godev and docker groups.
RUN groupadd --gid 1000 vscode \
    && useradd --uid 1000 --gid vscode --shell /bin/bash --create-home vscode \
    && echo 'vscode ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers.d/vscode \
    && chmod 0440 /etc/sudoers.d/vscode \
    && usermod -aG godev vscode \
    && groupadd -f docker \
    && usermod -aG docker vscode \
    && chown -R vscode:vscode /workspaces \
    && chmod -R 755 /workspaces

USER vscode

ENV DEBIAN_FRONTEND=dialog

CMD ["/bin/bash"]
