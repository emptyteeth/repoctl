// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scan returns the names of the immediate subdirectories of root that
// contain a PKGBUILD. Hidden directories are skipped.
func Scan(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(root, entry.Name(), "PKGBUILD")
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		dirs = append(dirs, entry.Name())
	}
	sort.Strings(dirs)
	return dirs, nil
}
