package reservation

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/giantswarm/microerror"
)

// MaxPushAttempts caps how many times PushWithRetry redoes render before it
// gives up and reports the failure.
const MaxPushAttempts = 5

// Push pushes dir's current branch to its upstream, using whatever
// credentials are already configured on the remote: a token baked into the
// URL by a githubclient clone in CI, or the checkout's own git credentials on
// a laptop. Reserve and Release share it, so there is one way to push, not
// two. It relies on the upstream tracking a clone -- go-git's or native
// git's -- always sets up, so no caller has to name the branch or the remote.
func Push(ctx context.Context, dir string) error {
	if err := runGit(ctx, dir, "push"); err != nil {
		return microerror.Maskf(pushError, "%s", err)
	}

	return nil
}

// PushWithRetry calls render, which must render, assert and commit exactly as
// Reserve and Release do, and pushes the commit it made. When the push is
// rejected, it fetches and hard-resets dir to its upstream's new tip --
// discarding the commit render just made, not replaying it -- and calls
// render again. A rebase that only replayed the old commit would carry
// whatever it rendered against the old tip; this reruns render against
// whatever landed there first, so a reservation that collided on the same
// kustomization.yaml or ConfigMap is folded in instead of corrupted.
func PushWithRetry(ctx context.Context, dir string, render func() error) error {
	var lastErr error
	for attempt := 1; attempt <= MaxPushAttempts; attempt++ {
		if attempt > 1 {
			if err := rebaseOntoRemote(ctx, dir); err != nil {
				return microerror.Mask(err)
			}
		}

		if err := render(); err != nil {
			return microerror.Mask(err)
		}

		if err := Push(ctx, dir); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	return microerror.Maskf(pushRetriesExhaustedError,
		"push rejected after %d attempts: %s", MaxPushAttempts, lastErr)
}

// rebaseOntoRemote discards the local commit render just made and moves dir
// to match its upstream's new tip, so the next render call starts from
// whatever a rejected push means someone else already landed.
//
// `git reset --hard` cannot be scoped to a set of paths: it always rewrites
// the whole working tree. render's own commit is meant to hold exactly its
// own files (commitAll stages nothing else), so the working tree should be
// clean the instant render returns. If it is not, the extra dirt did not come
// from this operation -- most likely dir defaults to ".", a checkout that
// also holds a developer's own in-progress edits, on a command that never
// clones (release, reap, extend all default --repo-dir to "."). Resetting
// anyway would take that unrelated work down with the stale commit, silently
// and unrecoverably. Refusing is the only safe move: there is no path to
// spare it from a hard reset, so this is the smallest change that actually
// closes the hole.
func rebaseOntoRemote(ctx context.Context, dir string) error {
	if err := runGit(ctx, dir, "fetch"); err != nil {
		return microerror.Mask(err)
	}

	status, err := gitStatusPorcelain(ctx, dir)
	if err != nil {
		return microerror.Mask(err)
	}
	if status != "" {
		return microerror.Maskf(dirtyWorktreeError,
			"refusing to reset %s to its upstream: the working tree carries changes outside the reservation's own commit, which a hard reset would destroy:\n%s",
			dir, status)
	}

	return runGit(ctx, dir, "reset", "--hard", "@{upstream}")
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

// gitStatusPorcelain returns dir's `git status --porcelain` output, trimmed:
// empty means a clean working tree.
func gitStatusPorcelain(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain") //nolint:gosec // dir is trusted, not user data
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git status --porcelain: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
