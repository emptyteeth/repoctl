// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cassava/repoctl/internal/term"
	"github.com/cassava/repoctl/pacman/alpm"
	"github.com/cassava/repoctl/pacman/meta"
	"github.com/cassava/repoctl/pkgbuild"
	"github.com/cassava/repoctl/repo"
	"github.com/goulash/errs"
	"github.com/spf13/cobra"
)

var (
	syncPkgbuildDir    string
	syncAllowDowngrade bool
	syncStrict         bool
	syncDryRun         bool
	syncNoState        bool
	syncRefresh        bool
	syncRebuildChanged bool
	syncForce          bool
	syncFailFast       bool
	syncPrune          bool
	syncSign           bool
	syncNoSign         bool
	syncKey            string
	syncMakepkgArgs    []string
	syncCleanbuild     bool
	syncClean          bool
	syncSyncdeps       bool
	syncRmdeps         bool
	syncNocheck        bool
)

func init() {
	MainCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringVar(&syncPkgbuildDir, "pkgbuild-dir", "", "directory containing one subdirectory per package with a PKGBUILD")
	syncCmd.RegisterFlagCompletionFunc("pkgbuild-dir", completeDirectory)
	syncCmd.Flags().BoolVar(&syncAllowDowngrade, "allow-downgrade", false, "allow building packages whose version is lower than the repository version")
	syncCmd.Flags().BoolVar(&syncStrict, "strict", false, "treat warnings as errors")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "show what would be done without building or modifying the repository")
	syncCmd.Flags().BoolVar(&syncNoState, "no-state", false, "do not read or write the state cache")
	syncCmd.Flags().BoolVar(&syncRefresh, "refresh", false, "ignore cached PKGBUILD metadata for this run")
	syncCmd.Flags().BoolVar(&syncRebuildChanged, "rebuild-changed", false, "rebuild when the PKGBUILD changed even if the version did not")
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "rebuild all selected packages")
	syncCmd.Flags().BoolVar(&syncFailFast, "fail-fast", false, "stop after the first failure")
	syncCmd.Flags().BoolVar(&syncPrune, "prune", false, "remove packages that are not produced by any PKGBUILD")
	syncCmd.Flags().BoolVar(&syncSign, "sign", false, "sign built packages")
	syncCmd.Flags().BoolVar(&syncNoSign, "nosign", false, "do not sign built packages")
	syncCmd.Flags().StringVar(&syncKey, "key", "", "GPG key used for signing")
	syncCmd.Flags().StringArrayVar(&syncMakepkgArgs, "makepkg-arg", nil, "additional argument passed to makepkg")
	syncCmd.Flags().BoolVar(&syncCleanbuild, "cleanbuild", false, "remove $srcdir before building")
	syncCmd.Flags().BoolVar(&syncClean, "clean", false, "clean up work files after building")
	syncCmd.Flags().BoolVar(&syncSyncdeps, "syncdeps", false, "install missing dependencies with pacman")
	syncCmd.Flags().BoolVar(&syncRmdeps, "rmdeps", false, "remove dependencies after building")
	syncCmd.Flags().BoolVar(&syncNocheck, "nocheck", false, "do not run the check() function")
}

var syncCmd = &cobra.Command{
	Use:   "sync [FILTER...]",
	Short: "Build and add packages from a PKGBUILD directory",
	Long: `Build packages from a directory of PKGBUILDs and add them to the repository.

  The pkgbuild_dir setting must point to a directory that contains one
  subdirectory per package, each with a PKGBUILD file. Without filters,
  all of these packages are processed.

  A package is skipped when the version in its PKGBUILD is the same as
  the version registered in the repository database. New packages and
  packages with a different version are built and then added through the
  same code path as the add command. Packages whose database entry has no
  package file are rebuilt, unless another package file with the same
  version can be registered instead.

  Packages that define pkgver() are unsupported and are skipped, because
  their version can only be determined by building them.

  Filters may name a directory, a pkgbase, or a pkgname. Matching any of
  them selects the directory, and all split packages produced by that
  directory are managed together.

  State is cached in .repoctl/ inside pkgbuild_dir. Use --no-state to
  disable it, or --refresh to ignore it once. --rebuild-changed rebuilds
  a package whose PKGBUILD changed even when its version did not; changes
  to local source files are not detected by the fingerprint. Because
  change detection depends on the state cache, --rebuild-changed has no
  effect together with --no-state.

  --prune removes repository packages that are not produced by any
  PKGBUILD. It cannot be combined with filters and is skipped when no
  PKGBUILDs are found. Debug packages named <pkgname>-debug are kept
  when their base package is present in the tree.
`,
	Example: `  repoctl sync
  repoctl sync foo
  repoctl sync --dry-run
  repoctl sync --rebuild-changed
  repoctl sync --prune`,
	Args:     cobra.ArbitraryArgs,
	PreRunE:  ProfileInit,
	PostRunE: ProfileTeardown,
	RunE:     runSync,
}

// syncAction is what sync intends to do with a PKGBUILD directory.
type syncAction int

const (
	syncSkip syncAction = iota
	syncBuild
	syncRepair
	syncAdd
	syncDowngrade
	syncUnsupported
	syncError
)

// syncUnit is one PKGBUILD directory and everything known about it.
type syncUnit struct {
	dir         string
	path        string
	inspection  *pkgbuild.Inspection
	metadata    *pkgbuild.Metadata
	unsupported string
	err         error

	action        syncAction
	existing      []string
	changed       bool
	pendingChange bool
}

// managed reports whether the unit can be built and added.
func (u *syncUnit) managed() bool {
	return u.err == nil && u.unsupported == "" && u.metadata != nil
}

// syncOptions controls how units are planned.
type syncOptions struct {
	AllowDowngrade bool
	Force          bool
	RebuildChanged bool
}

// syncCounts summarizes the result of a sync.
type syncCounts struct {
	built       int
	repaired    int
	added       int
	pruned      int
	skipped     int
	warnings    int
	unsupported int
	failed      int
}

func runSync(cmd *cobra.Command, filters []string) error {
	root := syncPkgbuildDir
	if root == "" {
		root = Profile.PkgBuildDir
	}
	if root == "" {
		return fmt.Errorf("pkgbuild_dir is not configured: set it in the profile or use --pkgbuild-dir")
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("pkgbuild_dir must be an absolute path: %s", root)
	}
	if err := validateSyncFilters(filters); err != nil {
		return err
	}

	if syncSign && syncNoSign {
		return errors.New("--sign and --nosign cannot be used together")
	}
	if syncNoSign && Repo.RequireSignature {
		return errors.New("the profile requires signatures, so --nosign cannot be used")
	}
	if syncKey != "" && !syncShouldSign() {
		return errors.New("--key requires signing to be enabled")
	}
	if syncPrune && len(filters) > 0 {
		return errors.New("--prune cannot be combined with filters")
	}

	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("cannot access pkgbuild_dir %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("pkgbuild_dir is not a directory: %s", root)
	}
	if _, err := exec.LookPath("makepkg"); err != nil {
		return fmt.Errorf("cannot find makepkg in PATH: %w", err)
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("makepkg cannot be run as root")
	}

	lock, err := pkgbuild.LockState(root)
	if err != nil {
		return err
	}
	defer lock.Close()

	statePath := pkgbuild.StatePath(root)
	state := pkgbuild.NewState()
	if !syncNoState {
		loaded, err := pkgbuild.LoadState(statePath)
		if err != nil {
			term.Warnf("Warning: ignoring state file %s: %s\n", statePath, err)
		} else {
			state = loaded
		}
	}

	dirs, err := pkgbuild.Scan(root)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		if len(filters) > 0 {
			return fmt.Errorf("no package matches filter(s): %s", strings.Join(filters, ", "))
		}
		term.Printf("No PKGBUILDs found in %s.\n", root)
		return nil
	}

	useCache := !syncRefresh && !syncNoState
	units := make([]*syncUnit, 0, len(dirs))
	for _, dir := range dirs {
		units = append(units, loadSyncUnit(root, dir, state, useCache))
	}

	selected, err := selectSyncUnits(units, filters)
	if err != nil {
		return err
	}

	// A repository that has not been initialized yet has no database file.
	// Reading the directory is still useful, so suppress the resulting
	// database error until the first package is added.
	var handler errs.Handler
	if !fileExists(Repo.DatabasePath()) {
		handler = func(error) error { return nil }
	}
	repoPkgs, err := Repo.ReadMeta(handler)
	if err != nil {
		return err
	}
	repoIndex := make(map[string]*meta.Package, len(repoPkgs))
	for _, pkg := range repoPkgs {
		repoIndex[pkg.Name] = pkg
	}

	options := syncOptions{
		AllowDowngrade: syncAllowDowngrade,
		Force:          syncForce,
		RebuildChanged: syncRebuildChanged,
	}

	detectSyncDuplicates(selected)
	for _, unit := range selected {
		switch {
		case unit.err != nil:
			unit.action = syncError
		case unit.unsupported != "":
			unit.action = syncUnsupported
		case unit.metadata == nil:
			unit.err = errors.New("package metadata is unavailable")
			unit.action = syncError
		default:
			decideSyncUnit(unit, repoIndex, options)
		}
	}

	if syncDryRun {
		counts := printSyncPlan(selected)
		if syncPrune {
			names, safe := pruneSyncPackages(units, repoIndex)
			if !safe {
				counts.warnings++
				term.Warnf("Warning: cannot determine orphan packages because some PKGBUILDs could not be read\n")
			} else {
				for _, name := range names {
					term.Printf("  prune        %s\n", name)
					counts.pruned++
				}
			}
		}
		printSyncSummary(counts, true)
		return syncExitError(counts)
	}

	var counts syncCounts
	built := make(map[*syncUnit]bool)
	preserve := make(map[*syncUnit]bool)
	buildOptions := pkgbuild.BuildOptions{
		Sign:  syncShouldSign(),
		Key:   syncKey,
		Flags: syncMakepkgFlags(),
	}

	for _, unit := range selected {
		failedBefore := counts.failed

		switch unit.action {
		case syncError:
			counts.failed++
			term.Errorf("Error: %s: %s\n", unit.dir, unit.err)
		case syncUnsupported:
			counts.unsupported++
			term.Printf("  unsupported  %-30s %s\n", unit.dir, unit.unsupported)
		case syncDowngrade:
			counts.warnings++
			term.Warnf("Warning: %s: version %s is lower than the repository version; use --allow-downgrade\n", unit.dir, unit.metadata.Version)
		case syncSkip:
			counts.skipped++
			term.Printf("  skip         %-30s %s\n", unit.dir, unit.metadata.Version)
			if unit.pendingChange {
				counts.warnings++
				preserve[unit] = true
				term.Warnf("Warning: %s: PKGBUILD changed but version is unchanged; bump pkgrel or use --rebuild-changed\n", unit.dir)
			}
		case syncAdd:
			term.Printf("  add          %-30s %s\n", unit.dir, unit.metadata.Version)
			if err := addSyncArtifacts(unit.existing); err != nil {
				counts.failed++
				term.Errorf("Error: %s: %s\n", unit.dir, err)
			} else {
				counts.added++
			}
		case syncBuild, syncRepair:
			label := "build"
			if unit.action == syncRepair {
				label = "repair"
			}
			term.Printf("  %-12s %-30s %s\n", label, unit.dir, unit.metadata.Version)
			built[unit] = true
			if err := buildSyncUnit(unit, buildOptions); err != nil {
				counts.failed++
				term.Errorf("Error: %s: %s\n", unit.dir, err)
			} else if unit.action == syncRepair {
				counts.repaired++
			} else {
				counts.built++
			}
		}

		if syncFailFast && counts.failed > failedBefore {
			break
		}
	}

	if syncPrune && (!syncFailFast || counts.failed == 0) {
		names, safe := pruneSyncPackages(units, repoIndex)
		if !safe {
			counts.warnings++
			term.Warnf("Warning: cannot determine orphan packages because some PKGBUILDs could not be read\n")
		} else {
			for _, name := range names {
				term.Printf("  prune        %s\n", name)
				failedBefore := counts.failed
				if err := pruneSyncPackage(name, repoIndex[name]); err != nil {
					counts.failed++
					term.Errorf("Error: cannot prune %s: %s\n", name, err)
				} else {
					counts.pruned++
				}
				if syncFailFast && counts.failed > failedBefore {
					break
				}
			}
		}
	}

	if !syncNoState {
		updateSyncState(state, units, built, preserve)
		if err := pkgbuild.SaveState(statePath, state); err != nil {
			term.Warnf("Warning: cannot save state file %s: %s\n", statePath, err)
		}
	}

	printSyncSummary(counts, false)
	return syncExitError(counts)
}

// loadSyncUnit reads the metadata for one PKGBUILD directory, using the
// cached state entry when its fingerprint still matches.
func loadSyncUnit(root, dir string, state *pkgbuild.State, useCache bool) *syncUnit {
	unit := &syncUnit{
		dir:  dir,
		path: filepath.Join(root, dir),
	}

	inspection, err := pkgbuild.Inspect(unit.path)
	if err != nil {
		unit.err = err
		return unit
	}
	unit.inspection = inspection

	if previous := state.Packages[dir]; previous != nil && previous.Fingerprint != inspection.Fingerprint {
		unit.changed = true
	}

	var (
		entry  *pkgbuild.StateEntry
		cached bool
	)
	if useCache {
		entry, cached = state.Entry(dir, inspection.Fingerprint)
	}

	if inspection.Dynamic {
		unit.unsupported = inspection.DynamicReason
		if cached && len(entry.Pkgnames) > 0 && entry.Version != "" {
			unit.metadata = &pkgbuild.Metadata{
				Pkgbase:  entry.Pkgbase,
				Pkgnames: append([]string(nil), entry.Pkgnames...),
				Version:  entry.Version,
			}
			return unit
		}
		// Metadata is best effort for unsupported packages. It makes
		// filtering and reporting more useful.
		if metadata, err := pkgbuild.ReadMetadata(unit.path); err == nil {
			unit.metadata = metadata
		}
		return unit
	}

	if cached {
		unit.unsupported = entry.Unsupported
		if len(entry.Pkgnames) > 0 && entry.Version != "" {
			unit.metadata = &pkgbuild.Metadata{
				Pkgbase:  entry.Pkgbase,
				Pkgnames: append([]string(nil), entry.Pkgnames...),
				Version:  entry.Version,
			}
			return unit
		}
	}

	metadata, err := pkgbuild.ReadMetadata(unit.path)
	if err != nil {
		unit.err = err
		return unit
	}
	unit.metadata = metadata
	return unit
}

// selectSyncUnits applies the filters. A filter matches a directory, a
// pkgbase, or a pkgname.
func selectSyncUnits(units []*syncUnit, filters []string) ([]*syncUnit, error) {
	if len(filters) == 0 {
		return units, nil
	}

	matched := make([]bool, len(filters))
	selected := make([]*syncUnit, 0, len(units))
	for _, unit := range units {
		if syncUnitMatches(unit, filters, matched) {
			selected = append(selected, unit)
		}
	}

	var missing []string
	for i, filter := range filters {
		if !matched[i] {
			missing = append(missing, filter)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no package matches filter(s): %s", strings.Join(missing, ", "))
	}
	return selected, nil
}

func syncUnitMatches(unit *syncUnit, filters []string, matched []bool) bool {
	found := false
	for i, filter := range filters {
		if unit.dir == filter {
			matched[i] = true
			found = true
			continue
		}
		if unit.metadata == nil {
			continue
		}
		if unit.metadata.Pkgbase == filter {
			matched[i] = true
			found = true
			continue
		}
		for _, name := range unit.metadata.Pkgnames {
			if name == filter {
				matched[i] = true
				found = true
				break
			}
		}
	}
	return found
}

func validateSyncFilters(filters []string) error {
	for _, filter := range filters {
		if filter == "" || filter == "." || filter == ".." || strings.ContainsAny(filter, "/\\") {
			return fmt.Errorf("invalid filter %q", filter)
		}
	}
	return nil
}

// detectSyncDuplicates marks managed units that claim the same pkgname.
func detectSyncDuplicates(units []*syncUnit) {
	byName := make(map[string][]*syncUnit)
	for _, unit := range units {
		if unit.metadata == nil {
			continue
		}
		for _, name := range unit.metadata.Pkgnames {
			byName[name] = append(byName[name], unit)
		}
	}

	for name, claims := range byName {
		var managed []*syncUnit
		for _, unit := range claims {
			if unit.managed() {
				managed = append(managed, unit)
			}
		}
		if len(managed) < 2 {
			continue
		}

		dirs := make([]string, 0, len(managed))
		for _, unit := range managed {
			dirs = append(dirs, unit.dir)
		}
		sort.Strings(dirs)

		message := fmt.Sprintf("package %q is produced by multiple directories: %s", name, strings.Join(dirs, ", "))
		for _, unit := range managed {
			unit.err = errors.New(message)
		}
	}
}

// decideSyncUnit compares the candidate version with the repository and
// decides what to do with the whole PKGBUILD directory.
func decideSyncUnit(unit *syncUnit, repoIndex map[string]*meta.Package, options syncOptions) {
	var (
		needBuild      bool
		needAdd        bool
		blockDowngrade bool
		onlyRepairs    = true
		existing       []string
	)

	for _, name := range unit.metadata.Pkgnames {
		candidate := unit.metadata.Version
		pkg := repoIndex[name]

		var (
			dbVersion    string
			dbFileExists bool
			existingPath string
		)

		if pkg != nil {
			if pkg.Database != nil {
				dbVersion = pkg.Database.Version
				dbFileExists = fileExists(repoFileNamePath(pkg.Database.Filename))
			}
			for _, file := range pkg.Files {
				if alpm.VerCmp(file.Version, candidate) != 0 {
					continue
				}
				path := repoFileNamePath(file.Filename)
				if fileExists(path) {
					existingPath = path
					break
				}
			}
		}

		switch {
		case dbVersion != "" && alpm.VerCmp(dbVersion, candidate) == 0:
			switch {
			case dbFileExists:
				// The repository is up to date.
			case existingPath != "":
				needAdd = true
				existing = append(existing, existingPath)
			default:
				needBuild = true
			}
		case dbVersion == "" || alpm.VerCmp(candidate, dbVersion) == 1:
			if existingPath != "" {
				needAdd = true
				existing = append(existing, existingPath)
			} else {
				needBuild = true
				onlyRepairs = false
			}
		default:
			// The candidate is older than the version in the repository.
			if options.AllowDowngrade {
				if existingPath != "" {
					needAdd = true
					existing = append(existing, existingPath)
				} else {
					needBuild = true
				}
				onlyRepairs = false
			} else {
				blockDowngrade = true
			}
		}
	}

	existing = uniqueStrings(existing)

	switch {
	case blockDowngrade:
		unit.action = syncDowngrade
	case needBuild:
		if onlyRepairs {
			unit.action = syncRepair
		} else {
			unit.action = syncBuild
		}
	case needAdd:
		unit.action = syncAdd
		unit.existing = existing
	default:
		unit.action = syncSkip
	}

	switch {
	case options.Force && unit.action != syncDowngrade:
		unit.action = syncBuild
	case options.RebuildChanged && unit.action == syncSkip && unit.changed:
		unit.action = syncBuild
	case unit.action == syncSkip && unit.changed:
		unit.pendingChange = true
	}
}

// buildSyncUnit builds one PKGBUILD in a temporary destination and adds
// the resulting packages to the repository.
func buildSyncUnit(unit *syncUnit, options pkgbuild.BuildOptions) error {
	pkgdest, err := os.MkdirTemp("", "repoctl-pkgdest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(pkgdest)

	if err := pkgbuild.Build(unit.path, pkgdest, options); err != nil {
		return err
	}

	artifacts, err := pkgbuild.ReadArtifacts(pkgdest)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return fmt.Errorf("makepkg produced no packages")
	}

	// makepkg may have produced a different version than SRCINFO reported
	// (for example when a pkgver() function slipped past detection). Use
	// the version from the actual package so that the state cache stays
	// consistent with what was added.
	for _, pkg := range artifacts {
		if containsString(unit.metadata.Pkgnames, pkg.Name) {
			unit.metadata.Version = pkg.Version
			break
		}
	}

	paths := make([]string, 0, len(artifacts))
	for _, pkg := range artifacts {
		paths = append(paths, pkg.Filename)
	}
	return addSyncArtifacts(paths)
}

// syncShouldSign reports whether makepkg should sign packages.
func syncShouldSign() bool {
	if syncNoSign {
		return false
	}
	return syncSign || Repo.RequireSignature
}

// syncMakepkgFlags returns the makepkg flags derived from the command line.
func syncMakepkgFlags() []string {
	var flags []string
	if syncCleanbuild {
		flags = append(flags, "--cleanbuild")
	}
	if syncClean {
		flags = append(flags, "--clean")
	}
	if syncSyncdeps {
		flags = append(flags, "--syncdeps")
	}
	if syncRmdeps {
		flags = append(flags, "--rmdeps")
	}
	if syncNocheck {
		flags = append(flags, "--nocheck")
	}
	return append(flags, syncMakepkgArgs...)
}

// addSyncArtifacts registers the given package files in the repository.
func addSyncArtifacts(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	addable := make([]string, 0, len(paths))
	for _, path := range paths {
		if Repo.RequireSignature {
			pkg, err := repo.NewSignedPkg(path)
			if err != nil {
				return err
			}
			if !pkg.HasSignature() {
				return fmt.Errorf("package %s has no signature", path)
			}
		}
		addable = append(addable, path)
	}
	return Repo.Copy(nil, addable...)
}

// pruneSyncPackages returns repository packages that are not produced by
// any PKGBUILD. The second return value is false when the set of packages
// produced by the PKGBUILD directory cannot be determined.
func pruneSyncPackages(units []*syncUnit, repoIndex map[string]*meta.Package) ([]string, bool) {
	known := make(map[string]bool)
	for _, unit := range units {
		if unit.metadata == nil {
			return nil, false
		}
		for _, name := range unit.metadata.Pkgnames {
			known[name] = true
		}
	}

	// makepkg may produce debug packages that are not listed in pkgname.
	// Keep them when their base package is present in the PKGBUILD tree.
	for name := range known {
		known[name+"-debug"] = true
	}

	var names []string
	for name := range repoIndex {
		if !known[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, true
}

// pruneSyncPackage removes one package from the repository.
func pruneSyncPackage(name string, pkg *meta.Package) error {
	if pkg == nil {
		return Repo.RemoveFromDatabase(name)
	}
	if pkg.Database == nil {
		// Only package files are present; remove them according to the
		// configured backup policy.
		paths := make([]string, 0, len(pkg.Files))
		for _, file := range pkg.Files {
			paths = append(paths, repoFileNamePath(file.Filename))
		}
		return Repo.Dispatch(nil, paths...)
	}
	if len(pkg.Files) > 0 {
		return Repo.Remove(nil, name)
	}
	return Repo.RemoveFromDatabase(name)
}

// updateSyncState stores the metadata cache for all scanned directories.
func updateSyncState(state *pkgbuild.State, units []*syncUnit, built, preserve map[*syncUnit]bool) {
	keep := make(map[string]bool, len(units))
	for _, unit := range units {
		keep[unit.dir] = true

		if preserve[unit] {
			// Keep the previous entry so that a changed but unbuilt
			// PKGBUILD is detected again on the next run.
			continue
		}
		if unit.inspection == nil {
			// Keep the previous entry when the PKGBUILD could not be read.
			continue
		}
		if unit.metadata == nil && unit.unsupported == "" {
			// Keep the previous entry when metadata could not be read.
			continue
		}

		fingerprint := unit.inspection.Fingerprint
		if built[unit] {
			// makepkg may rewrite pkgver for VCS packages. Recompute the
			// fingerprint so that the next run does not immediately see a
			// change. This is only a safety net because such packages are
			// normally skipped as unsupported.
			if inspection, err := pkgbuild.Inspect(unit.path); err == nil {
				fingerprint = inspection.Fingerprint
			}
		}

		entry := &pkgbuild.StateEntry{
			Fingerprint: fingerprint,
			Unsupported: unit.unsupported,
		}
		if unit.metadata != nil {
			entry.Pkgbase = unit.metadata.Pkgbase
			entry.Pkgnames = append([]string(nil), unit.metadata.Pkgnames...)
			entry.Version = unit.metadata.Version
		}
		state.Set(unit.dir, entry)
	}
	state.Prune(keep)
}

// printSyncPlan prints what a dry run would do and returns the counts.
func printSyncPlan(units []*syncUnit) syncCounts {
	var counts syncCounts
	for _, unit := range units {
		switch unit.action {
		case syncError:
			counts.failed++
			term.Errorf("Error: %s: %s\n", unit.dir, unit.err)
		case syncUnsupported:
			counts.unsupported++
			term.Printf("  unsupported  %-30s %s\n", unit.dir, unit.unsupported)
		case syncDowngrade:
			counts.warnings++
			term.Warnf("Warning: %s: version %s is lower than the repository version; use --allow-downgrade\n", unit.dir, unit.metadata.Version)
		case syncSkip:
			counts.skipped++
			term.Printf("  skip         %-30s %s\n", unit.dir, unit.metadata.Version)
			if unit.pendingChange {
				counts.warnings++
				term.Warnf("Warning: %s: PKGBUILD changed but version is unchanged; bump pkgrel or use --rebuild-changed\n", unit.dir)
			}
		case syncAdd:
			counts.added++
			term.Printf("  add          %-30s %s\n", unit.dir, unit.metadata.Version)
		case syncBuild, syncRepair:
			label := "build"
			if unit.action == syncRepair {
				label = "repair"
				counts.repaired++
			} else {
				counts.built++
			}
			term.Printf("  %-12s %-30s %s\n", label, unit.dir, unit.metadata.Version)
		}
	}
	return counts
}

func printSyncSummary(counts syncCounts, plan bool) {
	total := counts.built + counts.repaired + counts.added + counts.pruned +
		counts.skipped + counts.warnings + counts.unsupported + counts.failed
	if total == 0 {
		term.Printf("Nothing to do.\n")
		return
	}

	label := "Summary"
	if plan {
		label = "Plan"
	}
	term.Printf("%s: %d built, %d repaired, %d added, %d pruned, %d skipped, %d warning(s), %d unsupported, %d failed\n",
		label, counts.built, counts.repaired, counts.added, counts.pruned,
		counts.skipped, counts.warnings, counts.unsupported, counts.failed)
}

func syncExitError(counts syncCounts) error {
	if counts.failed > 0 {
		return fmt.Errorf("%d package(s) failed", counts.failed)
	}
	if syncStrict && counts.warnings > 0 {
		return fmt.Errorf("%d warning(s) treated as errors", counts.warnings)
	}
	return nil
}

func repoFileNamePath(filename string) string {
	return filepath.Join(Repo.Directory, filepath.Base(filename))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := values[:0]
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
