// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "alpha", "PKGBUILD"), "pkgname=alpha\n")
	writeFile(t, filepath.Join(root, "beta", "PKGBUILD"), "pkgname=beta\n")
	writeFile(t, filepath.Join(root, "not-a-package", "README"), "nothing\n")
	writeFile(t, filepath.Join(root, ".hidden", "PKGBUILD"), "pkgname=hidden\n")
	writeFile(t, filepath.Join(root, "README.md"), "root file\n")

	got, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan = %v, want %v", got, want)
	}
}

func TestScanSkipsSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real", "PKGBUILD"), "pkgname=real\n")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	got, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"real"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan = %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
