// Package changelog implements `devctl changelog cut` and `devctl changelog
// check`, which keep CHANGELOG.md's Unreleased section to the changes no
// release carries yet.
package changelog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/changelog"
)

const (
	name            = "changelog"
	description     = "Keep CHANGELOG.md's Unreleased section to the changes no release carries yet."
	longDescription = `Keep CHANGELOG.md's Unreleased section to the changes no release carries yet.

Auto Release tags a release from every releasable merge, and the entries the
merged pull request added under Unreleased are part of it: the CHANGELOG.md of
the tag lists them under Unreleased. An entry is released by the first release,
in version order among the "v" semver tags reachable from HEAD, whose
CHANGELOG.md holds it under Unreleased; the tags on one commit are one release,
named by the highest version (a promoted release candidate by its final
version).

  cut    moves every released entry into its release's section, creating the
         section and its link reference where they are missing. Every other
         section keeps its bytes.
  check  fails while Unreleased holds a released entry, listing each. With
         --base, an entry the base revision already holds released under
         Unreleased is listed as waiting for the base's own cut and does not
         fail: a pull request is checked on its merge with the base.

Run them in a clone with its tags: a shallow clone or one without tags sees no
release. The generated Auto Release workflow opens the cut as a pull request
after every release; its changelog check fails a pull request while released
entries the pull request brings remain under Unreleased.`

	flagBase = "base"
	flagFile = "file"
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

type flag struct {
	Base string
	File string
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}
	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  longDescription,
	}
	c.PersistentFlags().StringVar(&f.File, flagFile, "CHANGELOG.md", "The changelog, relative to the working directory, which must be inside the repository")

	c.AddCommand(&cobra.Command{
		Use:   "cut",
		Args:  cobra.NoArgs,
		Short: "Move every released entry out of Unreleased into its release's section.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return microerror.Mask(run(f, config.Stdout, false))
		},
	})
	check := &cobra.Command{
		Use:   "check",
		Args:  cobra.NoArgs,
		Short: "Fail while Unreleased holds an entry a release already carried.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return microerror.Mask(run(f, config.Stdout, true))
		},
	}
	check.Flags().StringVar(&f.Base, flagBase, "", "A pull request's base revision: entries it already holds released under Unreleased wait for its own cut and do not fail the check")
	c.AddCommand(check)

	return c, nil
}

func run(f *flag, stdout io.Writer, check bool) error {
	if filepath.IsAbs(f.File) || strings.HasPrefix(filepath.Clean(f.File), "..") {
		return microerror.Maskf(invalidFlagError, "--%s must be a path inside the working directory, got %q", flagFile, f.File)
	}
	raw, err := os.ReadFile(f.File)
	if err != nil {
		return microerror.Mask(err)
	}
	if !changelog.HasUnreleased(string(raw)) {
		fmt.Fprintf(stdout, "%s has no %s section: nothing to cut.\n", f.File, changelog.Unreleased)
		return nil
	}
	releases, err := changelog.Releases(".", f.File)
	if err != nil {
		return microerror.Mask(err)
	}
	if check {
		return microerror.Mask(runCheck(f, stdout, string(raw), releases))
	}

	text, moved := changelog.Cut(string(raw), releases)

	if len(moved) == 0 {
		fmt.Fprintf(stdout, "%s: nothing to cut (%d releases read).\n", f.File, len(releases))
		return nil
	}
	info, err := os.Stat(f.File)
	if err != nil {
		return microerror.Mask(err)
	}
	if err := os.WriteFile(f.File, []byte(text), info.Mode().Perm()); err != nil { // #nosec G703 -- the changelog the caller names, held inside the working directory above
		return microerror.Mask(err)
	}
	fmt.Fprintf(stdout, "%s: moved %d entries out of %s.\n", f.File, len(moved), changelog.Unreleased)
	return nil
}

func runCheck(f *flag, stdout io.Writer, text string, releases []changelog.Release) error {
	base := ""
	if f.Base != "" {
		var err error
		base, err = changelog.File(".", f.Base, f.File)
		if err != nil {
			return microerror.Mask(err)
		}
	}
	moved, pending := changelog.Pending(text, base, releases)

	if len(pending) > 0 {
		fmt.Fprintf(stdout, "%s: %d released entries under %s are already on the base %s and wait for its changelog cut pull request:\n", f.File, len(pending), changelog.Unreleased, f.Base)
		for _, m := range pending {
			fmt.Fprintf(stdout, "  %s: %s\n", m.Release, m.Summary(100))
		}
	}
	switch {
	case len(moved) == 0 && len(pending) > 0:
		fmt.Fprintf(stdout, "%s: beside those, %s holds only changes no release carries (%d releases read).\n", f.File, changelog.Unreleased, len(releases))
		return nil
	case len(moved) == 0:
		fmt.Fprintf(stdout, "%s: %s holds only changes no release carries (%d releases read).\n", f.File, changelog.Unreleased, len(releases))
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d entries under %s are already released:\n", f.File, len(moved), changelog.Unreleased)
	for _, m := range moved {
		fmt.Fprintf(&b, "  %s: %s\n", m.Release, m.Summary(100))
	}
	fmt.Fprintf(&b, "Merge the open changelog cut pull request, or run `devctl changelog cut` and commit %s.", f.File)
	return microerror.Maskf(releasedEntriesError, "%s", b.String())
}
