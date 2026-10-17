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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	klog "k8s.io/klog/v2"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

// vgpuHookContainerPath is where the vGPU hook is mounted in the container. It
// matches the path HAMi's device plugin uses, so images and docs carry over.
const vgpuHookContainerPath = "/usr/local/vgpu/libamvgpu.so"

// libcCheckName is the CDI hook (cmd/vgpu-hook-check) that stops a slice from
// starting when its image can not load the vGPU hook. It ships next to
// libamvgpu.so in the image and is installed next to it on the node.
const libcCheckName = "vgpu-hook-check"

// installVGPUHook copies the image-bundled hook to dst, which the driver
// container sees at the same path as the host. The copy goes through a
// temporary file and a rename, so a container started meanwhile never maps a
// partly written library. A missing source keeps whatever hook dst already has.
func installVGPUHook(src, dst string) error {
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		klog.Warningf("No bundled vGPU hook at %s; using the hook already at %s", src, dst)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read vGPU hook: %w", err)
	}
	if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create vGPU hook directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".libamvgpu-*.so")
	if err != nil {
		return fmt.Errorf("stage vGPU hook: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("stage vGPU hook: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stage vGPU hook: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o555); err != nil {
		return fmt.Errorf("stage vGPU hook: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("install vGPU hook: %w", err)
	}
	klog.Infof("Installed vGPU hook at %s", dst)
	return nil
}

// gpuSlice is the share of one whole GPU a claim result consumed.
type gpuSlice struct {
	cus     []int // the CUs given to the share
	maskCUs bool  // fewer CUs than the GPU has, so HSA_CU_MASK must limit them
	memMiB  int64 // memory limit in MiB, 0 for the whole memory
	// hookOnly means only the vGPU hook enforces memMiB (no dmem cgroup), so
	// the image must be able to load it.
	hookOnly bool
}

// placeGPUSlice picks the CUs for one share of a whole GPU from the capacity
// its allocation result consumed. busy holds the CUs already given to other
// shares of the same GPU. A result without consumed capacity gets no slice.
func placeGPUSlice(gpu *AmdGpuInfo, consumed map[resourceapi.QualifiedName]resource.Quantity, busy map[int]bool) (*gpuSlice, error) {
	if consumed == nil {
		return nil, nil
	}
	cuCount := int64(gpu.ComputeUnits)
	if q, ok := consumed["computeUnits"]; ok {
		cuCount = q.Value()
	}
	memBytes := int64(gpu.MemoryBytes)
	if q, ok := consumed["memory"]; ok {
		memBytes = q.Value()
	}
	cus, err := pickFreeCUs(gpu.ComputeUnits, int(cuCount), busy)
	if err != nil {
		return nil, err
	}
	slice := &gpuSlice{cus: cus, maskCUs: cuCount < int64(gpu.ComputeUnits)}
	if memBytes < int64(gpu.MemoryBytes) {
		slice.memMiB = max(memBytes>>20, 1)
	}
	return slice, nil
}

// sliceEdits returns the container edits for the slices of one claim, given
// in the order the container sees the GPUs (index 0 first). HSA_CU_MASK takes
// one "<index>:<CUs>" entry per GPU joined by ";", and the hook reads one
// HIP_DEVICE_MEMORY_LIMIT_<index> per GPU. A nil slice is a whole GPU.
//
// The memory cap is enforced by the vGPU hook. Without one there is nothing to
// read the limit, so none is set rather than one that looks enforced.
func sliceEdits(slices []*gpuSlice, hookHostPath string) *cdispec.ContainerEdits {
	var masks, env []string
	checkLibc := false
	for i, sl := range slices {
		if sl == nil {
			continue
		}
		if sl.maskCUs {
			masks = append(masks, fmt.Sprintf("%d:%s", i, formatCUs(sl.cus)))
		}
		if sl.memMiB > 0 && hookHostPath != "" {
			env = append(env, fmt.Sprintf("HIP_DEVICE_MEMORY_LIMIT_%d=%dm", i, sl.memMiB))
			checkLibc = checkLibc || sl.hookOnly
		}
	}
	edits := &cdispec.ContainerEdits{}
	if len(masks) > 0 {
		edits.Env = append(edits.Env, "HSA_CU_MASK="+strings.Join(masks, ";"))
	}
	if len(env) > 0 {
		edits.Env = append(edits.Env, env...)
		edits.Env = append(edits.Env, "LD_AUDIT="+vgpuHookContainerPath)
		edits.Mounts = append(edits.Mounts, &cdispec.Mount{
			HostPath:      hookHostPath,
			ContainerPath: vgpuHookContainerPath,
			Options:       []string{"ro", "nosuid", "nodev", "bind"},
		})
	}
	if checkLibc {
		check := filepath.Join(filepath.Dir(hookHostPath), libcCheckName)
		edits.Hooks = append(edits.Hooks, &cdispec.Hook{HookName: "createContainer", Path: check, Args: []string{libcCheckName}})
	}
	if len(edits.Env) == 0 {
		return nil
	}
	return edits
}

// pickFreeCUs returns the n lowest CU indices below total that are not busy.
func pickFreeCUs(total, n int, busy map[int]bool) ([]int, error) {
	cus := make([]int, 0, n)
	for cu := 0; cu < total && len(cus) < n; cu++ {
		if !busy[cu] {
			cus = append(cus, cu)
		}
	}
	if len(cus) < n {
		return nil, fmt.Errorf("need %d free CUs, only %d of %d are free", n, len(cus), total)
	}
	return cus, nil
}

// formatCUs renders sorted CU indices in HSA_CU_MASK list form, e.g. "0-3,8-11".
func formatCUs(cus []int) string {
	var parts []string
	for i := 0; i < len(cus); {
		j := i
		for j+1 < len(cus) && cus[j+1] == cus[j]+1 {
			j++
		}
		if i == j {
			parts = append(parts, fmt.Sprint(cus[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", cus[i], cus[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}
