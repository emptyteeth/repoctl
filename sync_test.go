// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cassava/repoctl/pacman"
	"github.com/cassava/repoctl/pacman/meta"
	"github.com/cassava/repoctl/pkgbuild"
	"github.com/cassava/repoctl/repo"
)

func TestDecideSyncUnit(t *testing.T) {
	tests := []struct {
		name           string
		metadata       *pkgbuild.Metadata
		repository     map[string]*meta.Package
		files          []string
		allow          bool
		changed        bool
		force          bool
		rebuildChanged bool
		want           syncAction
		wantAdd        bool
		wantPending    bool
	}{
		{
			name:       "new package",
			metadata:   metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{},
			want:       syncBuild,
		},
		{
			name:     "new package with artifact",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": fileOnlyPackage("alpha",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst")),
			},
			files:   []string{"alpha-1.0-1-any.pkg.tar.zst"},
			want:    syncAdd,
			wantAdd: true,
		},
		{
			name:     "up to date",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst")),
			},
			files: []string{"alpha-1.0-1-any.pkg.tar.zst"},
			want:  syncSkip,
		},
		{
			name:     "force rebuilds up to date package",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst")),
			},
			files: []string{"alpha-1.0-1-any.pkg.tar.zst"},
			force: true,
			want:  syncBuild,
		},
		{
			name:     "force does not override downgrade",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst",
					repoFile("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst")),
			},
			files: []string{"alpha-2.0-1-any.pkg.tar.zst"},
			force: true,
			want:  syncDowngrade,
		},
		{
			name:     "rebuild changed package",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst")),
			},
			files:          []string{"alpha-1.0-1-any.pkg.tar.zst"},
			changed:        true,
			rebuildChanged: true,
			want:           syncBuild,
		},
		{
			name:     "changed package is reported pending",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst")),
			},
			files:       []string{"alpha-1.0-1-any.pkg.tar.zst"},
			changed:     true,
			want:        syncSkip,
			wantPending: true,
		},
		{
			name:     "changed package cannot bypass downgrade protection",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst",
					repoFile("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst")),
			},
			files:          []string{"alpha-2.0-1-any.pkg.tar.zst"},
			changed:        true,
			rebuildChanged: true,
			want:           syncDowngrade,
		},
		{
			name:     "missing database artifact",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst"),
			},
			want: syncRepair,
		},
		{
			name:     "new version",
			metadata: metadata("1.1-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst")),
			},
			files: []string{"alpha-1.0-1-any.pkg.tar.zst"},
			want:  syncBuild,
		},
		{
			name:     "artifact already present",
			metadata: metadata("1.1-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst",
					repoFile("alpha", "1.0-1", "alpha-1.0-1-any.pkg.tar.zst"),
					repoFile("alpha", "1.1-1", "alpha-1.1-1-any.pkg.tar.zst")),
			},
			files:   []string{"alpha-1.0-1-any.pkg.tar.zst", "alpha-1.1-1-any.pkg.tar.zst"},
			want:    syncAdd,
			wantAdd: true,
		},
		{
			name:     "missing database file but artifact present",
			metadata: metadata("1.1-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "1.1-1", "alpha-1.1-1-x86_64.pkg.tar.zst",
					repoFile("alpha", "1.1-1", "alpha-1.1-1-any.pkg.tar.zst")),
			},
			files:   []string{"alpha-1.1-1-any.pkg.tar.zst"},
			want:    syncAdd,
			wantAdd: true,
		},
		{
			name:     "downgrade blocked",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst",
					repoFile("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst")),
			},
			files: []string{"alpha-2.0-1-any.pkg.tar.zst"},
			want:  syncDowngrade,
		},
		{
			name:     "downgrade allowed",
			metadata: metadata("1.0-1", "alpha"),
			repository: map[string]*meta.Package{
				"alpha": repoPackage("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst",
					repoFile("alpha", "2.0-1", "alpha-2.0-1-any.pkg.tar.zst")),
			},
			files: []string{"alpha-2.0-1-any.pkg.tar.zst"},
			allow: true,
			want:  syncBuild,
		},
		{
			name:     "split package with one missing output",
			metadata: metadata("2.0-1", "beta", "beta-extra"),
			repository: map[string]*meta.Package{
				"beta": repoPackage("beta", "2.0-1", "beta-2.0-1-any.pkg.tar.zst",
					repoFile("beta", "2.0-1", "beta-2.0-1-any.pkg.tar.zst")),
			},
			files: []string{"beta-2.0-1-any.pkg.tar.zst"},
			want:  syncBuild,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range test.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			oldRepo := Repo
			Repo = &repo.Repo{Directory: dir}
			t.Cleanup(func() {
				Repo = oldRepo
			})

			unit := &syncUnit{
				metadata: test.metadata,
				changed:  test.changed,
			}
			decideSyncUnit(unit, test.repository, syncOptions{
				AllowDowngrade: test.allow,
				Force:          test.force,
				RebuildChanged: test.rebuildChanged,
			})
			if unit.action != test.want {
				t.Errorf("action = %v, want %v", unit.action, test.want)
			}
			if test.wantAdd && len(unit.existing) == 0 {
				t.Error("expected existing artifacts to be selected")
			}
			if unit.pendingChange != test.wantPending {
				t.Errorf("pendingChange = %v, want %v", unit.pendingChange, test.wantPending)
			}
		})
	}
}

func TestDetectSyncDuplicates(t *testing.T) {
	first := &syncUnit{dir: "first", metadata: metadata("1.0-1", "same")}
	second := &syncUnit{dir: "second", metadata: metadata("1.0-1", "same")}
	third := &syncUnit{dir: "third", metadata: metadata("1.0-1", "other")}

	detectSyncDuplicates([]*syncUnit{first, second, third})

	if first.err == nil || second.err == nil {
		t.Error("duplicate package names were not reported")
	}
	if third.err != nil {
		t.Errorf("unrelated package was marked as duplicate: %v", third.err)
	}
}

func TestSelectSyncUnits(t *testing.T) {
	units := []*syncUnit{
		{dir: "foo", metadata: &pkgbuild.Metadata{Pkgbase: "foo-base", Pkgnames: []string{"foo"}, Version: "1.0-1"}},
		{dir: "bar", metadata: &pkgbuild.Metadata{Pkgbase: "bar", Pkgnames: []string{"bar", "bar-extra"}, Version: "1.0-1"}},
		{dir: "broken"},
	}

	tests := []struct {
		filters []string
		want    []string
		wantErr bool
	}{
		{filters: nil, want: []string{"foo", "bar", "broken"}},
		{filters: []string{"foo"}, want: []string{"foo"}},
		{filters: []string{"foo-base"}, want: []string{"foo"}},
		{filters: []string{"bar-extra"}, want: []string{"bar"}},
		{filters: []string{"broken"}, want: []string{"broken"}},
		{filters: []string{"foo", "bar-extra"}, want: []string{"foo", "bar"}},
		{filters: []string{"missing"}, wantErr: true},
	}

	for _, test := range tests {
		got, err := selectSyncUnits(units, test.filters)
		if test.wantErr {
			if err == nil {
				t.Errorf("filters %v: expected an error", test.filters)
			}
			continue
		}
		if err != nil {
			t.Errorf("filters %v: unexpected error: %v", test.filters, err)
			continue
		}

		var dirs []string
		for _, unit := range got {
			dirs = append(dirs, unit.dir)
		}
		if len(dirs) != len(test.want) {
			t.Fatalf("filters %v: got %v, want %v", test.filters, dirs, test.want)
		}
		for i := range dirs {
			if dirs[i] != test.want[i] {
				t.Errorf("filters %v: got %v, want %v", test.filters, dirs, test.want)
			}
		}
	}
}

func TestPruneSyncPackages(t *testing.T) {
	units := []*syncUnit{
		{dir: "foo", metadata: metadata("1.0-1", "foo")},
		{dir: "bar", metadata: metadata("1.0-1", "bar", "bar-extra")},
	}
	repository := map[string]*meta.Package{
		"foo":       repoPackage("foo", "1.0-1", "foo-1.0-1-any.pkg.tar.zst"),
		"foo-debug": repoPackage("foo-debug", "1.0-1", "foo-debug-1.0-1-any.pkg.tar.zst"),
		"bar-extra": repoPackage("bar-extra", "1.0-1", "bar-extra-1.0-1-any.pkg.tar.zst"),
		"orphan":    repoPackage("orphan", "1.0-1", "orphan-1.0-1-any.pkg.tar.zst"),
	}

	names, safe := pruneSyncPackages(units, repository)
	if !safe {
		t.Fatal("expected pruning to be safe")
	}
	if len(names) != 1 || names[0] != "orphan" {
		t.Errorf("orphans = %v, want [orphan]", names)
	}

	units = append(units, &syncUnit{dir: "broken"})
	if _, safe := pruneSyncPackages(units, repository); safe {
		t.Error("expected pruning to be unsafe when metadata is unavailable")
	}
}

func metadata(version string, names ...string) *pkgbuild.Metadata {
	return &pkgbuild.Metadata{
		Pkgbase:  names[0],
		Pkgnames: names,
		Version:  version,
	}
}

func repoPackage(name, version, database string, files ...*pacman.Package) *meta.Package {
	pkg := &meta.Package{
		Name: name,
		Database: &pacman.Package{
			Name:     name,
			Version:  version,
			Filename: database,
		},
	}
	pkg.Files = append(pkg.Files, files...)
	return pkg
}

func repoFile(name, version, filename string) *pacman.Package {
	return &pacman.Package{
		Name:     name,
		Version:  version,
		Filename: filename,
	}
}

func fileOnlyPackage(name string, files ...*pacman.Package) *meta.Package {
	pkg := &meta.Package{Name: name}
	pkg.Files = append(pkg.Files, files...)
	return pkg
}
