//go:build windows

package authexec

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
)

// execProgram runs the program and returns its exit code: Windows cannot
// replace a process.
func execProgram(path string, argv, env []string) (int, error) {
	cmd := exec.Command(path, argv[1:]...) // nolint:gosec // running the given program is the command's purpose
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}

func candidates(path string) []string { return []string{path + ".exe", path} }

func isExecutable(fs.FileMode) bool { return true }
