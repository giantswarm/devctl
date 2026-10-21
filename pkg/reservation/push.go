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

// Push pushes dir's HEAD to main on origin, using whatever credentials are
// already configured on the remote: the checkout's own git credentials (SSH or
// a credential helper on a laptop, the token actions/checkout persists in CI).
// Reserve and Release share it, so there is one way to push, not two. Like the
// sync before every command, it names main and origin explicitly instead of
// relying on a tracking branch.
func Push(ctx context.Context, dir string) error {
	if err := runGit(ctx, dir, "push", "origin", "HEAD:main"); err != nil {
		return microerror.Maskf(pushError, "%s", err)
	}

	return nil
}

// PushWithRetry calls render, which must render, assert and commit exactly as
// Reserve and Release do, and pushes the commit it made. When the push is
// rejected, it fetches and hard-resets dir to origin/main's new tip --
// discarding the commit render just made, not replaying it -- and calls
// render again. A rebase that only replayed the old commit would carry
// whatever it rendered against the old tip; this reruns render against
// whatever landed there first, so a reservation that collided on the same
// kustomization.yaml or ConfigMap is folded in instead of corrupted.
//
// When it gives up -- every attempt rejected, or a push error that is not a
// rejection (no credentials, no network) -- the last commit stays on local
// main and the error is a pushFailedError, so the caller can tell the user
// the change is committed but not pushed. A render error is returned as is.
//
// With dryRun it calls render once and pushes nothing: the commit stays on
// local main for the caller to show and drop.
func PushWithRetry(ctx context.Context, dir string, dryRun bool, render func() error) error {
	if dryRun {
		return microerror.Mask(render())
	}

	var lastErr error
	for attempt := 1; attempt <= MaxPushAttempts; attempt++ {
		if attempt > 1 {
			if err := rebaseOntoRemote(ctx, dir); err != nil {
				if IsDirtyWorktree(err) {
					return microerror.Mask(err)
				}
				return microerror.Maskf(pushFailedError, "%s: the commit is still on local main and was not pushed", err)
			}
		}

		if err := render(); err != nil {
			return microerror.Mask(err)
		}

		err := Push(ctx, dir)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRejection(err) {
			break
		}
	}

	return microerror.Maskf(pushFailedError,
		"push failed: %s: the commit is still on local main and was not pushed", lastErr)
}

// isRejection reports whether err is git refusing a push because the remote
// moved, the only failure a re-render can fix.
func isRejection(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "rejected") || strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "fetch first")
}

// rebaseOntoRemote discards the local commit render just made and moves dir
// to match origin/main's new tip, so the next render call starts from
// whatever a rejected push means someone else already landed.
//
// `git reset --hard` cannot be scoped to a set of paths: it always rewrites
// the whole working tree. render's own commit is meant to hold exactly its
// own files (commitAll stages nothing else), so the working tree should be
// clean the instant render returns. If it is not, the extra dirt did not come
// from this operation -- most likely dir is a developer's own checkout that
// also holds in-progress edits. Resetting
// anyway would take that unrelated work down with the stale commit, silently
// and unrecoverably. Refusing is the only safe move: there is no path to
// spare it from a hard reset, so this is the smallest change that actually
// closes the hole.
func rebaseOntoRemote(ctx context.Context, dir string) error {
	if err := runGit(ctx, dir, "fetch", "origin", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
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

	return runGit(ctx, dir, "reset", "--hard", "origin/main")
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

// gitStatusPorcelain returns dir's `git status --porcelain` output for tracked
// files, trimmed: empty means a clean working tree. Untracked files are the
// user's and a hard reset leaves them alone, so they do not count.
func gitStatusPorcelain(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain", "--untracked-files=no") //nolint:gosec // dir is trusted, not user data
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git status --porcelain: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// discardWrites puts the cluster's ConfigMap and collections back to HEAD and
// removes a component directory Reserve created, after a failure past the
// first write. Best effort: the original error is the one worth reporting.
func discardWrites(dir, cluster, chart string) {
	ctx := context.Background()
	_ = runGit(ctx, dir, "checkout", "HEAD", "--", clusterPath(cluster, ConfigMapFile), clusterPath(cluster, collectionsDir))
	_ = runGit(ctx, dir, "clean", "-fdq", "--", clusterPath(cluster, collectionsDir, reservationsDir, chart))
}
