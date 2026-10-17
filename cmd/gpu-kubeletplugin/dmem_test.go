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
)

func fakeCgroupRoot(t *testing.T, controllers string) string {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte(controllers), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dmem.capacity"), []byte("drm/0000:06:00.0/vram 17095983104\n"), 0o644))
	return root
}

func TestDmemRegion(t *testing.T) {
	root := fakeCgroupRoot(t, "cpuset cpu io memory hugetlb pids rdma misc dmem\n")
	region, ok := dmemRegion(root, "0000:06:00.0")
	assert.True(t, ok)
	assert.Equal(t, "drm/0000:06:00.0/vram", region)
	_, ok = dmemRegion(root, "0000:07:00.0")
	assert.False(t, ok, "a GPU without a registered region")
	_, ok = dmemRegion(fakeCgroupRoot(t, "cpuset cpu io memory\n"), "0000:06:00.0")
	assert.False(t, ok, "no dmem controller")
}

func TestSetPodDmemMax(t *testing.T) {
	root := t.TempDir()
	pod := filepath.Join(root, "kubepods.slice", "kubepods-besteffort.slice", "kubepods-besteffort-podab1b3034_4b59_455e_843c_713285de3985.slice")
	require.NoError(t, os.MkdirAll(pod, 0o755))
	require.NoError(t, setPodDmemMax(root, "ab1b3034-4b59-455e-843c-713285de3985", "drm/0000:06:00.0/vram", 2097152000))
	got, err := os.ReadFile(filepath.Join(pod, "dmem.max"))
	require.NoError(t, err)
	assert.Equal(t, "drm/0000:06:00.0/vram 2097152000\n", string(got))
}
