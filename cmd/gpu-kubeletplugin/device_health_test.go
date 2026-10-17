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
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func TestHealthReport(t *testing.T) {
	pciDevicesRoot = t.TempDir()
	t.Cleanup(func() { pciDevicesRoot = "/sys/bus/pci/devices" })
	require.NoError(t, os.MkdirAll(filepath.Join(pciDevicesRoot, "0000:06:00.0"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pciDevicesRoot, "0000:06:00.0", "gpu_busy_percent"), []byte("0\n"), 0o644))

	ok := &AmdGpuInfo{PCIAddress: "0000:06:00.0"}
	gone := &AmdGpuInfo{PCIAddress: "0000:07:00.0"}
	state := &DeviceState{allocatable: AllocatableDevices{
		"gpu-1-129":    {AmdGpu: ok},
		"gpu-2-130":    {AmdGpu: gone},
		"gpu-2-130-p0": {AmdPartition: &AmdPartitionInfo{Parent: gone}},
		"vfio-0":       {Vfio: &AmdGpuVFIOInfo{}},
	}}

	report := state.healthReport("node-a")
	require.Len(t, report.Devices, 3, "VFIO devices are not reported")
	byName := map[string]kubeletplugin.DeviceHealth{}
	for _, d := range report.Devices {
		assert.Equal(t, "node-a", d.PoolName)
		assert.False(t, d.LastUpdated.IsZero())
		byName[d.DeviceName] = d
	}
	assert.Equal(t, kubeletplugin.HealthStatusHealthy, byName["gpu-1-129"].Health)
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, byName["gpu-2-130"].Health)
	assert.Contains(t, byName["gpu-2-130"].Message, "0000:07:00.0")
	assert.Equal(t, kubeletplugin.HealthStatusUnhealthy, byName["gpu-2-130-p0"].Health, "a partition follows its GPU")
}

func TestHealthFromReadError(t *testing.T) {
	read := func(errno syscall.Errno) error {
		return &os.PathError{Op: "read", Path: "gpu_busy_percent", Err: errno}
	}
	for errno, want := range map[syscall.Errno]kubeletplugin.HealthStatus{
		syscall.EBUSY:  kubeletplugin.HealthStatusHealthy, // firmware busy, seen on the RX 9060 XT
		syscall.EAGAIN: kubeletplugin.HealthStatusHealthy,
		syscall.ENOENT: kubeletplugin.HealthStatusUnhealthy,
		syscall.ENODEV: kubeletplugin.HealthStatusUnhealthy,
		syscall.EIO:    kubeletplugin.HealthStatusUnhealthy,
	} {
		got, _ := healthFromReadError("0000:06:00.0", read(errno))
		assert.Equal(t, want, got, errno.Error())
	}
}

func TestWatchHealthStatusSendsAtOnceAndStops(t *testing.T) {
	d := &driver{nodeName: "node-a", state: &DeviceState{allocatable: AllocatableDevices{}}}
	ctx, cancel := context.WithCancel(context.Background())
	reports := make(chan kubeletplugin.DeviceHealthReport)
	done := make(chan error)
	go func() { done <- d.WatchHealthStatus(ctx, reports) }()
	<-reports
	cancel()
	assert.NoError(t, <-done)
}
