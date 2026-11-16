package checkextras

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const (
	name             = "check-extras"
	shortDescription = "Check that reservation blocks are the last patches of extras kustomizations."
	longDescription  = `Check that reservation blocks are the last patches of extras kustomizations.

reserve writes each extras reservation as a marked block at the end of the root
patches list of management-clusters/<cluster>/extras/kustomization.yaml. A patch
a human adds after such a block applies after the reservation and can change
the source again, so no dev build matches. This command fails on any such
patch, so CI can refuse the change.

It reads only the files it is given: no GitOps checkout, no cluster, no token.`
	example = `  devctl reservation check-extras management-clusters/graveler/extras/kustomization.yaml`
)

type Config struct {
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	return &cobra.Command{
		Use:     name + " <kustomization.yaml>...",
		Args:    cobra.MinimumNArgs(1),
		Short:   shortDescription,
		Long:    longDescription,
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, path := range args {
				if err := reservation.CheckExtrasPatches(path); err != nil {
					return microerror.Mask(err)
				}
				if _, err := fmt.Fprintf(config.Stdout, "%s: ok\n", path); err != nil {
					return microerror.Mask(err)
				}
			}
			return nil
		},
	}, nil
}
