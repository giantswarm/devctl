package find

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/utils/strings/slices"

	"github.com/giantswarm/devctl/pkg/githubclient"
)

const (
	//nolint:gosec
	githubTokenEnvVar = "GITHUB_TOKEN"
	githubOrg         = "giantswarm"

	// Criteria names
	critHasDocsDirectory          = "HAS_DOCS_DIR"
	critHasPrTemplateInDocs       = "HAS_PR_TEMPLATE_IN_DOCS"
	critReadmeHasOldCircleCiBadge = "README_OLD_CIRCLECI_BAGDE"
	critReadmeHasOldGodocLink     = "README_OLD_GODOC_LINK"
	critNoCodeownersFile          = "NO_CODEOWNERS"
	critCodeownersErrors          = "BAD_CODOWNERS"
	critNoDescription             = "NO_DESCRIPTION"
	critNoReadme                  = "NO_README"
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

	for _, repo := range repos {
		matched := []string{}

		repoMetadata, err := client.GetRepository(ctx, githubOrg, repo.Name)
		if err != nil {
			return microerror.Mask(err)
		}
		defaultBranch := repoMetadata.GetDefaultBranch()

		if !r.flag.IncludeArchived && repoMetadata.GetArchived() {
			// Skip archived repos
			continue
		}

		if !r.flag.IncludeFork && *repoMetadata.Fork {
			// Skip fork repos
			continue
		}

		// Check for matching criteria

		if slices.Contains(r.flag.What, critNoCodeownersFile) || r.flag.MustHaveCodeowners {
			_, _, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "CODEOWNERS", nil)
			if err != nil {
				if r.flag.MustHaveCodeowners {
					// Skip repo without CODEOWNERS file
					continue
				}

				output := fmt.Sprintf("  - /CODEOWNERS file not present (%s)\n", critNoCodeownersFile)
				if repoMetadata.Fork != nil && *repoMetadata.Fork {
					output += fmt.Sprintf("    - Note: this repo is a fork of %s\n", repoMetadata.GetForksURL())
				}
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critCodeownersErrors) {
			errs, resp, err := realClient.Repositories.GetCodeownersErrors(ctx, githubOrg, repo.Name)
			if err != nil {
				if resp != nil && resp.StatusCode == 404 {
					// CODEOWNERS not found in this repo. Do nothing.
				} else {
					return microerror.Mask(err)
				}
			}

			if errs != nil && len(errs.Errors) > 0 {
				output := fmt.Sprintf("  - Errors found in CODEOWNERS files (%s)\n", critCodeownersErrors)
				for _, item := range errs.Errors {
					messageFirstLine := strings.Split(item.Message, "\n")[0]
					output += fmt.Sprintf("    - https://github.com/%s/%s/blob/%s/%s#L%d - %q\n", githubOrg, repo.Name, defaultBranch, item.Path, item.Line, messageFirstLine)
				}
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critHasDocsDirectory) {
			_, items, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "docs", nil)
			if err == nil {
				output := fmt.Sprintf("  - /docs directory exists (%s)\n", critHasDocsDirectory)
				for _, item := range items {
					path := item.GetPath()
					ftype := item.GetType()
					output += fmt.Sprintf("    - %s %s\n", path, ftype)
				}
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critHasPrTemplateInDocs) {
			_, _, _, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "docs/pull_request_template.md", nil)
			if err == nil {
				output := fmt.Sprintf("  - /docs/pull_request_template.md file exists (%s)\n", critHasPrTemplateInDocs)
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critReadmeHasOldCircleCiBadge) || slices.Contains(r.flag.What, critNoReadme) || slices.Contains(r.flag.What, critReadmeHasOldGodocLink) {
			fileContent, _, resp, err := realClient.Repositories.GetContents(ctx, githubOrg, repo.Name, "README.md", nil)

			if (slices.Contains(r.flag.What, critReadmeHasOldCircleCiBadge) || slices.Contains(r.flag.What, critReadmeHasOldGodocLink)) && err == nil {
				decodedContent, _ := b64.StdEncoding.DecodeString(*fileContent.Content)

				if slices.Contains(r.flag.What, critReadmeHasOldCircleCiBadge) {
					if strings.Contains(string(decodedContent), fmt.Sprintf("https://circleci.com/gh/%s", githubOrg)) {
						output := fmt.Sprintf("  - /README.md has old CircleCI badge (%s)\n", critReadmeHasOldCircleCiBadge)
						if repoMetadata.Fork != nil && *repoMetadata.Fork {
							output += fmt.Sprintf("        Note: this repo is a fork of %s\n", repoMetadata.GetForksURL())
						}
						output += fmt.Sprintf("    - Edit via https://github.com/%s/%s/edit/%s/README.md\n", githubOrg, repo.Name, defaultBranch)
						output += fmt.Sprintf("    - Badge code via https://app.circleci.com/settings/project/github/%s/%s/status-badges)\n", githubOrg, repo.Name)
						matched = append(matched, output)
					}
				}

				if slices.Contains(r.flag.What, critReadmeHasOldGodocLink) {
					if strings.Contains(string(decodedContent), "godoc.org") {
						output := fmt.Sprintf("  - /README.md has link to godoc.org (%s)\n", critReadmeHasOldGodocLink)
						output += fmt.Sprintf("    - Should be https://pkg.go.dev/github.com/%s/%s\n", githubOrg, repo.Name)
						output += fmt.Sprintf("    - Edit via https://github.com/%s/%s/edit/%s/README.md\n", githubOrg, repo.Name, defaultBranch)
						matched = append(matched, output)
					}
				}
			}

			if slices.Contains(r.flag.What, critNoReadme) && err != nil && resp.StatusCode == 404 {
				output := fmt.Sprintf("  - /README.md not present (%s)\n", critNoReadme)
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critNoDescription) {
			if repoMetadata.GetDescription() == "" {
				output := fmt.Sprintf("  - Repository has no description (%s)\n", critNoDescription)
				matched = append(matched, output)
			}
		}

		if slices.Contains(r.flag.What, critDefaultBranchMaster) {
			if defaultBranch == "master" {
				output := fmt.Sprintf("  - Default branch is 'master' (%s)\n", critDefaultBranchMaster)
				if repoMetadata.Fork != nil && *repoMetadata.Fork {
					output += fmt.Sprintf("    - Note: this repo is a fork of %s\n", repoMetadata.GetForksURL())
				}
				matched = append(matched, output)
			}
		}

		// Print output per repo
		if len(matched) > 0 {
			matchingReposCount++
			fmt.Printf("\n- [ ] https://github.com/%s/%s\n", githubOrg, repo.Name)
			for _, item := range matched {
				fmt.Print(item)
			}
		}

	}

	fmt.Printf("\nFound %d matching non-archived repositoiries\n", matchingReposCount)

	return nil
}
