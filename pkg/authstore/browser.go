package authstore

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the platform's default browser and returns once
// the opener has started; the page loads in the background.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) //nolint:gosec // G204: the argument is the URL devctl built for the human
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec // G204: the argument is the URL devctl built for the human
	default:
		cmd = exec.Command("xdg-open", url) //nolint:gosec // G204: the argument is the URL devctl built for the human
	}
	return cmd.Start()
}
