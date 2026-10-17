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

// vgpu-hook-check is a CDI createContainer hook. The driver adds it to a GPU
// slice whose memory limit only the LD_AUDIT hook enforces, because the node
// has no dmem cgroup for the GPU. It stops the container from starting when
// its image can not load the hook: libamvgpu.so needs glibc 2.34 or newer,
// and musl ignores LD_AUDIT, so the pod would run without a memory limit.
//
// It is built static, so it runs on any host. runc runs it before
// pivot_root with the OCI state on stdin; the container root is the bundle's
// root.path.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// minGlibcMinor is libamvgpu's minimum glibc, 2.34 (amd-hami-core
// src/include/glibc_compat.h).
const minGlibcMinor = 34

// The loaders and libc.so.6 locations of glibc and musl distributions: the
// loader is the PT_INTERP path the image's binaries name.
var (
	glibcLoaders = []string{"lib64/ld-linux-x86-64.so.2", "lib/ld-linux-x86-64.so.2", "lib/ld-linux-aarch64.so.1"}
	libcDirs     = []string{"lib/x86_64-linux-gnu", "usr/lib/x86_64-linux-gnu", "lib64", "usr/lib64", "lib/aarch64-linux-gnu", "usr/lib/aarch64-linux-gnu", "lib", "usr/lib"}
	glibcVersion = regexp.MustCompile(`GLIBC_2\.([0-9]+)`)
)

func main() {
	root, err := containerRoot(os.Stdin)
	if err == nil {
		err = check(root)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "amd-dra-driver: %v\n", err)
		os.Exit(1)
	}
}

// containerRoot reads the OCI state and returns the container's root path.
func containerRoot(stdin io.Reader) (string, error) {
	var state struct {
		Bundle string `json:"bundle"`
	}
	if err := json.NewDecoder(stdin).Decode(&state); err != nil {
		return "", fmt.Errorf("reading the OCI state: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(state.Bundle, "config.json"))
	if err != nil {
		return "", fmt.Errorf("reading the OCI config: %w", err)
	}
	var spec struct {
		Root struct {
			Path string `json:"path"`
		} `json:"root"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return "", fmt.Errorf("parsing the OCI config: %w", err)
	}
	if filepath.IsAbs(spec.Root.Path) {
		return spec.Root.Path, nil
	}
	return filepath.Join(state.Bundle, spec.Root.Path), nil
}

// check returns an error unless the image at root has glibc 2.34 or newer.
func check(root string) error {
	const why = "the GPU memory limit of this slice needs the vGPU hook (LD_AUDIT), and this node has no dmem cgroup to enforce it otherwise"
	glibc := false
	for _, l := range glibcLoaders {
		if p, err := resolveInRoot(root, l); err == nil {
			if _, err := os.Stat(p); err == nil {
				glibc = true
			}
		}
	}
	if !glibc {
		lib, err := resolveInRoot(root, "lib")
		if err != nil {
			return fmt.Errorf("reading the image: %w", err)
		}
		if musl, _ := filepath.Glob(filepath.Join(lib, "ld-musl-*")); len(musl) > 0 {
			return fmt.Errorf("the image uses musl, which does not load LD_AUDIT; %s. Use a glibc 2.%d or newer image", why, minGlibcMinor)
		}
		return fmt.Errorf("the image has no glibc dynamic loader; %s. Use a glibc 2.%d or newer image", why, minGlibcMinor)
	}
	for _, dir := range libcDirs {
		p, err := resolveInRoot(root, filepath.Join(dir, "libc.so.6"))
		if err != nil {
			return fmt.Errorf("reading libc.so.6: %w", err)
		}
		data, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading libc.so.6: %w", err)
		}
		minor := -1
		for _, m := range glibcVersion.FindAllSubmatch(data, -1) {
			if v, err := strconv.Atoi(string(m[1])); err == nil && v > minor {
				minor = v
			}
		}
		if minor < minGlibcMinor {
			return fmt.Errorf("the image has glibc 2.%d, the vGPU hook needs 2.%d or newer; %s", minor, minGlibcMinor, why)
		}
		return nil
	}
	return fmt.Errorf("the image has a glibc loader but no libc.so.6; %s", why)
}

// resolveInRoot returns the host path of rel inside the image at root,
// resolving every symlink on the way against root: an absolute link such as
// lib64 -> /usr/lib64 would otherwise lead to the host's files. A missing
// component ends the walk; the caller sees it as a missing file.
func resolveInRoot(root, rel string) (string, error) {
	path := root
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	for hops := 0; len(parts) > 0; {
		part := parts[0]
		parts = parts[1:]
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(path, part)
		if part == ".." {
			next = filepath.Dir(path)
		}
		if next != root && !strings.HasPrefix(next, root+string(filepath.Separator)) {
			next = root // ".." at the root stays at the root, as in a chroot
		}
		target, err := os.Readlink(next)
		if err != nil {
			path = next
			continue
		}
		if hops++; hops > 40 {
			return "", fmt.Errorf("too many symlinks in %s", rel)
		}
		if filepath.IsAbs(target) {
			path = root
		}
		parts = append(strings.Split(filepath.Clean(target), string(filepath.Separator)), parts...)
	}
	return path, nil
}
