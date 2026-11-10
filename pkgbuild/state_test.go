// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(StateDir(dir), "state.json")

	state := NewState()
	state.Set("foo", &StateEntry{
		Fingerprint: "sha256:abc",
		Pkgbase:     "foo",
		Pkgnames:    []string{"foo", "foo-libs"},
		Version:     "1:1.2-3",
	})
	if err := SaveState(path, state); err != nil {
		t.Fatal(err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := got.Entry("foo", "sha256:abc")
	if !ok {
		t.Fatal("entry not found after round trip")
	}
	if entry.Version != "1:1.2-3" {
		t.Errorf("Version = %q, want %q", entry.Version, "1:1.2-3")
	}
	if len(entry.Pkgnames) != 2 {
		t.Errorf("Pkgnames = %v, want two entries", entry.Pkgnames)
	}
}

func TestLoadStateMissing(t *testing.T) {
	state, err := LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Schema != StateSchema || state.Packages == nil {
		t.Fatalf("unexpected empty state: %+v", state)
	}
}

func TestLoadStateCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("expected an error for corrupt state")
	}
}

func TestLoadStateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema":99,"packages":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("expected an error for an unsupported schema")
	}
}

func TestEnsureStateDirGitignore(t *testing.T) {
	root := t.TempDir()
	if err := EnsureStateDir(root); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(StateDir(root), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "*\n" {
		t.Errorf(".gitignore = %q, want %q", data, "*\n")
	}
}

func TestLockState(t *testing.T) {
	root := t.TempDir()

	lock, err := LockState(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := LockState(root); err == nil {
		t.Fatal("expected the second lock to fail")
	}

	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := LockState(root)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	second.Close()
}

func TestPrune(t *testing.T) {
	state := NewState()
	state.Set("keep", &StateEntry{Fingerprint: "sha256:a"})
	state.Set("drop", &StateEntry{Fingerprint: "sha256:b"})

	state.Prune(map[string]bool{"keep": true})

	if _, ok := state.Packages["keep"]; !ok {
		t.Error("kept entry was removed")
	}
	if _, ok := state.Packages["drop"]; ok {
		t.Error("unused entry was not removed")
	}
}
