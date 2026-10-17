# HAMi AMD DRA Driver

[![CI](https://github.com/Project-HAMi/amd-dra-driver/actions/workflows/ci.yml/badge.svg)](https://github.com/Project-HAMi/amd-dra-driver/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/Project-HAMi/amd-dra-driver/badge)](https://securityscorecards.dev/viewer/?uri=github.com/Project-HAMi/amd-dra-driver)
[![License](https://img.shields.io/github/license/Project-HAMi/amd-dra-driver)](LICENSE)

A Kubernetes Dynamic Resource Allocation (DRA) driver that lets [HAMi](https://github.com/Project-HAMi/HAMi) share AMD GPUs between pods. It limits each pod to its share of the GPU's compute units and memory, and is the driver behind the `amd` vendor of [HAMi-DRA](https://github.com/Project-HAMi/HAMi-DRA).

- [STATUS.md](STATUS.md): what was checked on a real GPU, the problems found and fixed, and what is left.
- [ONBOARDING.md](ONBOARDING.md): how the project works, how to install, build and test, for someone new.
- [CHANGELOG.md](CHANGELOG.md): what each release changed. The current release is `v0.4.0`.

It is based on [ROCm/k8s-gpu-dra-driver](https://github.com/ROCm/k8s-gpu-dra-driver). The library that limits memory inside pods lives in [amd-hami-core](https://github.com/Project-HAMi/amd-hami-core); the device plugin path for the same sharing is [amd-device-plugin](https://github.com/Project-HAMi/amd-device-plugin). Flags and the upstream install guide are in [docs](docs/cli-options.md).
