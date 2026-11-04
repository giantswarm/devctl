// Package gitops gives the reservation commands one way to find the GitOps
// repo: a checkout on disk, fast-forwarded to origin/main before any command
// logic runs. The tip of main is the only source of truth; no command reads or
// writes a cluster.
package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const (
	FlagRepo   = "gitops-repo"
	FlagDir    = "gitops-dir"
	FlagDryRun = "dry-run"
	FlagOutput = "output"

	OutputTable = "table"
	OutputJSON  = "json"

	// EnvTargetDir is the umbrella checkout directory; the GitOps repo lives in
	// its cmc/<repo> subdirectory.
	EnvTargetDir = "UMBRELLA_TARGET_DIR"

	// DefaultRepo holds the configuration of every Giant Swarm management
	// cluster.
	DefaultRepo = reservation.DefaultGitOpsRepository

	branch = "main"
)

// CheckoutHelp is the paragraph every reservation command's long help
// carries about the GitOps checkout.
const CheckoutHelp = `The command works on a checkout of the GitOps repository: --gitops-dir,
or $` + EnvTargetDir + `/cmc/<repo> when that is empty, where <repo> is the
name part of --gitops-repo. It never clones. First it fast-forwards the
checkout to origin/main. It stops when the checkout is not on main, has changes
to tracked files, or has local commits that are not on origin/main. It leaves
untracked files alone. Git uses the checkout's own credentials.`

// Flags select the GitOps repo and where its checkout is, and the flags every
// reservation command shares: how to print the result and, for a command that
// pushes, whether to push.
type Flags struct {
	Repo   string
	Dir    string
	DryRun bool
	Output string
}

func (f *Flags) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Repo, FlagRepo, DefaultRepo, "GitOps repository holding the management cluster, as owner/repo. Only its name selects the checkout under $"+EnvTargetDir+"/cmc/.")
	cmd.Flags().StringVar(&f.Dir, FlagDir, "", "Checkout of the GitOps repository. Empty takes $"+EnvTargetDir+"/cmc/<repo>. The command fast-forwards it to origin/main first, and refuses a checkout that is not on a clean main.")
	cmd.Flags().StringVar(&f.Output, FlagOutput, OutputTable, "Output format: table or json.")
}

// InitDryRun adds --dry-run, for a command that pushes.
func (f *Flags) InitDryRun(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.DryRun, FlagDryRun, false, "Make the commits in the GitOps checkout, show them on stderr, then drop them. Nothing is pushed.")
}

func (f *Flags) Validate() error {
	if f.Output != OutputTable && f.Output != OutputJSON {
		return microerror.Maskf(invalidFlagError, "--%s %q: want %s or %s", FlagOutput, f.Output, OutputTable, OutputJSON)
	}
	_, err := f.repoName()
	return microerror.Mask(err)
}

// JSON reports whether --output asks for JSON.
func (f *Flags) JSON() bool {
	return f.Output == OutputJSON
}

// WriteJSON prints v as indented JSON.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return microerror.Mask(enc.Encode(v))
}

func (f *Flags) repoName() (string, error) {
	owner, name, ok := strings.Cut(f.Repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", microerror.Maskf(invalidFlagError, "--%s must be owner/repo, got %q", FlagRepo, f.Repo)
	}

	return name, nil
}

// Resolve returns the checkout directory and a description of what selected
// it. --gitops-dir wins, then $UMBRELLA_TARGET_DIR/cmc/<repo>; there is no
// fallback clone.
func (f *Flags) Resolve() (dir, source string, err error) {
	if f.Dir != "" {
		return f.Dir, "--" + FlagDir, nil
	}

	target := os.Getenv(EnvTargetDir)
	if target == "" {
		return "", "", microerror.Maskf(invalidFlagError,
			"cannot find the GitOps checkout: pass --%s, or set %s so that $%s/cmc/<repo> is it", FlagDir, EnvTargetDir, EnvTargetDir)
	}
	name, err := f.repoName()
	if err != nil {
		return "", "", microerror.Mask(err)
	}

	return filepath.Join(target, "cmc", name), "$" + EnvTargetDir + "/cmc/" + name, nil
}

// Checkout is a GitOps checkout that has been synced to origin/main.
type Checkout struct {
	Dir    string
	source string
	flags  *Flags
}

// Open resolves the checkout and syncs it to origin/main. It runs before any
// command logic, list included. Git uses the checkout's own credentials.
func (f *Flags) Open(ctx context.Context, stderr io.Writer) (*Checkout, error) {
	dir, source, err := f.Resolve()
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, microerror.Maskf(invalidFlagError, "the GitOps checkout %s (from %s) does not exist or is not a directory", dir, source)
	}

	if err := Sync(ctx, dir, stderr); err != nil {
		return nil, microerror.Mask(err)
	}

	return &Checkout{Dir: dir, source: source, flags: f}, nil
}

// Explain turns the errors the user can act on into messages that say so: a
// directory that is not a GitOps repo names what selected it, and a push that
// gave up says the change is not in effect and how to clear the local commit.
// Any other error comes back as it is.
func (c *Checkout) Explain(err error) error {
	switch {
	case reservation.IsNotGitOpsRepo(err):
		return microerror.Maskf(invalidFlagError,
			"%s (from %s) is not the GitOps repo that holds the management cluster configuration: it has no management-clusters directory. Point --%s at a checkout of that repo, for example %s",
			c.Dir, c.source, FlagDir, DefaultRepo)
	case reservation.IsPushFailed(err):
		return microerror.Maskf(pushFailedError,
			"%s\n\nThe reservation change is NOT in effect: nothing reached origin. The commit stays on local main in %s, and the next devctl run refuses to work on it until it is gone. To drop it, run:\n\n  %s",
			err, c.Dir, resetCommand(c.Dir))
	}

	return err
}

// Wrote tells the user that a pushed change still has to be applied by Flux.
// A dry run pushed nothing, so it says nothing.
func (c *Checkout) Wrote(stderr io.Writer) {
	if c.flags.DryRun {
		return
	}
	_, _ = fmt.Fprintln(stderr, "Flux applies the change to the cluster in a few minutes.")
}

// Finish ends the command. After a dry run it shows the commits the command
// made on stderr and resets the checkout to origin/main, even when err is
// set. If tracked files changed since the command committed, it refuses the
// reset to preserve that work. A cleanup failure is reported alongside err.
func (c *Checkout) Finish(ctx context.Context, stderr io.Writer, err error) error {
	if !c.flags.DryRun {
		return err
	}

	log, logErr := git(ctx, c.Dir, "log", "-p", "--reverse", "origin/"+branch+"..HEAD")
	switch {
	case logErr != nil:
		_, _ = fmt.Fprintf(stderr, "Dry run: cannot show the commits: %s\n", logErr)
	case log == "":
		_, _ = fmt.Fprintln(stderr, "Dry run: no commit to push.")
	default:
		_, _ = fmt.Fprintf(stderr, "%s\n\nDry run: nothing was pushed.\n", log)
	}

	status, statusErr := git(ctx, c.Dir, "status", "--porcelain", "--untracked-files=no")
	if statusErr != nil {
		return errors.Join(err, microerror.Maskf(syncError, "dry run: cannot check whether it is safe to drop the commits: %s. The checkout was left untouched", statusErr))
	}
	if status != "" {
		return errors.Join(err, microerror.Maskf(syncError,
			"dry run: refusing to drop the commits in %s because tracked files have changed; a reset could destroy this work. Preserve these changes before dropping the dry-run commits:\n%s", c.Dir, status))
	}

	// --keep also protects edits to paths being restored if they arrive after
	// the status check.
	if _, resetErr := git(ctx, c.Dir, "reset", "--keep", "origin/"+branch); resetErr != nil {
		return errors.Join(err, microerror.Maskf(syncError, "dry run: cannot drop the commits: %s. After preserving any local changes, drop them with:\n\n  %s", resetErr, resetCommand(c.Dir)))
	}
	if log != "" {
		_, _ = fmt.Fprintf(stderr, "Dry run: dropped these commits from %s.\n", c.Dir)
	}

	return err
}

func resetCommand(dir string) string {
	// Leave simple paths readable, and quote the rest as one POSIX shell word.
	if strings.ContainsAny(dir, " \t\r\n'\"\\`$;&|<>()*?[]{}!~#") {
		dir = "'" + strings.ReplaceAll(dir, "'", "'\"'\"'") + "'"
	}
	return "git -C " + dir + " reset --hard origin/" + branch
}

// Sync brings dir to the tip of origin/main, or stops with the reason it
// cannot do that safely.
func Sync(ctx context.Context, dir string, stderr io.Writer) error {
	if _, err := git(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return microerror.Maskf(syncError, "%s is not a git checkout: %s", dir, err)
	}

	current, err := git(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		current = "a detached HEAD"
	}
	if current != branch {
		return microerror.Maskf(syncError,
			"%s is on branch %s, not %s; devctl cannot safely sync the GitOps repo. Check out %s there, or point --%s at another checkout",
			dir, current, branch, branch, FlagDir)
	}

	// Untracked files are the user's: devctl never stages them, and a
	// fast-forward that would overwrite one must fail, including ignored files.
	status, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return microerror.Maskf(syncError, "%s: %s", dir, err)
	}
	if status != "" {
		return microerror.Maskf(syncError,
			"%s is dirty; devctl cannot safely sync the GitOps repo. Commit, stash or discard these changes first:\n%s", dir, status)
	}

	// An explicit refspec, so a shallow checkout with no configured fetch
	// refspec (a CI checkout of depth 1) still updates origin/main.
	if _, err := git(ctx, dir, "fetch", "origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
		return microerror.Maskf(syncError, "cannot fetch origin %s in %s: %s", branch, dir, err)
	}

	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return microerror.Maskf(syncError, "%s: %s", dir, err)
	}
	remote, err := git(ctx, dir, "rev-parse", "origin/"+branch)
	if err != nil {
		return microerror.Maskf(syncError, "%s: %s", dir, err)
	}
	if head == remote {
		return nil
	}

	// HEAD is an ancestor of origin/main: behind, so a fast-forward is safe.
	// This holds for a shallow checkout too, as the fetch connects the new
	// commits to the commit HEAD already has.
	if _, err := git(ctx, dir, "merge-base", "--is-ancestor", "HEAD", "origin/"+branch); err != nil {
		// Not an ancestor: ahead or diverged. In a shallow checkout the count
		// may include commits older than the shallow boundary.
		ahead, _ := git(ctx, dir, "rev-list", "--count", "origin/"+branch+"..HEAD")
		return microerror.Maskf(syncError,
			"%s has %s local commit(s) that are not on origin/%s; devctl cannot safely sync the GitOps repo, and every run refuses until they are gone. If they are not needed, run:\n\n  %s",
			dir, ahead, branch, resetCommand(dir))
	}

	if _, err := git(ctx, dir, "merge", "--ff-only", "--no-overwrite-ignore", "origin/"+branch); err != nil {
		return microerror.Maskf(syncError, "cannot fast-forward %s to origin/%s: %s", dir, branch, err)
	}
	short, _ := git(ctx, dir, "rev-parse", "--short", "HEAD")
	_, _ = fmt.Fprintf(stderr, "Updated %s to origin/%s at %s.\n", dir, branch, short)

	return nil
}

// git runs git in dir and returns its trimmed stdout. An error carries stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // dir and args are trusted, not user data
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
