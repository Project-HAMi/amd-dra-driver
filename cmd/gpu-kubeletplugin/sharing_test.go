/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the \"License\");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an \"AS IS\" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
)

// rx9060xt matches the RDNA4 card the GPUSharing gate was tried on.
func rx9060xt() *AmdGpuInfo {
	return &AmdGpuInfo{MemoryBytes: 16304 << 20, ComputeUnits: 32, SimdUnits: 64, cardIndex: 1, renderIndex: 129}
}

func consumed(mem string, cus int64) map[resourceapi.QualifiedName]resource.Quantity {
	return map[resourceapi.QualifiedName]resource.Quantity{
		"memory":       resource.MustParse(mem),
		"computeUnits": *resource.NewQuantity(cus, resource.DecimalSI),
	}
}

func TestSliceEdits(t *testing.T) {
	sl, err := placeGPUSlice(rx9060xt(), consumed("2000Mi", 16), nil)
	require.NoError(t, err)
	assert.Len(t, sl.cus, 16)
	edits := sliceEdits([]*gpuSlice{sl}, "/usr/local/vgpu/libamvgpu.so")
	assert.Equal(t, []string{"HSA_CU_MASK=0:0-15", "HIP_DEVICE_MEMORY_LIMIT_0=2000m", "LD_AUDIT=" + vgpuHookContainerPath}, edits.Env)
	require.Len(t, edits.Mounts, 1)
	assert.Equal(t, "/usr/local/vgpu/libamvgpu.so", edits.Mounts[0].HostPath)
	assert.Equal(t, vgpuHookContainerPath, edits.Mounts[0].ContainerPath)
}

func TestSliceEditsSkipsBusyCUs(t *testing.T) {
	busy := map[int]bool{}
	for cu := 0; cu < 16; cu++ {
		busy[cu] = true
	}
	sl, err := placeGPUSlice(rx9060xt(), consumed("2000Mi", 16), busy)
	require.NoError(t, err)
	assert.Equal(t, 16, sl.cus[0])
	// No hook configured: the CU mask is set, the memory limit is not.
	edits := sliceEdits([]*gpuSlice{sl}, "")
	assert.Equal(t, []string{"HSA_CU_MASK=0:16-31"}, edits.Env)
	assert.Empty(t, edits.Mounts)

	_, err = placeGPUSlice(rx9060xt(), consumed("2000Mi", 17), busy)
	assert.ErrorContains(t, err, "need 17 free CUs, only 16 of 32 are free")
}

func TestSliceEditsWholeGPU(t *testing.T) {
	sl, err := placeGPUSlice(rx9060xt(), consumed("16304Mi", 32), nil)
	require.NoError(t, err)
	assert.Len(t, sl.cus, 32)
	assert.Nil(t, sliceEdits([]*gpuSlice{sl}, "/hook"))

	// A result without consumed capacity (gate off, or an older scheduler) is left alone.
	sl, err = placeGPUSlice(rx9060xt(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, sl)
}

func TestSliceEditsSeveralGPUs(t *testing.T) {
	half, err := placeGPUSlice(rx9060xt(), consumed("2000Mi", 16), nil)
	require.NoError(t, err)
	quarter, err := placeGPUSlice(rx9060xt(), consumed("4000Mi", 8), map[int]bool{0: true})
	require.NoError(t, err)
	// GPU 1 is whole, so it gets no mask and no limit but keeps its index.
	edits := sliceEdits([]*gpuSlice{half, nil, quarter}, "/hook")
	assert.Equal(t, []string{
		"HSA_CU_MASK=0:0-15;2:1-8",
		"HIP_DEVICE_MEMORY_LIMIT_0=2000m",
		"HIP_DEVICE_MEMORY_LIMIT_2=4000m",
		"LD_AUDIT=" + vgpuHookContainerPath,
	}, edits.Env)
	assert.Len(t, edits.Mounts, 1)
	assert.Empty(t, edits.Hooks, "dmem caps these slices, so any image may run")
}

func TestSliceEditsCheckLibcWithoutDmem(t *testing.T) {
	sl, err := placeGPUSlice(rx9060xt(), consumed("2000Mi", 16), nil)
	require.NoError(t, err)
	sl.hookOnly = true
	edits := sliceEdits([]*gpuSlice{sl}, "/usr/local/vgpu/amd-dra/libamvgpu.so")
	require.Len(t, edits.Hooks, 1)
	assert.Equal(t, "createContainer", edits.Hooks[0].HookName)
	assert.Equal(t, "/usr/local/vgpu/amd-dra/vgpu-hook-check", edits.Hooks[0].Path)

	// Without a memory limit there is nothing the hook must enforce.
	cus, err := placeGPUSlice(rx9060xt(), consumed("16304Mi", 16), nil)
	require.NoError(t, err)
	cus.hookOnly = true
	assert.Empty(t, sliceEdits([]*gpuSlice{cus}, "/hook").Hooks)
}

func TestFormatCUs(t *testing.T) {
	assert.Equal(t, "0-3,8,10-11", formatCUs([]int{0, 1, 2, 3, 8, 10, 11}))
	assert.Equal(t, "5", formatCUs([]int{5}))
}

func TestShareGPUDevice(t *testing.T) {
	device := rx9060xt().GetDevice()
	shareGPUDevice(&device, 32)
	assert.True(t, *device.AllowMultipleAllocations)
	cu := device.Capacity["computeUnits"].RequestPolicy
	assert.Equal(t, int64(32), cu.Default.Value())
	assert.Equal(t, int64(1), cu.ValidRange.Step.Value())
	assert.Equal(t, int64(0), device.Capacity["simdUnits"].RequestPolicy.Default.Value())
	// Memory stays unconstrained, so a request without it takes the whole GPU.
	assert.Nil(t, device.Capacity["memory"].RequestPolicy)
}

func TestApplyGPUSliceAvoidsPreparedShares(t *testing.T) {
	state := &DeviceState{
		allocatable:      AllocatableDevices{"gpu-1-129": {AmdGpu: rx9060xt()}},
		vgpuHookHostPath: "/usr/local/vgpu/libamvgpu.so",
	}
	first := make([]int, 16)
	for i := range first {
		first[i] = i
	}
	prepared := PreparedClaims{
		"claim-a": {{Device: drapbv1.Device{DeviceName: "gpu-1-129"}, ComputeUnits: first}},
	}
	device := &PreparedDevice{Device: drapbv1.Device{DeviceName: "gpu-1-129"}}
	result := &resourceapi.DeviceRequestAllocationResult{Device: "gpu-1-129", ConsumedCapacity: consumed("2000Mi", 16)}

	require.NoError(t, state.applyGPUSlice(device, result, prepared, nil, nil))
	assert.Equal(t, 16, device.ComputeUnits[0])
	state.applySliceEdits(PreparedDevices{device})
	assert.Contains(t, device.ContainerEdits.Env, "HSA_CU_MASK=0:16-31")
}

func TestApplySliceEditsOrdersByRenderNode(t *testing.T) {
	gpu := func(render int) *AmdGpuInfo {
		g := rx9060xt()
		g.renderIndex = render
		return g
	}
	state := &DeviceState{
		allocatable: AllocatableDevices{
			"gpu-2-130": {AmdGpu: gpu(130)},
			"gpu-1-129": {AmdGpu: gpu(129)},
		},
		vgpuHookHostPath: "/hook",
	}
	// The claim lists the second card first; the container numbers them by render node.
	second := &PreparedDevice{Device: drapbv1.Device{DeviceName: "gpu-2-130"}}
	first := &PreparedDevice{Device: drapbv1.Device{DeviceName: "gpu-1-129"}}
	for _, d := range []*PreparedDevice{second, first} {
		result := &resourceapi.DeviceRequestAllocationResult{Device: d.DeviceName, ConsumedCapacity: consumed("2000Mi", 16)}
		require.NoError(t, state.applyGPUSlice(d, result, nil, nil, nil))
	}
	second.slice.memMiB = 3000
	state.applySliceEdits(PreparedDevices{second, first})

	require.NotNil(t, first.ContainerEdits)
	assert.Nil(t, second.ContainerEdits, "the edits go on one device only")
	assert.Equal(t, []string{
		"HSA_CU_MASK=0:0-15;1:0-15",
		"HIP_DEVICE_MEMORY_LIMIT_0=2000m",
		"HIP_DEVICE_MEMORY_LIMIT_1=3000m",
		"LD_AUDIT=" + vgpuHookContainerPath,
	}, first.ContainerEdits.Env)
}

func TestInstallVGPUHook(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bundled.so")
	dst := filepath.Join(dir, "host", "vgpu", "libamvgpu.so")
	require.NoError(t, os.WriteFile(src, []byte("hook-v1"), 0o644))

	require.NoError(t, installVGPUHook(src, dst))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "hook-v1", string(got))
	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o555), info.Mode().Perm())

	// An updated image replaces the installed hook.
	require.NoError(t, os.WriteFile(src, []byte("hook-v2"), 0o644))
	require.NoError(t, installVGPUHook(src, dst))
	got, _ = os.ReadFile(dst)
	assert.Equal(t, "hook-v2", string(got))

	// Without a bundled hook the one on the node is kept.
	require.NoError(t, installVGPUHook(filepath.Join(dir, "missing.so"), dst))
	got, _ = os.ReadFile(dst)
	assert.Equal(t, "hook-v2", string(got))
	entries, _ := os.ReadDir(filepath.Dir(dst))
	assert.Len(t, entries, 1, "no temporary files are left behind")
}

func TestGetDevicePublishesUUID(t *testing.T) {
	gpu := rx9060xt()
	assert.NotContains(t, gpu.GetDevice().Attributes, resourceapi.QualifiedName("uuid"))
	gpu.UUID = "33ff7590-0000-1000-8021-340866ba8c47"
	assert.Equal(t, "33ff7590-0000-1000-8021-340866ba8c47", *gpu.GetDevice().Attributes["uuid"].StringValue)
}

func TestGPUBrand(t *testing.T) {
	assert.Equal(t, "Radeon", gpuBrand("AMD_Radeon_RX_9060_XT"))
	assert.Equal(t, "Instinct", gpuBrand("AMD_Instinct_MI300X_OAM"))
	assert.Equal(t, "", gpuBrand(""))

	gpu := rx9060xt()
	gpu.ProductName = "AMD_Radeon_RX_9060_XT"
	assert.Equal(t, "Radeon", *gpu.GetDevice().Attributes["brand"].StringValue)
}
