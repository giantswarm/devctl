//go:build !windows

package termout

import "github.com/mattn/go-isatty"

func supportsANSI(fd uintptr) bool {
	return isatty.IsTerminal(fd)
}
