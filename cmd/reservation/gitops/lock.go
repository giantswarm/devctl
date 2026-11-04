package gitops

import (
	"context"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// lockCheckout uses the checkout's git directory, so alternate paths and
// symlinks to the same checkout share one lock. The lock file stays in place:
// removing it could let a new opener lock a different inode while one is held.
func lockCheckout(ctx context.Context, dir string) (*os.File, error) {
	gitDir, err := git(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, microerror.Maskf(syncError, "cannot locate the git directory in %s: %s", dir, err)
	}
	file, err := os.OpenFile(filepath.Join(gitDir, "devctl-reservation.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, microerror.Maskf(syncError, "cannot open the reservation lock in %s: %s", dir, err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, microerror.Maskf(syncError, "cannot lock %s for this reservation command (another command may be using it): %s", dir, err)
	}
	return file, nil
}

// Close releases ownership of the checkout. Read-only callers use Close;
// mutating callers use Finish, which holds ownership through dry-run cleanup.
// The operating system also releases ownership when the process exits.
func (c *Checkout) Close() error {
	if c.lock == nil {
		return nil
	}
	file := c.lock
	c.lock = nil
	if err := file.Close(); err != nil {
		return microerror.Maskf(syncError, "cannot close the reservation lock in %s: %s", c.Dir, err)
	}
	return nil
}
