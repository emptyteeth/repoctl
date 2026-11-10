// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeTestPackage(t, filepath.Join(dir, "foo-1.0-1-any.pkg.tar.gz"), "foo", "1.0-1")
	if err := os.WriteFile(filepath.Join(dir, "foo-1.0-1-any.pkg.tar.gz.sig"), []byte("signature"), 0o644); err != nil {
		t.Fatal(err)
	}

	pkgs, err := ReadArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("ReadArtifacts returned %d packages, want 1", len(pkgs))
	}
	if pkgs[0].Name != "foo" {
		t.Errorf("Name = %q, want %q", pkgs[0].Name, "foo")
	}
	if pkgs[0].Version != "1.0-1" {
		t.Errorf("Version = %q, want %q", pkgs[0].Version, "1.0-1")
	}
}

func TestBuild(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("makepkg refuses to run as root")
	}
	if _, err := exec.LookPath("makepkg"); err != nil {
		t.Skip("makepkg is not installed")
	}
	if _, err := exec.LookPath("fakeroot"); err != nil {
		t.Skip("fakeroot is not installed")
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "PKGBUILD"), `pkgname=repoctl-test-pkg
pkgver=1.0
pkgrel=1
arch=('any')
pkgdesc="test package"
url="https://example.com"
license=('MIT')
package() {
  mkdir -p "$pkgdir/usr/share/repoctl-test"
  echo test > "$pkgdir/usr/share/repoctl-test/test"
}
`)

	dest := t.TempDir()
	if err := Build(dir, dest, BuildOptions{}); err != nil {
		t.Fatal(err)
	}

	pkgs, err := ReadArtifacts(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("build produced %d packages, want 1", len(pkgs))
	}
	if pkgs[0].Name != "repoctl-test-pkg" {
		t.Errorf("Name = %q, want %q", pkgs[0].Name, "repoctl-test-pkg")
	}
}

func writeTestPackage(t *testing.T, path, name, version string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	info := []byte("pkgname = " + name + "\n" +
		"pkgbase = " + name + "\n" +
		"pkgver = " + version + "\n" +
		"arch = any\n")
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: ".PKGINFO",
		Mode: 0o644,
		Size: int64(len(info)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(info); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
}
