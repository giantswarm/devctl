package reconcile

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/engine"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
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

	return microerror.Mask(r.run(ctx, args[0]))
}

func (r *runner) run(ctx context.Context, arg string) error {
	// The result is the command's output and stdout is for it alone.
	r.logger.SetOutput(r.stderr)

	owner, name, err := engine.Slug(arg, r.flag.Owner)
	if err != nil {
		return microerror.Mask(err)
	}

	token, err := engine.Token(r.flag.GithubTokenEnvVar)
	if err != nil {
		return microerror.Mask(err)
	}
	gh, err := engine.GitHubClient(r.logger, r.flag.GithubTokenEnvVar, false)
	if err != nil {
		return microerror.Mask(err)
	}
	dispatch, err := r.dispatchClient(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	ci, err := engine.CircleCIClient(r.logger, r.flag.CircleCITokenEnvVar)
	if err != nil {
		return microerror.Mask(err)
	}

	team, entry, err := r.entry(ctx, gh, name)
	if err != nil {
		return microerror.Mask(err)
	}

	mode := reconcile.ModeRepair
	if r.flag.DryRun {
		mode = reconcile.ModeCheck
	}
	var steps []reconcile.Step
	for _, s := range r.flag.Steps {
		steps = append(steps, reconcile.Step(strings.TrimSpace(s)))
	}
	req := reconcile.Request{
		Owner:         owner,
		Team:          team,
		Entry:         entry,
		Added:         r.flag.Added,
		Mode:          mode,
		Steps:         steps,
		RenderOptions: r.flag.Options,
		// nil: the protection step reads the pipeline from the repository.
		Pipeline: nil,
	}

	if !entry.Accepted {
		// The declaration is at fault, not the run: the refusal is the
		// result, a finding per problem, for the callers to parse.
		r.logger.Warnf("entry %q of %s is refused: %d problem(s); nothing runs", name, r.flag.TeamFile, len(entry.Problems))
		return microerror.Mask(engine.Report(r.stdout, reconcile.Refused(req, time.Now()), r.flag.Output))
	}

	baseline := reconcile.DefaultBaseline()
	baseline.EnforceAdmins = r.flag.EnforceAdmins
	runner := reconcile.Runner{
		GitHub:   gh.GetUnderlyingClient(ctx),
		Dispatch: dispatch,
		Checks:   gh,
		CircleCI: ci,
		// The token downloads the templates: giantswarm/template is private.
		Renderer: reposetup.Renderer{
			Templates: reposetup.GitHubTemplates{Token: token},
			Log:       engine.LogWriter(r.logger),
		},
		Baseline: &baseline,
		Log:      engine.LogWriter(r.logger),
	}
	res, err := runner.Run(ctx, req)
	if err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(engine.Report(r.stdout, res, r.flag.Output))
}

// dispatchClient is the client of --dispatch-token-envvar, the one the
// catalog step dispatches with; nil without the flag, and the GitHub token
// dispatches.
func (r *runner) dispatchClient(ctx context.Context) (*github.Client, error) {
	if r.flag.DispatchTokenEnvVar == "" {
		return nil, nil
	}
	client, err := engine.GitHubClient(r.logger, r.flag.DispatchTokenEnvVar, false)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return client.GetUnderlyingClient(ctx), nil
}

// entry is the desired state: the repository's entry of --team-file,
// validated the way the reconciler validates it (refused when not accepted:
// the caller reports the problems), or the undeclared entry of --team.
func (r *runner) entry(ctx context.Context, gh *githubclient.Client, name string) (string, reposetup.Entry, error) {
	if r.flag.TeamFile == "" {
		return r.flag.Team, reposetup.UndeclaredEntry(reposetup.Undeclared{Name: name, ComponentType: r.flag.ComponentType}), nil
	}

	teamFile, err := reposetup.ReadTeamFile(r.flag.TeamFile)
	if err != nil {
		return "", reposetup.Entry{}, microerror.Mask(err)
	}
	schema, err := engine.Schema(ctx, r.logger, gh, r.flag.Schema)
	if err != nil {
		return "", reposetup.Entry{}, microerror.Mask(err)
	}
	// The entry of a repository that exists is validated in existing mode,
	// the schema alone; --added creates the repository, so the creation
	// rules apply. No name check either way: the repository exists, or the
	// creation PR checked the name.
	mode := reposetup.ModeExisting
	if r.flag.Added {
		mode = reposetup.ModeCreate
	}
	validator := reposetup.Validator{Schema: schema, Owner: r.flag.Owner}
	result, err := validator.Validate(ctx, reposetup.Request{TeamFile: teamFile, Names: []string{name}, Mode: mode})
	if err != nil {
		return "", reposetup.Entry{}, microerror.Mask(err)
	}
	return teamFile.Team, result.Entries[0], nil
}
