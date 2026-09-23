package create

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/release"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, _ []string) error {
	// One token for every GitHub read of the release: the giantswarm
	// repositories and the public upstream ones.
	token, err := authstore.ResolveGitHub(ctx)
	if err != nil {
		return err
	}
	if token.Warning != "" {
		logrus.Warn(token.Warning)
	}

	creationCommand := fmt.Sprintf("%v", strings.Join(os.Args, " "))

	err = release.CreateRelease(token.Value, r.flag.Name, r.flag.Base, r.flag.Releases, r.flag.Provider, r.flag.Components, r.flag.Apps, r.flag.Overwrite, creationCommand, r.flag.BumpAll, r.flag.Drop, r.flag.Yes, r.flag.Output, r.flag.Verbose, r.flag.ChangesOnly, r.flag.RequestedOnly, r.flag.UpdateExisting, r.flag.PreserveReadme, r.flag.RegenerateReadme, r.flag.ChangelogNoisePatterns)
	if err != nil {
		return githubclient.ExplainNotFound(microerror.Mask(err), authstore.GitHubNotFoundHint(token))
	}
	return nil
}
