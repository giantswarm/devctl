package failed

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

const command = "release failed"

type Config struct {
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	r := &runner{stdout: config.Stdout, gate: versiongate.Check, open: openGitHub}
	c := &cobra.Command{
		Use:   "failed <owner/repo>",
		Short: "List releases marked not published by a CLI release wait; one JSON document.",
		Long: `List GitHub releases whose notes carry devctl's not-published notice.

The CLI release wait and the release wait after pr merge record a failed,
verified CircleCI tag pipeline in the release notes. A later successful wait
removes that notice. This command reads those recorded notices only; it does
not inspect historical pipelines or infer failure from an empty asset list.

Output: the JSON envelope and repository, releases[{tag, url}]. Exit 0 means
the listing completed, including an empty list; 7 is usage or a read failure,
8 is missing authentication. Reads at most 1000 releases; exceeding that bound
is an error, never a silently incomplete list. No release is changed.`,
		Args:        cobra.ArbitraryArgs,
		RunE:        r.Run,
		Annotations: agentcli.AgentFacing(),
	}
	c.SetFlagErrorFunc(r.FlagError)
	return c, nil
}

func openGitHub(ctx context.Context, owner string, transport http.RoundTripper, warn func(string)) (*githubclient.Client, error) {
	token, err := authexec.RepositoryToken(ctx, owner, authstore.RequireGitHub, authexec.PersonGitHub)
	if err != nil {
		return nil, err
	}
	warn(token.Warning)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	var renew func(context.Context, string) (string, error)
	if authexec.Identity(token) == authexec.IdentityApp {
		renew = authstore.RenewGitHubToken
	}
	client, _, err := githubclient.NewConditional(githubclient.Config{
		AccessToken: token.Value,
		BaseURL:     agentcli.EndpointsFromEnv().GitHubAPIURL,
		Logger:      logger,
		Renew:       renew,
		Transport:   transport,
	})
	return client, err
}
