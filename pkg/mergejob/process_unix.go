//go:build !windows

package mergejob

import (
	"errors"
	"os/exec"
	"syscall"
)

// Alive reports whether a process with pid runs.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Detach makes cmd start in a session of its own, so it outlives its
// caller's terminal, process group and signals.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// Shell is the command line that runs script.
func Shell(script string) []string { return []string{"/bin/sh", "-c", script} }
