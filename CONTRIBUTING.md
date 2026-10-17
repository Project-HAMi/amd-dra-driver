# Contributing

Welcome, and thank you for your interest in contributing to the HAMi
amd-dra-driver. This guide explains how to propose changes.

## Code of Conduct

This project follows the HAMi community
[Code of Conduct](https://github.com/Project-HAMi/community/blob/main/CODE-OF-CONDUCT.md).
By participating you are expected to uphold it.

## Getting Started

- Read the [README](README.md) for what the driver does, how HAMi-DRA installs it, and what was verified on a real GPU.
- The driver is written in Go and uses cgo against the AMD SMI library vendored in `third_party/amd_smi`. Clone with the `amd-hami-core` submodule, which builds the memory hook. The supported build is the container image:

  ```sh
  git clone --recurse-submodules https://github.com/Project-HAMi/amd-dra-driver.git
  docker build -f deployments/container/Dockerfile \
    --build-arg GOLANG_VERSION=$(awk '/^go /{print $2}' go.mod) \
    -t amd-dra-driver:dev .
  ```

  For a local Go build on Linux, as a non-root user:

  ```sh
  LD_LIBRARY_PATH=$PWD/third_party/amd_smi/lib go test ./cmd/... ./pkg/...
  ```

- The code is based on [ROCm/k8s-gpu-dra-driver](https://github.com/ROCm/k8s-gpu-dra-driver). Fixes that are not specific to HAMi are welcome upstream too.

## Finding Something to Work On

- Browse the [open issues](https://github.com/Project-HAMi/amd-dra-driver/issues).
- Issues labeled `good first issue` are a good starting point.
- If you plan to work on something, comment on the issue so others know.

## Filing an Issue

Open a [new issue](https://github.com/Project-HAMi/amd-dra-driver/issues/new/choose)
and include: what you expected, what happened, the GPU model and ROCm version,
and steps to reproduce. For security problems, do not open a public issue;
follow [SECURITY.md](SECURITY.md).

## Pull Request Workflow

1. Fork the repository and create a topic branch from `main`.
2. Make your change. Keep pull requests focused and small where possible.
3. Add or update tests. Run `go vet ./cmd/... ./pkg/...` and `go test ./cmd/... ./pkg/...`.
4. Sign off every commit to certify the [DCO](https://developercertificate.org/):

   ```sh
   git commit -s -m "your message"
   ```

5. Use the pull request template and add a `/kind` label
   (`/kind bug`, `/kind feature`, `/kind documentation`).
6. Open the pull request against `main` and describe what changed and why.
   Link the issue it fixes with `Fixes #<issue>`.

## Code Review

- Maintainers review pull requests and may request changes.
- Automated checks (CI, DCO, CodeQL) must pass.
- Once a reviewer is satisfied they add `/lgtm`, and an approver adds
  `/approve`; the bot then merges the pull request.

Thank you for helping improve the project.
