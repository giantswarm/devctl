package find

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/devctl/pkg/githubclient"
	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/utils/strings/slices"
)

const (
	githubTokenEnvVar = "GITHUB_TOKEN"
	githubOrg         = "giantswarm"

	// Criteria names
	critHasDocsDirectory          = "HAS_DOCS_DIR"
	critHasPrTemplateInDocs       = "HAS_PR_TEMPLATE_IN_DOCS"
	critReadmeHasOldCircleCiBadge = "README_OLD_CIRCLECI_BAGDE"
	critNoCodeownersFile          = "NO_CODEOWNERS"
	critNoDescription             = "NO_DESCRIPTION"
	critDefaultBranchMaster       = "DEFAULT_BRANCH_MASTER"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
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
	token, found := os.LookupEnv(githubTokenEnvVar)
	if !found {
		return microerror.Maskf(envVarNotFoundError, "environement variable %#q was not found", githubTokenEnvVar)
	}

	if len(r.flag.What) < 1 {
		return microerror.Maskf(invalidConfigError, "no search criteria specified via --what flag")
	}

	c := githubclient.Config{
		Logger:      r.logger,
		AccessToken: token,
	}

	client, err := githubclient.New(c)
	if err != nil {
		return microerror.Mask(err)
	}

	repos, err := client.ListRepositories(ctx, githubOrg)
	if err != nil {
		return microerror.Mask(err)
	}

	realClient := client.GetUnderlyingClient(ctx)

	matchingReposCount := 0

	for i, repo := range repos {
		matched := []string{}

		repoMetadata, err := client.GetRepository(ctx, githubOrg, repo.Name)
		if err != nil {
			return microerror.Mask(err)
		}

		if !r.flag.IncludeArchived && repoMetadata.GetArchived() {
			// Skip archived repos
			continue
		}

		if !r.flag.IncludeFork && *repoMetadata.Fork {
			// Skip fork repos
			continue
		}

		// Check for matching criteria

		if slices.Contains(r.flag.What, critHasDocsDirectory) {
			_, items, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "docs", nil)
			if err == nil {
				output := fmt.Sprintf("    /docs directory exists (%s)\n", critHasDocsDirectory)
				for _, item := range items {
					path := item.GetPath()
					ftype := item.GetType()
					output += fmt.Sprintf("        %s %s\n", path, ftype)
				}
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critHasPrTemplateInDocs) {
			_, _, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "docs/pull_request_template.md", nil)
			if err == nil {
				output := fmt.Sprintf("    /docs/pull_request_template.md file exists (%s)\n", critHasPrTemplateInDocs)
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critReadmeHasOldCircleCiBadge) {
			fileContent, _, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "README.md", nil)
			if err == nil {
				decodedContent, _ := b64.StdEncoding.DecodeString(*fileContent.Content)
				if strings.Contains(string(decodedContent), fmt.Sprintf("https://circleci.com/gh/%s", githubOrg)) {
					output := fmt.Sprintf("    /README.md has old CircleCI badge (%s)\n", critReadmeHasOldCircleCiBadge)
					if repoMetadata.Fork != nil && *repoMetadata.Fork {
						output += fmt.Sprintf("        Note: this repo is a fork of %s\n", repoMetadata.GetForksURL())
					}
					output += fmt.Sprintf("        Edit via https://github.com/%s/%s/edit/%s/README.md\n", githubOrg, repo.Name, repoMetadata.GetDefaultBranch())
					output += fmt.Sprintf("        Badge code via https://app.circleci.com/settings/project/github/%s/%s/status-badges)\n", githubOrg, repo.Name)
					matched = append(matched, output)
				}
			}
		}

		if slices.Contains(r.flag.What, critNoCodeownersFile) {
			_, _, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "CODEOWNERS", nil)
			if err != nil {
				output := fmt.Sprintf("    /CODEOWNERS file not present (%s)\n", critNoCodeownersFile)
				if repoMetadata.Fork != nil && *repoMetadata.Fork {
					output += fmt.Sprintf("        Note: this repo is a fork of %s\n", repoMetadata.GetForksURL())
				}
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critNoDescription) {
			if repoMetadata.GetDescription() == "" {
				output := fmt.Sprintf("    Repository has no description (%s)\n", critNoDescription)
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critDefaultBranchMaster) {
			if repoMetadata.GetDefaultBranch() == "master" {
				output := fmt.Sprintf("    Default branch is 'master' (%s)\n", critDefaultBranchMaster)
				if repoMetadata.Fork != nil && *repoMetadata.Fork {
					output += fmt.Sprintf("        Note: this repo is a fork of %s\n", repoMetadata.GetForksURL())
				}
				matched = append(matched, output)
			}
		}

		// Print output per repo
		if len(matched) > 0 {
			matchingReposCount++
			fmt.Printf("\n(%d of %d) https://github.com/%s/%s\n", i, len(repos), githubOrg, repo.Name)
			for _, item := range matched {
				fmt.Print(item)
			}
		}

	}

	fmt.Printf("\nFound %d matching non-archived repositoiries\n", matchingReposCount)

	return nil
}
