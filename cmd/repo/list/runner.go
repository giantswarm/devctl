package list

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/devctl/pkg/depclient"
	"github.com/giantswarm/devctl/pkg/githubclient"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	freshDays = 100
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) RunWithError(cmd *cobra.Command, args []string) error {
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

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var err error

	var github *githubclient.Client
	{
		c := githubclient.Config{
			Logger: r.logger,

			AccessToken: r.flag.GithubAccessToken,
		}

		github, err = githubclient.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var dep *depclient.Client
	{
		c := depclient.Config{
			Logger: r.logger,
		}

		dep, err = depclient.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	var freshGoRepos []githubclient.Repository
	{
		repos, err := github.ListRepositories(ctx, "giantswarm")
		if err != nil {
			return microerror.Mask(err)
		}

		for _, repo := range repos {
			if strings.ToLower(repo.Language) != "go" {
				continue
			}

			yearAgo := time.Now().Add(-freshDays * 24 * time.Hour)
			if yearAgo.After(repo.UpdatedAt) {
				continue
			}

			freshGoRepos = append(freshGoRepos, repo)
		}
	}

	type Result struct {
		Repo string
		Deps []string
	}

	var results []Result
	{
		for _, repo := range freshGoRepos {
			result := Result{
				Repo: repo.Name,
			}

			gopkgToml, err := github.GetFile(ctx, repo.Owner, repo.Name, "Gopkg.toml", "master")
			if githubclient.IsNotFound(err) {
				continue
			} else if err != nil {
				return microerror.Mask(err)
			}

			depMf, err := dep.ReadManifest(ctx, gopkgToml.Data)
			if err != nil {
				return microerror.Mask(err)
			}

			dependencies := append(depMf.Constraints, depMf.Overrides...)
			for _, d := range dependencies {
				if d.Name != r.flag.DependsOn {
					continue
				}

				ver := d.Version
				if ver == "" {
					ver = d.Branch
				}

				result.Deps = append(result.Deps, d.Name+"@"+ver)
			}

			if len(result.Deps) > 0 {
				results = append(results, result)
			}
		}
	}

	fmt.Fprintf(r.stdout, "----> Go repos depending on %q edited in last %d days (%d):\n", r.flag.DependsOn, freshDays, len(results))
	for _, res := range results {
		fmt.Fprintf(r.stdout, "%s\n", res.Repo)
		for _, d := range res.Deps {
			fmt.Fprintf(r.stdout, "\t%s\n", d)
		}
	}

	return nil
}
