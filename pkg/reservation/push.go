package reservation

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/giantswarm/microerror"
)

// Push runs `git push` for branch in dir, using whatever credentials are
// already configured on its origin remote: a token baked into the URL by a
// githubclient clone in CI, or the checkout's own git credentials on a
// laptop. Reserve and Release share it, so there is one way to push, not two.
func Push(ctx context.Context, dir, branch string) error {
	if err := runGit(ctx, dir, "push", "origin", branch); err != nil {
		return microerror.Maskf(pushError, "%s", err)
	}

	return nil
}

// PushWithRetry calls render, which must render, assert and commit exactly as
// Reserve and Release do, and pushes the commit it made.
func PushWithRetry(ctx context.Context, dir, branch string, render func() error) error {
	if err := render(); err != nil {
		return microerror.Mask(err)
	}

	return Push(ctx, dir, branch)
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // dir and args are trusted, not user data
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return nil
}
