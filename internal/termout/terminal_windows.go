package termout

import "golang.org/x/sys/windows"

// The prefixes are returned as strings and written directly to the file,
// so a native console must already have ANSI escape processing enabled.
// Checking its mode must not change a caller-owned console's settings.
func supportsANSI(fd uintptr) bool {
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(fd), &mode)
	const required = windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	return err == nil && mode&required == required
}
