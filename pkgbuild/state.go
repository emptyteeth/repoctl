// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package pkgbuild

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// StateSchema is the version of the state file format.
const StateSchema = 1

// State caches metadata about PKGBUILD directories. It is an optimization
// only: the PKGBUILDs and the repository database remain the source of truth.
type State struct {
	Schema   int                    `json:"schema"`
	Packages map[string]*StateEntry `json:"packages"`
}

// StateEntry is the cached metadata for one PKGBUILD directory.
type StateEntry struct {
	Fingerprint Fingerprint `json:"fingerprint"`
	Pkgbase     string      `json:"pkgbase,omitempty"`
	Pkgnames    []string    `json:"pkgnames,omitempty"`
	Version     string      `json:"version,omitempty"`
	Unsupported string      `json:"unsupported,omitempty"`
}

// NewState returns an empty state.
func NewState() *State {
	return &State{
		Schema:   StateSchema,
		Packages: make(map[string]*StateEntry),
	}
}

// StateDir returns the directory that repoctl uses for its state files.
func StateDir(root string) string {
	return filepath.Join(root, ".repoctl")
}

// StatePath returns the path to the state file for root.
func StatePath(root string) string {
	return filepath.Join(StateDir(root), "state.json")
}

// LockPath returns the path to the lock file for root.
func LockPath(root string) string {
	return filepath.Join(StateDir(root), "state.lock")
}

// EnsureStateDir creates the state directory and a .gitignore that keeps
// its contents out of version control.
func EnsureStateDir(root string) error {
	dir := StateDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	gitignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignore); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(gitignore, []byte("*\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// LoadState reads the state file. A missing file yields an empty state.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewState(), nil
	}
	if err != nil {
		return nil, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state file %s: %w", path, err)
	}
	if state.Schema != StateSchema {
		return nil, fmt.Errorf("unsupported state schema %d", state.Schema)
	}
	if state.Packages == nil {
		state.Packages = make(map[string]*StateEntry)
	}
	for dir, entry := range state.Packages {
		if entry == nil {
			delete(state.Packages, dir)
			continue
		}
		if err := validateFingerprint(entry.Fingerprint); err != nil {
			return nil, fmt.Errorf("invalid state for %s: %w", dir, err)
		}
	}
	return &state, nil
}

// SaveState writes the state file atomically.
func SaveState(path string, state *State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Entry returns the cached entry for dir if its fingerprint matches.
func (s *State) Entry(dir string, fingerprint Fingerprint) (*StateEntry, bool) {
	entry := s.Packages[dir]
	if entry == nil || entry.Fingerprint != fingerprint {
		return nil, false
	}
	return entry, true
}

// Set stores the cached entry for dir.
func (s *State) Set(dir string, entry *StateEntry) {
	s.Packages[dir] = entry
}

// Prune removes entries for directories that are no longer present.
func (s *State) Prune(keep map[string]bool) {
	for dir := range s.Packages {
		if !keep[dir] {
			delete(s.Packages, dir)
		}
	}
}

// Lock is an advisory lock that prevents concurrent syncs of the same
// PKGBUILD directory.
type Lock struct {
	file *os.File
}

// LockState acquires an exclusive lock for root.
func LockState(root string) (*Lock, error) {
	if err := EnsureStateDir(root); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(LockPath(root), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another sync is already running for %s", root)
	}
	return &Lock{file: file}, nil
}

// Close releases the lock.
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	if cerr := l.file.Close(); err == nil {
		err = cerr
	}
	l.file = nil
	return err
}
