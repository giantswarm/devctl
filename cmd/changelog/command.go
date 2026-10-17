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
  check  fails while Unreleased holds a released entry, listing each.

Run them in a clone with its tags: a shallow clone or one without tags sees no
release. The generated Auto Release workflow opens the cut as a pull request
after every release; its changelog check fails a pull request while released
entries remain under Unreleased.`

	flagFile = "file"
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

type flag struct {
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
	c.AddCommand(&cobra.Command{
		Use:   "check",
		Args:  cobra.NoArgs,
		Short: "Fail while Unreleased holds an entry a release already carried.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return microerror.Mask(run(f, config.Stdout, true))
		},
	})

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
	text, moved := changelog.Cut(string(raw), releases)

	if check {
		if len(moved) == 0 {
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
