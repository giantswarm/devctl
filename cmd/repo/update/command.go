// Package update is `devctl repo update`: a declared repository's team-file
// entry changed.
package update

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
)

const (
	name      = "update"
	shortDesc = "Change a declared repository's team-file entry"
	longDesc  = `Change the configuration of a declared repository: its team-file entry is
replaced by the entry you pass, in a pull request opened as you that the
team reviews; the reconciler applies the change after the merge. The entry
is validated against the repositories schema.

--set changes fields of the entry as the inventory holds it: a dotted path
and a YAML value (gen.ci.generate=false, align=true, description="What it
does", gen.flavours=[app, k8sapi]); --unset removes one. --entry-file passes
the whole entry as YAML or JSON instead (- for stdin). Deprecate, archive or
delete with ` + "`devctl repo set-lifecycle`" + `, move to another team with
` + "`devctl repo transfer`" + `.

Examples:
  devctl repo update my-service --set align=true --dry-run
  devctl repo update my-service --set gen.ci.generate=false --set description="What it does"
  devctl repo update my-service --entry-file entry.yaml --reason "the fork flavour"`
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
	Stdin  io.Reader
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	if config.Stdin == nil {
		config.Stdin = os.Stdin
	}

	f := &flag{}
	r := &runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, stdin: config.Stdin, open: client.Open}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY", name),
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

type flag struct {
	write.Flags
	Set       []string
	Unset     []string
	EntryFile string
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd, "write nothing")
	cmd.Flags().StringArrayVar(&f.Set, "set", nil, "A field of the entry as path=value, the path dotted (gen.ci.generate=false), the value YAML; repeatable.")
	cmd.Flags().StringArrayVar(&f.Unset, "unset", nil, "A field of the entry to remove, the path dotted; repeatable.")
	cmd.Flags().StringVar(&f.EntryFile, "entry-file", "", "The whole entry as YAML or JSON (- for stdin), instead of --set and --unset.")
}

func (f *flag) Validate() error {
	if err := f.Flags.Validate(); err != nil {
		return microerror.Mask(err)
	}
	if f.EntryFile == "" && len(f.Set) == 0 && len(f.Unset) == 0 {
		return microerror.Maskf(client.InvalidFlagError, "nothing to change: pass --set, --unset or --entry-file")
	}
	if f.EntryFile != "" && (len(f.Set) > 0 || len(f.Unset) > 0) {
		return microerror.Maskf(client.InvalidFlagError, "--entry-file replaces the whole entry; --set and --unset change the inventory's: pass one or the other")
	}
	for _, s := range f.Set {
		if _, _, err := splitSet(s); err != nil {
			return microerror.Mask(err)
		}
	}
	return nil
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
