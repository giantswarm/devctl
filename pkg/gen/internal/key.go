package internal

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// RegenerableFilePrefix defines a prefix for files that can be
	// regenerated in a subsequent generator execution. Otherwise the file
	// is considered a scaffolding file which is generated once and
	// supposed to be edited by the user.
	//
	// NOTE: It is important to design scaffolding files in a way so they
	// stay compatible with regenerated files as they are not updated in
	// subsequent generator executions.
	RegenerableFilePrefix = "zz_generated."
)

func FileName(dir, name string) string {
	return filepath.Join(dir, name)
}

func Header(comment, githubUrl string) string {
	parts := []string{
		comment + " DO NOT EDIT. Generated with:",
		comment,
		comment + "    devctl",
	}
	if githubUrl != "" {
		parts = append(parts,
			comment,
			comment+"    "+githubUrl,
		)
	}
	parts = append(parts, comment)

	return strings.Join(parts, "\n")
}

func StepSetUpGitIdentity() string {
	return strings.Join([]string{
		"      - name: Set up git identity",
		"        run: |",
		`          git config --local user.email "dev@giantswarm.io"`,
		`          git config --local user.name "taylorbot"`,
	}, "\n")
}

// Package returns Go package name for the give directory.
func Package(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		panic(fmt.Sprintf("filepath.Abs: %s", err))
	}

	return filepath.Base(abs)
}

// RegenerableFileName returns file name prefixed with "zz_generated." denoting
// that it be regenerated in a subsequent generator execution. Otherwise the
// file is considered a scaffolding file which is generated once and supposed
// to be edited by the user.
//
// NOTE: It is important to design scaffolding files in a way so they stay
// compatible with regenerated files as they are not updated in subsequent
// generator executions.
func RegenerableFileName(dir, suffix string) string {
	return filepath.Join(dir, RegenerableFilePrefix+suffix)
}
