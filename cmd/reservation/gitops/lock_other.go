//go:build !unix && !windows

package gitops

import (
	"fmt"
	"os"
)

func lockFile(_ *os.File) error {
	return fmt.Errorf("reservation checkout locking is unsupported on this platform")
}
