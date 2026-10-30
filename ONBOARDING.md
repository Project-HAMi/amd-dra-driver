# Onboarding: Project-HAMi/amd-dra-driver

A short guide for someone who opens the repo for the first time. For what was tested on a real GPU and what is still open, read [STATUS.md](STATUS.md).

## 1. What it is

- A Kubernetes DRA driver (`gpu.amd.com`) that lets [HAMi](https://github.com/Project-HAMi/HAMi) share AMD GPUs between pods. It started from AMD's [ROCm/k8s-gpu-dra-driver](https://github.com/ROCm/k8s-gpu-dra-driver) and keeps its discovery, partitions and VFIO support. Apache 2.0.
- This repository adds the `GPUSharing` feature gate, HAMi's memory hook and a few attributes. Without the gate it behaves like the upstream driver.
- [HAMi-DRA](https://github.com/Project-HAMi/HAMi-DRA) installs it with `drivers.amd.enabled` and turns `amd.com/gpu`, `amd.com/gpumem` and `amd.com/gpucores` requests into ResourceClaims for it.
- The same sharing on the device plugin path is [amd-device-plugin](https://github.com/Project-HAMi/amd-device-plugin). A node runs one of the two.

## 2. How a pod gets its GPU slice

1. The pod asks for `amd.com/gpu: 1`, `amd.com/gpumem: 2000` and `amd.com/gpucores: 50`.
2. The HAMi-DRA webhook removes those and creates a ResourceClaim on `gpu.amd.com` with capacity `memory: 2097152k` and `computeUnits: 16` (50 percent of `amdReferenceComputeUnits`).
3. The Kubernetes scheduler picks a GPU that still has that much capacity left. The driver publishes each whole GPU with `allowMultipleAllocations`, so several claims can share it.
4. When the pod starts, kubelet asks the driver to prepare the claim. The driver picks a free set of CUs and writes a CDI spec with `HSA_CU_MASK=0:<CUs>`, `HIP_DEVICE_MEMORY_LIMIT_0=2000m`, `LD_AUDIT` and the hook mount. It keeps the CUs in its checkpoint, and caps the pod's VRAM in the kernel through `dmem.max`.
5. In the container, the ROCm runtime keeps the work on the given CUs and `libamvgpu.so` stops allocations past the limit.

## 3. Resources a pod can ask for

- Through HAMi-DRA: `amd.com/gpu` (number of GPUs), `amd.com/gpumem` (MiB), `amd.com/gpumem-percentage` (percent of each GPU's memory), `amd.com/gpucores` (percent of the CUs), and the annotations `amd.com/use-gpu-uuid`, `amd.com/nouse-gpu-uuid`, `amd.com/use-gputype` and `amd.com/nouse-gputype`. The type annotations match any part of the name, in any case, as in HAMi. A pod labelled `hami.io/skip-dra: "true"` is not converted and stays on the device plugin. The percentage, the type matching and the label come with HAMi-DRA#172 and #182, merged, not released yet.
- Several GPUs in one request are limited each on its own: one `HSA_CU_MASK` entry and one `HIP_DEVICE_MEMORY_LIMIT_<index>` per GPU. A container should get its shared GPUs from one claim.
- Directly, a ResourceClaim on `gpu.amd.com` with `capacity.requests.memory` and `capacity.requests.computeUnits`. A claim without capacity takes the whole GPU.

## 4. What the driver publishes

- One device per whole GPU, named `gpu-<card>-<renderD>`, with the attributes `type: amdgpu`, `productName`, `brand` (`Radeon` or `Instinct`, from the product name), `uuid` (from AMD SMI), `deviceID`, `numaNode`, the PCI bus ID and PCIe root.
- Capacity `memory`, `computeUnits` and `simdUnits`. With `GPUSharing`, `computeUnits` is consumed in whole CUs and `simdUnits` defaults to 0.
- The health of each GPU, reported to kubelet every 10 s; kubelet shows it in the pod status under `allocatedResourcesStatus`.
- `productName` comes from sysfs, or from the AMD SMI market name where sysfs has none (RDNA cards), with spaces turned into `_`. Where that name is empty or the generic `AMD Radeon Graphics` (AMD SMI says so for the MI325X), the PCI device ID names the Instinct part (`pkg/amdgpu`, `InstinctProductName`), so `brand` is `Instinct` and `amd.com/use-gputype: MI325X` matches.

## 5. amd-hami-core and `libamvgpu.so`

- [amd-hami-core](https://github.com/Project-HAMi/amd-hami-core) is a submodule. The image builds `libamvgpu.so` from it into `/opt/hami/lib/amd/`.
- At startup the driver copies it to `--vgpu-hook-host-path` on the node. Use a path apart from the device plugin's `/usr/local/vgpu/libamvgpu.so`; the charts use `/usr/local/vgpu/amd-dra/libamvgpu.so`.
- The hook is loaded with `LD_AUDIT`, so a process that removes it has no hook limit. The dmem cap (`--dmem-backend`, on by default) still holds where the node has the dmem controller.
- Where there is no dmem cap, the driver also installs `vgpu-hook-check` next to the hook and adds it as a CDI `createContainer` hook to memory slices. It stops a container whose image can not load the hook (musl, or glibc older than 2.34) with a message that says so.

## 6. Install and main settings

The cluster needs DRA with the `DRAConsumableCapacity` feature gate and CDI in the container runtime. The node needs the `amdgpu` driver bound to the GPU, not only loaded: check `lspci -k` and `/dev/kfd`. An Instinct MI325X needed kernel 6.14 with `amdgpu-dkms` 6.16.13, because the in-tree driver of kernels 6.8 and 7.0 did not bind to it, and that DKMS version does not build on 6.17 and 7.0. The dmem memory cap needs kernel 6.14 or newer. The driver runs privileged, so its namespace must allow privileged pods. Hand a node to the driver with:

```bash
kubectl label node <node> hami.io/amd-mode=dra
```

With HAMi-DRA (the `drivers.amd` option is in [HAMi-DRA#172](https://github.com/Project-HAMi/HAMi-DRA/pull/172) and not released yet; until it is, set `webhook.image.tag=latest`, since `v0.2.3` does not know AMD):

```bash
helm install hami-dra charts/hami-dra -n hami-dra \
  --set "deviceVendors={amd}" \
  --set amdReferenceComputeUnits=32 \
  --set drivers.nvidia.enabled=false \
  --set drivers.amd.enabled=true
```

On its own, with this repository's chart:

```bash
helm install amd-dra-driver ./helm-charts-k8s -n kube-amd-gpu --create-namespace \
  --set image.repository=ghcr.io/project-hami/amd-dra-driver \
  --set image.tag=0.4.0 \
  --set kubeletPlugin.nodeSelector."hami\.io/amd-mode"=dra \
  --set featureGates.GPUSharing=true \
  --set kubeletPlugin.vgpuHookHostPath=/usr/local/vgpu/amd-dra/libamvgpu.so
```

Main flags: `--feature-gates GPUSharing=true`, `--vgpu-hook-host-path`, `--vgpu-hook-source` and `--dmem-backend`. All flags are in [docs/cli-options.md](docs/cli-options.md).

## 7. Repo layout

- `cmd/gpu-kubeletplugin`: the driver. `sharing.go` (CU and memory slices, hook install), `dmem.go` (kernel cap), `state.go` (prepare and unprepare), `deviceinfo.go` (what is published), `device_health.go` (health for kubelet).
- `cmd/vgpu-hook-check`: the static CDI hook that checks an image can load the vGPU hook.
- `pkg/amdgpu`, `pkg/amdsmi`: GPU discovery and AMD SMI.
- `amd-hami-core`: the hook, as a submodule.
- `helm-charts-k8s`: the chart. `deployments/container/Dockerfile`: the image.
- `docs`: the upstream guides and the flag reference.

## 8. Build and test

```bash
git clone --recurse-submodules https://github.com/Project-HAMi/amd-dra-driver.git
cd amd-dra-driver
docker build -f deployments/container/Dockerfile \
  -t amd-dra-driver .
LD_LIBRARY_PATH=$PWD/third_party/amd_smi/lib go test ./cmd/... ./pkg/...
```

Run the tests as a non-root user: one upstream VFIO test fails as root. CI pushes `ghcr.io/project-hami/amd-dra-driver` on every merge to `main`, and a `v*` tag publishes the release image. Record changes in [CHANGELOG.md](CHANGELOG.md).

## 9. Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Sign off every commit (DCO).

## 10. Upstream

Changes from ROCm/k8s-gpu-dra-driver `develop` are merged in from an `upstream` remote. The driver name `gpu.amd.com` and the Go module path are unchanged to keep those merges simple. Fixes that are not specific to HAMi are welcome upstream too.
