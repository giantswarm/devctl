package agentcli

import (
	"os/signal"
	"syscall"
)

// IgnoreHangup keeps a wait running when its terminal goes away: a closed
// terminal, SSH session or harness that drops the process sends SIGHUP,
// whose default ends the process without its document. A wait reads nothing
// from stdin, so a closed stdin changes nothing either; the wait ends with
// the verdict as it would in the foreground.
func IgnoreHangup() {
	signal.Ignore(syscall.SIGHUP)
}
