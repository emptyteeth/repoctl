// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "PKGBUILD")

	if err := os.WriteFile(path, []byte("pkgname=foo\npkgver=1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	second, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint changed without changing PKGBUILD: %q != %q", first.Fingerprint, second.Fingerprint)
	}

	if err := os.WriteFile(path, []byte("pkgname=foo\npkgver=1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == third.Fingerprint {
		t.Fatalf("fingerprint did not change after changing PKGBUILD: %q", first.Fingerprint)
	}
}

func TestDetectDynamicPkgver(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"pkgver() {", true},
		{"pkgver () {", true},
		{"pkgver()", true},
		{"function pkgver {", true},
		{"function pkgver() {", true},
		{"  pkgver() { echo 1.0; }", true},
		{"# pkgver() {", false},
		{"pkgver=1.0", false},
		{"printf 'pkgver() {'", false},
	}

	for _, test := range tests {
		got := detectDynamicPkgver(test.line+"\n") != ""
		if got != test.want {
			t.Errorf("detectDynamicPkgver(%q) = %v, want %v", test.line, got, test.want)
		}
	}
}

func TestInspectMissingPKGBUILD(t *testing.T) {
	if _, err := Inspect(t.TempDir()); err == nil {
		t.Fatal("expected an error for a missing PKGBUILD")
	}
}
