// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cassava/repoctl/pacman"
	"github.com/cassava/repoctl/pacman/alpm"
)

// BuildOptions controls how makepkg is invoked.
type BuildOptions struct {
	// Sign makes makepkg create package signatures.
	Sign bool
	// Key selects the GPG key used for signing.
	Key string
	// Flags are additional arguments passed to makepkg.
	Flags []string
}

// Build runs makepkg in dir with packages written to pkgdest.
//
// Standard input, output, and error are inherited so that the user can
// follow the build and answer any remaining prompts.
func Build(dir, pkgdest string, opts BuildOptions) error {
	args := []string{"--noconfirm"}
	if opts.Sign {
		args = append(args, "--sign")
	}
	if opts.Key != "" {
		args = append(args, "--key", opts.Key)
	}
	args = append(args, opts.Flags...)

	cmd := exec.Command("makepkg", args...)
	cmd.Dir = dir
	cmd.Env = envWith("PKGDEST", pkgdest)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("makepkg failed in %s: %w", dir, err)
	}
	return nil
}

// envWith returns the current environment with key set to value.
func envWith(key, value string) []string {
	prefix := key + "="
	env := os.Environ()
	filtered := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		filtered = append(filtered, kv)
	}
	return append(filtered, prefix+value)
}

// ReadArtifacts reads all package files that makepkg produced in dir.
// Signature files are not packages and are therefore skipped; they are
// picked up automatically when the package is added to the repository.
func ReadArtifacts(dir string) (pacman.Packages, error) {
	var pkgs pacman.Packages
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !alpm.HasPackageFormat(path) {
			return nil
		}

		pkg, err := pacman.Read(path)
		if err != nil {
			return fmt.Errorf("read package %s: %w", path, err)
		}
		pkgs = append(pkgs, pkg)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Sort(pkgs)
	return pkgs, nil
}
