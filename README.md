# HAMi AMD DRA Driver

[![CI](https://github.com/Project-HAMi/amd-dra-driver/actions/workflows/ci.yml/badge.svg)](https://github.com/Project-HAMi/amd-dra-driver/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/Project-HAMi/amd-dra-driver/badge)](https://securityscorecards.dev/viewer/?uri=github.com/Project-HAMi/amd-dra-driver)
[![License](https://img.shields.io/github/license/Project-HAMi/amd-dra-driver)](LICENSE)

A Kubernetes Dynamic Resource Allocation (DRA) driver that lets [HAMi](https://github.com/Project-HAMi/HAMi) share AMD GPUs between pods. Several ResourceClaims can each take a slice of one GPU, and each pod is limited to its share of the GPU's compute units and memory. It is the driver behind the `amd` vendor of [HAMi-DRA](https://github.com/Project-HAMi/HAMi-DRA).

It is based on [ROCm/k8s-gpu-dra-driver](https://github.com/ROCm/k8s-gpu-dra-driver) and keeps its device discovery, partitions and VFIO support. This repository adds the `GPUSharing` feature gate and HAMi's memory hook from [amd-hami-core](https://github.com/Project-HAMi/amd-hami-core). The device plugin path for the same sharing is [amd-device-plugin](https://github.com/Project-HAMi/amd-device-plugin).

## What it does

- With `GPUSharing`, publishes each whole GPU with `allowMultipleAllocations` and consumable `memory` and `computeUnits`. A claim that asks for no capacity still takes the whole GPU.
- Gives each slice a free set of CUs through `HSA_CU_MASK`. The CUs are kept in the driver checkpoint, so later slices and a driver restart avoid them.
- Limits each slice's memory with `HIP_DEVICE_MEMORY_LIMIT_0`, enforced by `libamvgpu.so`. The hook is built into the image and copied to `--vgpu-hook-host-path` on the node at startup.
- Without `GPUSharing` it behaves like the upstream driver.

**Known gaps:**

- One shared GPU per container: the limits address the GPU as device 0.
- A process that re-execs itself without `LD_AUDIT` has no memory limit. There is no kernel-level (dmem) cap yet.
- A node runs either this driver or the HAMi AMD device plugin, chosen with the label `hami.io/amd-mode=dra`. The HAMi scheduler does not see DRA claims: with two DRA slices on CUs 0-15 and 16-31, a device plugin pod on the same node was still given CUs 0-15. The charts of HAMi-DRA and amd-device-plugin follow the label. Keep `--vgpu-hook-host-path` away from the device plugin's `/usr/local/vgpu/libamvgpu.so`.
- HAMi-DRA converts `amd.com/gpu` pods in every namespace it watches, so in those namespaces AMD pods only land on DRA nodes.
- The driver publishes no `uuid` attribute, and an empty `productName` on cards without one in sysfs, so HAMi-DRA's `amd.com/use-gpu-uuid` and `amd.com/use-gputype` do not match yet.

## Install

The cluster needs DRA with the `DRAConsumableCapacity` feature gate, and CDI enabled in the container runtime. The driver runs privileged, so its namespace must allow privileged pods. Hand a node to the driver with:

```bash
kubectl label node <node> hami.io/amd-mode=dra
```

With HAMi-DRA, `drivers.amd.enabled` installs this driver and converts `amd.com/gpu`, `amd.com/gpumem` and `amd.com/gpucores` into claims. This chart option is in [HAMi-DRA#172](https://github.com/Project-HAMi/HAMi-DRA/pull/172) and not released yet:

```bash
helm install hami-dra charts/hami-dra -n hami-dra \
  --set "deviceVendors={amd}" \
  --set amdReferenceComputeUnits=32 \
  --set drivers.nvidia.enabled=false \
  --set drivers.amd.enabled=true
```

`amdReferenceComputeUnits` is the CU count of one GPU, used to turn `amd.com/gpucores` percent into CUs.

On its own, with this repository's chart:

```bash
helm install amd-dra-driver ./helm-charts-k8s -n kube-amd-gpu --create-namespace \
  --set image.repository=ghcr.io/project-hami/amd-dra-driver \
  --set image.tag=main \
  --set kubeletPlugin.nodeSelector."hami\.io/amd-mode"=dra \
  --set featureGates.GPUSharing=true \
  --set kubeletPlugin.vgpuHookHostPath=/usr/local/vgpu/amd-dra/libamvgpu.so
```

A claim for a slice of one GPU:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: amd-slice
spec:
  spec:
    devices:
      requests:
      - name: gpu
        exactly:
          deviceClassName: gpu.amd.com
          capacity:
            requests:
              memory: 2000Mi
              computeUnits: "16"
```

Flags, feature gates and the upstream install guide are in [docs/cli-options.md](docs/cli-options.md) and [docs/installation.md](docs/installation.md).

## Build

```bash
git clone --recurse-submodules https://github.com/Project-HAMi/amd-dra-driver.git
cd amd-dra-driver
docker build -f deployments/container/Dockerfile \
  --build-arg GOLANG_VERSION=$(awk '/^go /{print $2}' go.mod) \
  -t amd-dra-driver .
```

## Test

```bash
LD_LIBRARY_PATH=$PWD/third_party/amd_smi/lib go test ./cmd/... ./pkg/...
```

Run as a non-root user: `TestBindToDriver/bind_failure_clears_driver_override` from upstream fails as root.

## Verified on

- AMD Radeon RX 9060 XT (gfx1200, 16 GiB, 32 CUs), one GPU, RKE2 v1.36.3, kernel 7.0, ROCm 7.2.4 images, with HAMi-DRA#172. Two `amd.com/gpumem: 2000` and `amd.com/gpucores: 50` pods ran on CUs 0-15 and 16-31, each was stopped at 2000 MiB, a third waited until one was deleted, and slices kept their CUs across a driver restart. Two Ollama pods (`llama3.2:3b`, `amd.com/gpumem: 4000`, `amd.com/gpucores: 50`) each saw a 3.9 GiB GPU and ran fully on it, at 48 to 75 tokens/s alone and 24 to 36 each when both ran at once.
- Not run yet: Instinct GPUs, partitioned GPUs, several GPUs per node.

## Upstream

Changes from [ROCm/k8s-gpu-dra-driver](https://github.com/ROCm/k8s-gpu-dra-driver) `develop` are merged in from the `upstream` remote. The driver name `gpu.amd.com` and the Go module path are unchanged to keep those merges simple, and the ROCm-only workflows are not kept.
