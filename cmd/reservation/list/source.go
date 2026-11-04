package list

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// Sync permits untracked files. A local-only ConfigMap must not become the
// source of a listing that promises the state recorded on main.
func requireCommittedConfigMap(ctx context.Context, dir, cluster string) error {
	path := filepath.Join("management-clusters", cluster, reservation.ConfigMapFile)
	if _, err := os.Lstat(filepath.Join(dir, path)); errors.Is(err, os.ErrNotExist) {
		// Let the engine explain a missing repository, cluster or opt-in file.
		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "-e", "HEAD:"+filepath.ToSlash(path)) //nolint:gosec // fixed Git operation, validated cluster, explicit checkout
	if output, err := cmd.CombinedOutput(); err != nil {
		return microerror.Maskf(invalidFlagError,
			"cannot list reservations: %s must be recorded in synced main; local files are left untouched: %s: %s",
			path, err, strings.TrimSpace(string(output)))
	}

	return nil
}
