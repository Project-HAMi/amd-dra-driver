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

import "testing"

// FuzzParseDeviceName feeds arbitrary claim result device names to the parser
// that turns them into DRM card and render node indices.
func FuzzParseDeviceName(f *testing.F) {
	for _, seed := range []string{"gpu-1-129", "gpu-0-128", "gpu--1-x", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		_, _, _ = parseDeviceName(name)
	})
}

// FuzzPickFreeCUs checks that a slice only ever gets free CUs of the GPU, the
// number it asked for, each once.
func FuzzPickFreeCUs(f *testing.F) {
	f.Add(uint8(32), uint8(16), uint64(0xffff))
	f.Add(uint8(32), uint8(17), uint64(0xffff))
	f.Add(uint8(0), uint8(0), uint64(0))
	f.Fuzz(func(t *testing.T, total, n uint8, busyMask uint64) {
		busy := map[int]bool{}
		for cu := 0; cu < 64; cu++ {
			if busyMask&(1<<cu) != 0 {
				busy[cu] = true
			}
		}
		cus, err := pickFreeCUs(int(total), int(n), busy)
		if err != nil {
			return
		}
		if len(cus) != int(n) {
			t.Fatalf("got %d CUs, want %d", len(cus), n)
		}
		seen := map[int]bool{}
		for _, cu := range cus {
			if cu < 0 || cu >= int(total) || busy[cu] || seen[cu] {
				t.Fatalf("bad CU %d in %v (total %d, busy %v)", cu, cus, total, busy)
			}
			seen[cu] = true
		}
		_ = formatCUs(cus)
	})
}
