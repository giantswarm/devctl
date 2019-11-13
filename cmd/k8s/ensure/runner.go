package ensure

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/giantswarm/devctl/pkg/githubclient"
	"github.com/giantswarm/devctl/pkg/mod"
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

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	repo := r.flag.Repo
	if repo == "" {
		var err error
		repo, err = os.Getwd()
		if err != nil {
			return microerror.Mask(err)
		}
	}

	kubernetesVersion, err := mod.ReadKubernetesVersion(repo)
	if err != nil {
		return microerror.Mask(err)
	}
	kubernetesVersionTag := fmt.Sprintf("kubernetes-%s", kubernetesVersion)

	gomodData, err := mod.ReadGomod(repo)
	if err != nil {
		return microerror.Mask(err)
	}

	gomod, err := modfile.Parse(path.Join(repo, "go.mod"), gomodData, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	k8sDependencies := mod.KnownKubernetesDependencies()
	foundK8sDependencies := []string{}
	emptyVersion := "v0.0.0"
	for _, require := range gomod.Require {
		for _, k8sDependency := range k8sDependencies {
			if require.Mod.Path == k8sDependency {
				if require.Mod.Version != emptyVersion {
					return microerror.Maskf(mismatchedDependencyError, "Kubernetes dependency %s in require block has pseudo-version %s, expected %s", require.Mod.Path, require.Mod.Version, emptyVersion)
				}
				foundK8sDependencies = append(foundK8sDependencies, require.Mod.Path)
			}
		}
	}

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

	versions := map[string]string{}
	for _, k8sDependency := range k8sDependencies {
		split := strings.Split(k8sDependency, "/")
		repo := split[1]
		commit, err := github.FindCommitByTag(ctx, "kubernetes", repo, kubernetesVersionTag)
		if err != nil {
			return microerror.Mask(err)
		}
		commitTimestamp := commit.Committer.Date.UTC().Format("20060102150405")
		commitSHA := *commit.SHA
		commitSHAShort := commitSHA[:12]
		dependencyVersion := fmt.Sprintf("%s-%s-%s", emptyVersion, commitTimestamp, commitSHAShort)
		versions[k8sDependency] = dependencyVersion
	}

	for _, replace := range gomod.Replace {
		for _, k8sDependency := range k8sDependencies {
			if replace.Old.Path == k8sDependency {
				expected := versions[k8sDependency]
				if replace.New.Version != expected {
					return microerror.Maskf(mismatchedDependencyError, "Kubernetes dependency %s in replace block has pseudo-version %s, expected %s", replace.Old.Path, replace.New.Version, expected)
				}
			}
		}
	}

	return nil
}
