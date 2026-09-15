package release

import (
	"os"
	"os/exec"
	"strings"

	"github.com/giantswarm/microerror"
)

// Call "diff" on the current machine to compare the file at leftPath against rightPath.
func createDiff(leftPath string, rightPath string) (string, error) {
	cmd := exec.Command("diff", leftPath, rightPath, "-y", "-t") // #nosec G204 -- fixed binary; both paths are built by this package under the checked provider directory, and the flags follow them
	var writer strings.Builder
	cmd.Stdout = &writer
	cmd.Stderr = os.Stdout
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 { // diff exits with 1 when files differ
			return "", microerror.Mask(exitErr)
		}
	} else if err != nil {
		return "", microerror.Mask(err)
	}
	return writer.String(), nil
}
