// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Fingerprint identifies the contents of a PKGBUILD.
type Fingerprint string

// Inspection is the result of reading a PKGBUILD without executing makepkg.
type Inspection struct {
	Fingerprint Fingerprint
	// Dynamic is set when the PKGBUILD defines pkgver(). Such packages are
	// unsupported because their version can only be determined by building.
	Dynamic       bool
	DynamicReason string
}

var dynamicPkgverPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(?:function\s+)?pkgver\s*\(\s*\)\s*(?:\{|#|$)`),
	regexp.MustCompile(`^function\s+pkgver\s*(?:\{|#|$)`),
}

// Inspect reads dir/PKGBUILD and returns its fingerprint and whether it
// defines a dynamic pkgver() function.
func Inspect(dir string) (*Inspection, error) {
	path := filepath.Join(dir, "PKGBUILD")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	reason := detectDynamicPkgver(string(data))
	return &Inspection{
		Fingerprint:   Fingerprint("sha256:" + hex.EncodeToString(sum[:])),
		Dynamic:       reason != "",
		DynamicReason: reason,
	}, nil
}

// detectDynamicPkgver reports whether the PKGBUILD defines pkgver().
// This is a deliberately small heuristic: parsing shell would require
// executing it, and the only purpose here is to skip unsupported packages.
func detectDynamicPkgver(src string) string {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, pattern := range dynamicPkgverPatterns {
			if pattern.MatchString(line) {
				return "dynamic pkgver()"
			}
		}
	}
	return ""
}

// validateFingerprint is used by tests and state loading to guard against
// malformed cached values.
func validateFingerprint(fp Fingerprint) error {
	if !strings.HasPrefix(string(fp), "sha256:") {
		return fmt.Errorf("unsupported fingerprint format %q", fp)
	}
	return nil
}
