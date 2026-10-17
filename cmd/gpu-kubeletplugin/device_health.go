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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

// healthInterval is how often the GPUs are checked. Kubelet marks a device
// Unknown when no report arrives within HealthCheckTimeout (30 s by default).
const healthInterval = 10 * time.Second

// pciDevicesRoot is where sysfs lists PCI devices; a variable for tests.
var pciDevicesRoot = "/sys/bus/pci/devices"

// gpuHealth reads the amdgpu busy counter of the GPU at pciAddr. The read
// fails when the GPU left the bus, its driver was unbound, or it is wedged or
// in reset, which are the states in which a pod can not use it. EBUSY and
// EAGAIN only mean the power management firmware was busy answering, which
// happens on a working GPU.
func gpuHealth(pciAddr string) (kubeletplugin.HealthStatus, string) {
	_, err := os.ReadFile(filepath.Join(pciDevicesRoot, pciAddr, "gpu_busy_percent"))
	return healthFromReadError(pciAddr, err)
}

func healthFromReadError(pciAddr string, err error) (kubeletplugin.HealthStatus, string) {
	switch {
	case err == nil, errors.Is(err, syscall.EBUSY), errors.Is(err, syscall.EAGAIN):
		return kubeletplugin.HealthStatusHealthy, ""
	case errors.Is(err, os.ErrNotExist):
		return kubeletplugin.HealthStatusUnhealthy, fmt.Sprintf("GPU %s is gone from the PCI bus or not bound to amdgpu", pciAddr)
	default:
		return kubeletplugin.HealthStatusUnhealthy, fmt.Sprintf("GPU %s does not answer: %v", pciAddr, err)
	}
}

// healthReport returns the health of every GPU and hardware partition of the
// pool. A partition shares the health of its GPU. VFIO and synthetic
// partition devices are not reported, which kubelet shows as Unknown.
func (s *DeviceState) healthReport(pool string) kubeletplugin.DeviceHealthReport {
	s.Lock()
	addrs := map[string]string{}
	for name, dev := range s.allocatable {
		switch {
		case dev.AmdGpu != nil:
			addrs[name] = dev.AmdGpu.PCIAddress
		case dev.AmdPartition != nil && dev.AmdPartition.Parent != nil:
			addrs[name] = dev.AmdPartition.Parent.PCIAddress
		}
	}
	s.Unlock()

	now := time.Now()
	report := kubeletplugin.DeviceHealthReport{}
	for name, addr := range addrs {
		status, msg := gpuHealth(addr)
		report.Devices = append(report.Devices, kubeletplugin.DeviceHealth{
			PoolName:    pool,
			DeviceName:  name,
			Health:      status,
			LastUpdated: now,
			Message:     msg,
		})
	}
	sort.Slice(report.Devices, func(i, j int) bool { return report.Devices[i].DeviceName < report.Devices[j].DeviceName })
	return report
}

// WatchHealthStatus sends kubelet the health of all devices now and then
// every healthInterval, which also keeps them from timing out to Unknown.
func (d *driver) WatchHealthStatus(ctx context.Context, reports chan<- kubeletplugin.DeviceHealthReport) error {
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case reports <- d.state.healthReport(d.nodeName):
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
