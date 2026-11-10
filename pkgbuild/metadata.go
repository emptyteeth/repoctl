// Copyright (c) 2026, Ben Morgan. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package pkgbuild manages PKGBUILD directories that feed a local Pacman
// repository.
package pkgbuild

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Metadata is the information from a PKGBUILD that is needed to decide
// whether a package has to be built.
type Metadata struct {
	Pkgbase  string
	Pkgnames []string
	Version  string
	Arch     []string
}

// ReadMetadata runs makepkg --printsrcinfo in dir and parses the result.
//
// PKGBUILD top-level code may write to stdout, so unknown lines are
// ignored. Only the fields needed by repoctl are extracted.
func ReadMetadata(dir string) (*Metadata, error) {
	cmd := exec.Command("makepkg", "--printsrcinfo")
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("makepkg --printsrcinfo: %s", msg)
	}

	return parseSrcinfo(stdout.Bytes())
}

// parseSrcinfo extracts the package metadata from SRCINFO output.
func parseSrcinfo(data []byte) (*Metadata, error) {
	md := &Metadata{}

	var (
		section string
		epoch   string
		pkgver  string
		pkgrel  string
	)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		if line[0] != '\t' && line[0] != ' ' {
			// Section headers are unindented. Anything else at this
			// level is noise from the PKGBUILD and is ignored.
			key, value, ok := splitSrcinfo(line)
			if !ok {
				continue
			}
			switch key {
			case "pkgbase":
				section = "pkgbase"
				md.Pkgbase = value
			case "pkgname":
				section = "pkgname"
				if value != "" {
					md.Pkgnames = append(md.Pkgnames, value)
				}
			}
			continue
		}

		// Fields are indented with a tab. Only fields in the pkgbase
		// section are relevant for the candidate version.
		if section != "pkgbase" {
			continue
		}
		key, value, ok := splitSrcinfo(strings.TrimSpace(line))
		if !ok {
			continue
		}
		switch key {
		case "epoch":
			epoch = value
		case "pkgver":
			pkgver = value
		case "pkgrel":
			pkgrel = value
		case "arch":
			if value != "" {
				md.Arch = append(md.Arch, value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse SRCINFO: %w", err)
	}

	if md.Pkgbase == "" {
		return nil, fmt.Errorf("SRCINFO does not contain pkgbase")
	}
	if len(md.Pkgnames) == 0 {
		return nil, fmt.Errorf("SRCINFO does not contain pkgname")
	}
	if pkgver == "" || pkgrel == "" {
		return nil, fmt.Errorf("SRCINFO does not contain pkgver and pkgrel")
	}

	md.Version = formatVersion(epoch, pkgver, pkgrel)
	return md, nil
}

// splitSrcinfo splits a "key = value" line.
func splitSrcinfo(line string) (key, value string, ok bool) {
	i := strings.Index(line, " = ")
	if i < 0 {
		return "", "", false
	}

	key = strings.TrimSpace(line[:i])
	value = strings.TrimSpace(line[i+3:])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, value, true
}

// formatVersion formats the version in the same form that Pacman and
// the package database use.
func formatVersion(epoch, pkgver, pkgrel string) string {
	version := pkgver + "-" + pkgrel
	if epoch != "" {
		version = epoch + ":" + version
	}
	return version
}
