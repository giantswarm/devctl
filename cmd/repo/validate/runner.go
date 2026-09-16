package validate

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

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

	client, err := r.githubClient()
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

// githubClient is the client for the schema fetch and the name checks, or
// nil without a token.
func (r *runner) githubClient() (*githubclient.Client, error) {
	token := os.Getenv(r.flag.GithubTokenEnvVar)
	if token == "" {
		r.logger.Warnf("no GitHub token in $%s: validating against the embedded schema, repository names are not checked", r.flag.GithubTokenEnvVar)
		return nil, nil
	}

	client, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token,
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
