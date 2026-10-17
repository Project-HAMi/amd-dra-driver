# Changelog

Changes of this repository on top of [ROCm/k8s-gpu-dra-driver](https://github.com/ROCm/k8s-gpu-dra-driver). Images are published as `ghcr.io/project-hami/amd-dra-driver:<version>`.

## Unreleased

### Fixed

- The Security Insights file declares secret scanning with the tool type that OpenSSF baseline scanners read (#20).

## v0.4.0

### Added

- Every GPU of a claim gets its own `HSA_CU_MASK` entry and `HIP_DEVICE_MEMORY_LIMIT_<index>`, numbered in render node order, instead of all limits naming device 0 (#17).
- GPU health is reported to kubelet from `gpu_busy_percent`; a GPU that left the bus or does not answer is `Unhealthy` (#17).
- On a GPU without a dmem region, a static CDI `createContainer` hook (`vgpu-hook-check`) stops a memory slice whose image can not load the vGPU hook: musl, or glibc older than 2.34 (#17).

### Changed

- The image is based on UBI 9.8 minimal (#18), and amd-hami-core is at its current main (#19).

## v0.3.0

### Added

- A `brand` attribute (`Radeon` or `Instinct`) from the product name (#15).

### Fixed

- Topology values are range-checked before integer conversion, images and build tools are pinned by digest, fuzz tests were added, and `go.etcd.io/etcd/client/pkg/v3` was upgraded (#14).

## v0.2.0

### Added

- The image bundles HAMi's vGPU hook from the amd-hami-core submodule and installs it on the node at `--vgpu-hook-host-path`.
- The `uuid` attribute and a `productName` for RDNA cards, from AMD SMI (#2).
- The VRAM of a memory slice is capped in the kernel through the pod's `dmem.max` (#3).
- The layout, CI, security policy and Scorecard of the other HAMi AMD repositories (#4), with README, STATUS.md and ONBOARDING.md (#1, #13).

## v0.1.0

### Added

- The `GPUSharing` feature gate: whole GPUs are published with consumable capacity, and each share gets its own CUs through `HSA_CU_MASK` and a memory limit for HAMi's vGPU hook.
- The image is built by CI and pushed to `ghcr.io/project-hami/amd-dra-driver`.
