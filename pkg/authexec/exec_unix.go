//go:build !windows

package authexec

import (
	"io/fs"
	"syscall"
)

// execProgram replaces devctl with the program: its exit code, signals and
// terminal are the program's own.
func execProgram(path string, argv, env []string) (int, error) {
	return 0, syscall.Exec(path, argv, env) // nolint:gosec // running the given program is the command's purpose
}

func candidates(path string) []string { return []string{path} }

func isExecutable(mode fs.FileMode) bool { return mode&0o111 != 0 }
