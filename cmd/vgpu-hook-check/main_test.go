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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func image(t *testing.T, files map[string]string, links map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, p), []byte(content), 0o644))
	}
	for p, target := range links {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755))
		require.NoError(t, os.Symlink(target, filepath.Join(root, p)))
	}
	return root
}

func TestCheck(t *testing.T) {
	ubuntu := image(t, map[string]string{
		"lib/x86_64-linux-gnu/ld-linux-x86-64.so.2": "",
		"lib/x86_64-linux-gnu/libc.so.6":            "GLIBC_2.2.5 GLIBC_2.34 GLIBC_2.39",
	}, map[string]string{"lib64/ld-linux-x86-64.so.2": "/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2"})
	assert.NoError(t, check(ubuntu))

	ubi := image(t, map[string]string{"usr/lib64/ld-linux-x86-64.so.2": "", "usr/lib64/libc.so.6": "GLIBC_2.34"},
		map[string]string{"lib64": "usr/lib64"})
	assert.NoError(t, check(ubi))

	old := image(t, map[string]string{"lib64/ld-linux-x86-64.so.2": "", "lib64/libc-2.28.so": "GLIBC_2.17 GLIBC_2.28"},
		map[string]string{"lib64/libc.so.6": "/lib64/libc-2.28.so"})
	assert.ErrorContains(t, check(old), "glibc 2.28")

	alpine := image(t, map[string]string{"lib/ld-musl-x86_64.so.1": ""}, nil)
	assert.ErrorContains(t, check(alpine), "musl")

	assert.ErrorContains(t, check(image(t, map[string]string{"app": ""}, nil)), "no glibc dynamic loader")

	// Links that point out of the image stay inside it, as they would in the container.
	absDir := image(t, map[string]string{"usr/lib64/ld-linux-x86-64.so.2": "", "usr/lib64/libc.so.6": "GLIBC_2.28"},
		map[string]string{"lib64": "/usr/lib64"})
	assert.ErrorContains(t, check(absDir), "glibc 2.28", "lib64 -> /usr/lib64 is read inside the image")
	escape := image(t, map[string]string{"lib64/ld-linux-x86-64.so.2": "", "etc/passwd": "GLIBC_2.20"},
		map[string]string{"lib64/libc.so.6": "../../../../../../etc/passwd"})
	assert.ErrorContains(t, check(escape), "glibc 2.20", "../ above the root stays at the root")
}

func TestResolveInRoot(t *testing.T) {
	root := image(t, map[string]string{"usr/lib/x": ""}, map[string]string{"lib": "usr/lib", "abs": "/usr/lib", "loop": "loop"})
	for rel, want := range map[string]string{"lib/x": "usr/lib/x", "abs/x": "usr/lib/x", "../../lib/x": "usr/lib/x", "missing/x": "missing/x"} {
		got, err := resolveInRoot(root, rel)
		require.NoError(t, err, rel)
		assert.Equal(t, filepath.Join(root, want), got, rel)
	}
	_, err := resolveInRoot(root, "loop/x")
	assert.ErrorContains(t, err, "too many symlinks")
}

func TestContainerRoot(t *testing.T) {
	bundle := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "config.json"), []byte(`{"root":{"path":"rootfs"}}`), 0o644))
	root, err := containerRoot(strings.NewReader(`{"ociVersion":"1.2.0","id":"c","status":"creating","bundle":"` + bundle + `"}`))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(bundle, "rootfs"), root)
}
