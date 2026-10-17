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

// The kernel dmem cgroup controller caps the VRAM of a GPU slice in the
// driver, so a process that drops LD_AUDIT still cannot take more than its
// share. Ported from HAMi's AMD device plugin (internal/pkg/dmem). Lowering
// dmem.max below current use does not reclaim memory, so the cap has to land
// before the workload allocates; it is written as soon as the pod cgroup
// exists, which is before the containers start.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	klog "k8s.io/klog/v2"
)

const dmemCgroupRoot = "/sys/fs/cgroup"

// dmemRegion returns the dmem VRAM region of the GPU at pciAddr (e.g.
// "drm/0000:06:00.0/vram"), or false when the dmem controller is not active or
// the GPU driver registered no region for it.
func dmemRegion(root, pciAddr string) (string, bool) {
	controllers, err := os.ReadFile(filepath.Join(root, "cgroup.controllers"))
	if err != nil || !slices.Contains(strings.Fields(string(controllers)), "dmem") {
		return "", false
	}
	f, err := os.Open(filepath.Join(root, "dmem.capacity"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	want := "drm/" + strings.ToLower(pciAddr) + "/vram"
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) > 0 && strings.ToLower(fields[0]) == want {
			return fields[0], true
		}
	}
	return "", false
}

// podCgroupPaths returns where the systemd cgroup driver puts a pod with uid,
// one path per QoS class. Only one of them exists for a given pod.
func podCgroupPaths(root, uid string) []string {
	slug := strings.ReplaceAll(uid, "-", "_")
	return []string{
		filepath.Join(root, "kubepods.slice", "kubepods-pod"+slug+".slice"),
		filepath.Join(root, "kubepods.slice", "kubepods-burstable.slice", "kubepods-burstable-pod"+slug+".slice"),
		filepath.Join(root, "kubepods.slice", "kubepods-besteffort.slice", "kubepods-besteffort-pod"+slug+".slice"),
	}
}

// setPodDmemMax writes "<region> <limitBytes>" to the pod's dmem.max, waiting
// up to about 20s for the pod cgroup to appear (kubelet prepares claims before
// it creates the pod sandbox).
func setPodDmemMax(root, uid, region string, limitBytes int64) error {
	line := []byte(fmt.Sprintf("%s %d\n", region, limitBytes))
	delays := []time.Duration{100, 200, 400, 800, 1500, 3000, 5000, 9000}
	for i := 0; ; i++ {
		for _, dir := range podCgroupPaths(root, uid) {
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			return os.WriteFile(filepath.Join(dir, "dmem.max"), line, 0o644)
		}
		if i == len(delays) {
			return fmt.Errorf("cgroup of pod %s not found: %w", uid, os.ErrNotExist)
		}
		time.Sleep(delays[i] * time.Millisecond)
	}
}

// capSliceVRAM caps the VRAM of every pod the claim is reserved for, in the
// background so Prepare does not wait for the pod cgroups. A failure leaves
// the slice to the vGPU hook alone and is logged.
func capSliceVRAM(root, pciAddr string, limitBytes int64, podUIDs []string) {
	region, ok := dmemRegion(root, pciAddr)
	if !ok {
		klog.V(2).Infof("dmem: no VRAM region for GPU %s, the slice is capped by the vGPU hook only", pciAddr)
		return
	}
	for _, uid := range podUIDs {
		go func() {
			if err := setPodDmemMax(root, uid, region, limitBytes); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					klog.Warningf("dmem: cap %s at %d bytes for pod %s: %v", region, limitBytes, uid, err)
					return
				}
				klog.Warningf("dmem: %v; the slice is capped by the vGPU hook only", err)
				return
			}
			klog.Infof("dmem: capped %s at %d bytes for pod %s", region, limitBytes, uid)
		}()
	}
}
