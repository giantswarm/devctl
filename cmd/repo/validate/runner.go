package validate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx))
}

func (r *runner) run(ctx context.Context) error {
	// The dry run is the command's output and stdout is for it alone; the
	// log lines devctl otherwise writes to stdout go to stderr here.
	r.logger.SetOutput(r.stderr)

	teamFile, err := reposetup.ReadTeamFile(r.flag.TeamFile)
	if err != nil {
		return microerror.Mask(err)
	}

	client, err := r.githubClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	schema, err := r.schema(ctx, client)
	if err != nil {
		return microerror.Mask(err)
	}

	validator := reposetup.Validator{
		Schema: schema,
		Owner:  r.flag.Owner,
	}
	if client != nil {
		validator.Names = reposetup.GitHubNameChecker{Repositories: client}
	}

	result, err := validator.Validate(ctx, reposetup.Request{
		TeamFile:    teamFile,
		Names:       r.flag.Entries,
		Mode:        r.flag.mode(),
		Author:      r.flag.Author,
		AuthorTeams: r.flag.AuthorTeams,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	enc := json.NewEncoder(r.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return microerror.Mask(err)
	}

	if !result.Accepted {
		refused := 0
		for _, entry := range result.Entries {
			if !entry.Accepted {
				refused++
			}
		}
		return microerror.Maskf(refusedError, "%d of %d entries refused; the problems name the fields", refused, len(result.Entries))
	}

	return nil
}

// githubClient is the client for the schema fetch and the name checks: the
// App login, or the token in --github-token-envvar (by default
// $DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or $OPSCTL_GITHUB_TOKEN) overriding it.
// nil when neither is there: the data read is public, so no token is not an
// error.
func (r *runner) githubClient(ctx context.Context) (*githubclient.Client, error) {
	var envVars []string
	if r.flag.GithubTokenEnvVar != "" {
		envVars = []string{r.flag.GithubTokenEnvVar}
	}
	token, err := authstore.ResolveGitHub(ctx, envVars...)
	var required *authstore.AuthRequiredError
	if errors.As(err, &required) {
		r.logger.Warnf("no GitHub token (%s): validating against the embedded schema, repository names are not checked", required.Cause)
		return nil, nil
	} else if err != nil {
		return nil, microerror.Mask(err)
	}
	token.WarnOnce(r.stderr)

	client, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token.Value,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return client, nil
}

// schema is --schema when given, else the schema on giantswarm/github main,
// else the embedded copy.
func (r *runner) schema(ctx context.Context, client *githubclient.Client) (*reposetup.Schema, error) {
	if r.flag.Schema != "" {
		data, err := os.ReadFile(filepath.Clean(r.flag.Schema))
		if err != nil {
			return nil, microerror.Mask(err)
		}
		return reposetup.CompileSchema(data, reposetup.SchemaOriginFile)
	}

	if client != nil {
		schema, err := reposetup.FetchSchema(ctx, client)
		if err == nil {
			return schema, nil
		}
		r.logger.Warnf("cannot read the repositories schema from %s/%s@%s (%v): validating against the embedded copy", reposetup.SchemaRepositoryOwner, reposetup.SchemaRepository, reposetup.SchemaRef, err)
	}

	return reposetup.EmbeddedSchema()
}
