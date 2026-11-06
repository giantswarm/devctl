package termout_test

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/giantswarm/devctl/v8/internal/termout"
)

func TestPrefixesConsoleModes(t *testing.T) {
	console := privateConsole(t)
	const plain = windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_WRAP_AT_EOL_OUTPUT
	const ansi = plain | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	for _, tc := range []struct {
		name    string
		mode    uint32
		noColor string
		error   string
		warning string
	}{
		{"plain console", plain, "", "Error", "Warning"},
		{"ANSI console", ansi, "", "\x1b[31mError\x1b[0m", "\x1b[33mWarning\x1b[0m"},
		{"NO_COLOR", ansi, "1", "Error", "Warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			handle := windows.Handle(console.Fd())
			if err := windows.SetConsoleMode(handle, tc.mode); err != nil {
				t.Fatal(err)
			}
			if got := termout.Error(console); got != tc.error {
				t.Errorf("Error = %q, want %q", got, tc.error)
			}
			if got := termout.Warning(console); got != tc.warning {
				t.Errorf("Warning = %q, want %q", got, tc.warning)
			}
			var after uint32
			if err := windows.GetConsoleMode(handle, &after); err != nil {
				t.Fatal(err)
			}
			if after != tc.mode {
				t.Errorf("console mode changed from %#x to %#x", tc.mode, after)
			}
		})
	}
}

// Create a private, inactive screen buffer so the test never writes to or
// changes the modes of a person's terminal. A headless runner may have no
// attached console from which Windows can create a screen buffer.
func privateConsole(t *testing.T) *os.File {
	t.Helper()
	create := windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateConsoleScreenBuffer")
	const consoleTextmodeBuffer = 1
	handle, _, err := create.Call(
		uintptr(windows.GENERIC_READ|windows.GENERIC_WRITE),
		uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE),
		0, consoleTextmodeBuffer, 0,
	)
	if handle == uintptr(windows.InvalidHandle) {
		t.Skipf("private console screen buffer unavailable: %v", err)
	}
	console := os.NewFile(handle, "termout-test-console")
	t.Cleanup(func() { _ = console.Close() })
	return console
}
