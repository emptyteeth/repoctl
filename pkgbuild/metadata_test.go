// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import "testing"

func TestParseSrcinfo(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Metadata
		wantErr bool
	}{
		{
			name: "simple",
			input: `pkgbase = foo
	pkgdesc = A test package
	pkgver = 1.2.3
	pkgrel = 4
	url = https://example.com
	arch = x86_64
	license = MIT

pkgname = foo
`,
			want: &Metadata{
				Pkgbase:  "foo",
				Pkgnames: []string{"foo"},
				Version:  "1.2.3-4",
				Arch:     []string{"x86_64"},
			},
		},
		{
			name: "split with epoch",
			input: `pkgbase = bar
	pkgver = 2.0
	pkgrel = 1
	epoch = 3
	arch = any

pkgname = bar
pkgname = bar-libs
`,
			want: &Metadata{
				Pkgbase:  "bar",
				Pkgnames: []string{"bar", "bar-libs"},
				Version:  "3:2.0-1",
				Arch:     []string{"any"},
			},
		},
		{
			name: "top-level noise",
			input: `GARBAGE FROM PKGBUILD
pkgver = 9.9
pkgbase = baz
	pkgver = 1.0
	pkgrel = 1

pkgname = baz
`,
			want: &Metadata{
				Pkgbase:  "baz",
				Pkgnames: []string{"baz"},
				Version:  "1.0-1",
			},
		},
		{
			name: "missing pkgname",
			input: `pkgbase = broken
	pkgver = 1.0
	pkgrel = 1
`,
			wantErr: true,
		},
		{
			name: "missing version",
			input: `pkgbase = broken
	pkgrel = 1

pkgname = broken
`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseSrcinfo([]byte(test.input))
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Pkgbase != test.want.Pkgbase {
				t.Errorf("Pkgbase = %q, want %q", got.Pkgbase, test.want.Pkgbase)
			}
			if got.Version != test.want.Version {
				t.Errorf("Version = %q, want %q", got.Version, test.want.Version)
			}
			if len(got.Pkgnames) != len(test.want.Pkgnames) {
				t.Fatalf("Pkgnames = %v, want %v", got.Pkgnames, test.want.Pkgnames)
			}
			for i := range got.Pkgnames {
				if got.Pkgnames[i] != test.want.Pkgnames[i] {
					t.Errorf("Pkgnames[%d] = %q, want %q", i, got.Pkgnames[i], test.want.Pkgnames[i])
				}
			}
			if len(got.Arch) != len(test.want.Arch) {
				t.Errorf("Arch = %v, want %v", got.Arch, test.want.Arch)
			}
		})
	}
}
