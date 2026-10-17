# Status

What was checked, what was found and what is left, in plain words. Read [ONBOARDING.md](ONBOARDING.md) to learn how the project works.

## In one minute

- Every change this repository adds to ROCm/k8s-gpu-dra-driver was run on a real AMD GPU, together with HAMi-DRA ([#172](https://github.com/Project-HAMi/HAMi-DRA/pull/172), not merged yet).
- Two pods can share one GPU: each gets its own CUs and its own memory limit, enforced by the hook and by the kernel.
- What is left: the checks that need hardware this machine did not have (end of this file).

## How we tested

- AMD Radeon RX 9060 XT (gfx1200, RDNA4, 16304 MiB, 32 CUs), one GPU, no hardware partitions.
- RKE2 v1.36.3 with `DRAConsumableCapacity` on, systemd cgroup driver, cgroup v2, kernel 7.0.
- ROCm 7.2.4 images for the test pods; Ollama 0.12.5 (`llama3.2:3b`) as a real workload.
- The same node also has an NVIDIA GPU and runs the HAMi AMD device plugin, so the two paths were checked side by side.
- Every check below was run on the node, and the cluster was cleaned up after each run.

## What was found and fixed

- **The released ROCm driver could not publish the GPU.** v1.0.1 read the NVIDIA module version as `driverVersion` on this mixed-vendor node (`580.178.04`), and the API server rejected the ResourceSlice. This repository is based on `develop`, which has the fix (ROCm#117).
- **A slice got the whole GPU.** A GPU without hardware partitions was exclusive: a `gpumem: 2000` pod allocated 15872 MiB and a second pod stayed Pending. Fixed by the `GPUSharing` gate.
- **The hook and the device plugin overwrote each other.** Both wrote `/usr/local/vgpu/libamvgpu.so` with different builds. The DRA driver now installs its bundled hook at `/usr/local/vgpu/amd-dra/libamvgpu.so`.
- **The DRA driver and the device plugin handed out the same GPU.** With two DRA slices on CUs 0-15 and 16-31, a device plugin pod on the same node was given CUs 0-15 again: the HAMi scheduler does not see DRA claims. A node now runs one of them, chosen with the label `hami.io/amd-mode=dra` (HAMi-DRA#172, amd-device-plugin#203).
- **`amd.com/use-gpu-uuid` and `amd.com/use-gputype` could not match.** The driver published no `uuid` and an empty `productName` on this card. Fixed in #2.
- **`env -u LD_AUDIT` escaped the memory limit.** Fixed in #3 with the dmem cgroup cap.

## Every change of this repository

| Change | Result on the RX 9060 XT | Not verified |
|---|---|---|
| `GPUSharing` gate | Two `gpumem: 2000`, `gpucores: 50` pods ran on CUs 0-15 and 16-31, each saw a 2000 MiB GPU and was stopped after 1792 MiB. A spin kernel took 209 ms alone and 208 ms in each pod with both busy. A third slice and a whole-GPU claim waited; deleting a slice freed its CUs for the next one; CU assignments survived a driver restart. A whole-GPU claim alone got no limits and allocated 15872 MiB. | Several GPUs, two shared GPUs in one container |
| Bundled hook (`amd-hami-core` submodule) | Installed at `/usr/local/vgpu/amd-dra/libamvgpu.so` at startup, not rewritten on restart, the device plugin's hook kept its checksum. Slices loaded it and stopped at 1792 MiB. | Nothing |
| #1 README and default hook path | Covered by the end-to-end run below. | Nothing |
| #2 AMD SMI `uuid` and `productName` | ResourceSlice showed `uuid: 33007590-0000-1000-8021-340866ba8c47` and `productName: AMD_Radeon_RX_9060_XT`; claims selecting either were allocated, another product name stayed Pending without a CEL error. The UUID differs in its first byte from the `33ff7590-...` the device plugin registers (different AMD SMI versions). | Instinct GPUs |
| #3 dmem cap | `env -u LD_AUDIT` allocation stopped at 1792 MiB instead of 15872 MiB, `dmem.current` held at 2025971712 bytes, the other slice still got its 1792 MiB, a whole-GPU claim kept `max`. | Non-systemd cgroup drivers |
| #4 repository layout | CI, CodeQL and the Helm checks pass; their commands were run on the node first. | Nothing |
| End to end with HAMi-DRA | One `helm install` of HAMi-DRA with `drivers.amd.enabled` on a node labelled `hami.io/amd-mode=dra`: the slices above, and two Ollama pods (`gpumem: 4000`, `gpucores: 50`) each saw a 3.9 GiB GPU and ran fully on it, at 48 to 75 tokens/s alone and 24 to 36 each together. The HAMi-DRA monitor reported a core ratio of 0.5 per slice. | Nothing |

## What is not verified, and why

- **Instinct GPUs, hardware partitions and several GPUs per node.** The test node has one RDNA4 card. The upstream partition and VFIO code is unchanged.
- **Two shared GPUs in one container.** The limits address the GPU as device 0, so a container should get one shared GPU.
- **A process that drops `LD_AUDIT` on a node without the dmem controller.** The hook is the only memory limit there.
