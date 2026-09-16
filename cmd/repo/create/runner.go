package create

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/auth"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

// output is what the command prints with --output json: the dry run and,
// once opened, the pull request.
type output struct {
	DryRun      *reposetup.Result `json:"dryRun"`
	PullRequest string            `json:"pullRequest,omitempty"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx))
}

func (r *runner) run(ctx context.Context) error {
	// The dry run is the command's output and stdout is for it alone.
	r.logger.SetOutput(r.stderr)

	token, source, err := auth.GitHubToken(ctx, r.flag.GithubTokenEnvVar)
	if err != nil {
		return microerror.Mask(err)
	}
	r.logger.Debugf("GitHub token from %s", source)

	client, err := githubclient.New(githubclient.Config{Logger: r.logger, AccessToken: token})
	if err != nil {
		return microerror.Mask(err)
	}
	remote := reposetup.Remote{GitHub: client.GetUnderlyingClient(ctx)}

	// The team file first: an unknown team ends the command here.
	teamFile, err := remote.TeamFile(ctx, r.flag.Team)
	if err != nil {
		return microerror.Mask(err)
	}

	declaration, err := reposetup.Creation{
		Name:          r.flag.Name,
		ComponentType: r.flag.ComponentType,
		Description:   r.flag.Description,
		Visibility:    r.flag.Visibility,
		Flavours:      r.flag.Flavours,
		Language:      r.flag.Language,
	}.Declaration()
	if err != nil {
		return microerror.Mask(err)
	}
	content, err := reposetup.InsertEntry(teamFile.Team, teamFile.Content, declaration)
	if err != nil {
		return microerror.Mask(err)
	}
	changed, err := reposetup.ParseTeamFile(teamFile.Team, bytes.NewReader(content))
	if err != nil {
		return microerror.Mask(err)
	}

	schema, err := reposetup.FetchSchema(ctx, client)
	if err != nil {
		r.logger.Warnf("cannot read the repositories schema from %s (%v): validating against the embedded copy", remote.Slug(), err)
		schema, err = reposetup.EmbeddedSchema()
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// The guard's inputs are read from GitHub as the person; a token that
	// cannot read the organisation's teams gets no guard, not a wrong one.
	person, err := remote.Person(ctx, r.flag.Owner)
	if err != nil {
		if person.Login == "" {
			return microerror.Mask(err)
		}
		r.logger.Warnf("cannot read your team memberships in %s (%v): the notice about the team's review is not given", r.flag.Owner, err)
		person = reposetup.Person{}
	}

	validator := reposetup.Validator{
		Schema: schema,
		Names:  reposetup.GitHubNameChecker{Repositories: client},
		Owner:  r.flag.Owner,
	}
	result, err := validator.Validate(ctx, reposetup.Request{
		TeamFile:    changed,
		Names:       []string{r.flag.Name},
		Author:      person.Login,
		AuthorTeams: person.Teams,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	out := output{DryRun: result}
	if r.flag.Output == outputText {
		r.printDryRun(teamFile, result)
	}
	if !result.Accepted {
		r.print(out)
		return microerror.Maskf(refusedError, "%s is refused; the problems name the fields, no pull request was opened", r.flag.Name)
	}
	if r.flag.DryRun {
		if r.flag.Output == outputText {
			fmt.Fprintln(r.stdout, "dry run: no pull request opened")
		}
		r.print(out)
		return nil
	}

	pr, err := remote.OpenPullRequest(ctx, reposetup.CreationPullRequest(teamFile, content, result))
	if err != nil {
		return microerror.Mask(err)
	}
	out.PullRequest = pr.GetHTMLURL()
	if r.flag.Output == outputText {
		fmt.Fprintf(r.stdout, "pull request: %s\n", out.PullRequest)
	}
	r.print(out)

	return nil
}

// printDryRun writes the dry run as text: the entry as it goes into the
// team file, the template, the name check, the problems and the notices.
func (r *runner) printDryRun(tf *reposetup.RemoteTeamFile, result *reposetup.Result) {
	entry := result.Entries[0]
	fmt.Fprintf(r.stdout, "%s/%s in %s of %s (schema: %s)\n\n", r.flag.Owner, entry.Name, tf.Path, tf.Team, result.Schema)
	fmt.Fprint(r.stdout, entry.Rendered)
	fmt.Fprintln(r.stdout)
	if entry.Template != "" {
		fmt.Fprintf(r.stdout, "template:   %s\n", entry.Template)
	}
	check := string(entry.NameCheck.Verdict)
	if entry.NameCheck.Detail != "" {
		check += " -- " + entry.NameCheck.Detail
	}
	fmt.Fprintf(r.stdout, "name check: %s\n", check)
	if entry.Accepted {
		fmt.Fprintln(r.stdout, "verdict:    accepted")
	} else {
		fmt.Fprintln(r.stdout, "verdict:    refused")
		for _, p := range entry.Problems {
			fmt.Fprintf(r.stdout, "  %s\n", p)
		}
	}
	if len(result.Notices) > 0 {
		fmt.Fprintln(r.stdout, "notices:")
		for _, n := range result.Notices {
			fmt.Fprintf(r.stdout, "  %s: %s\n", n.Kind, n.Message)
		}
	}
	fmt.Fprintln(r.stdout)
}

// print writes the JSON output; text output is written as it happens.
func (r *runner) print(out output) {
	if r.flag.Output != outputJSON {
		return
	}
	enc := json.NewEncoder(r.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		r.logger.Errorf("writing the output: %v", err)
	}
}
