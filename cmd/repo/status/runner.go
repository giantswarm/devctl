package status

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/auth"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

const (
	sourceManager = "giantswarm-repo-manager"
	sourceEngine  = "engine"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	// manager overrides the inventory client; nil builds one from the flags.
	manager manager.RepositoryGetter
}

// output is the set-up state with where it came from.
type output struct {
	// Source is giantswarm-repo-manager or engine.
	Source string `json:"source"`
	// Endpoint is the muster endpoint the manager was reached through.
	Endpoint string `json:"endpoint,omitempty"`
	// Result is the engine's check result: the manager's stored one or the
	// one just run.
	Result *reconcile.Result `json:"result"`
	// Align is the repository's opt-in to alignment as its entry declares it
	// (align: true); nil when the source did not read the entry -- the
	// manager's record carries the set-up state alone.
	Align *bool `json:"align,omitempty"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx, args[0]))
}

func (r *runner) run(ctx context.Context, arg string) error {
	r.logger.SetOutput(r.stderr)

	owner, repo := r.flag.Owner, arg
	if i := strings.IndexByte(arg, '/'); i >= 0 {
		owner, repo = arg[:i], arg[i+1:]
	}
	if owner == "" || repo == "" {
		return microerror.Maskf(invalidFlagError, "expected [OWNER/]REPOSITORY, got %q", arg)
	}

	if out, ok := r.fromManager(ctx, owner+"/"+repo); ok {
		return microerror.Mask(r.print(out))
	}

	out, err := r.fromEngine(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(r.print(out))
}

// fromManager asks giantswarm-repo-manager when an endpoint is configured.
// Not configured, unreachable or without a usable answer: false, and the
// engine judges.
func (r *runner) fromManager(ctx context.Context, repository string) (*output, bool) {
	client := r.manager
	if client == nil {
		if r.flag.MusterEndpoint == "" {
			r.logger.Debug("no muster endpoint: judging with the engine's checks")
			return nil, false
		}
		client = &manager.Client{
			Endpoint: r.flag.MusterEndpoint,
			Token:    os.Getenv(r.flag.MusterTokenEnvVar),
			Version:  project.Version(),
		}
	}

	record, err := client.GetRepository(ctx, repository)
	switch {
	case err != nil:
		r.logger.Warnf("%s at %s did not answer (%v): judging with the engine's checks", sourceManager, r.flag.MusterEndpoint, err)
		return nil, false
	case record.Setup == nil:
		r.logger.Warnf("%s has no set-up state for %s: judging with the engine's checks", sourceManager, repository)
		return nil, false
	}

	return &output{Source: sourceManager, Endpoint: r.flag.MusterEndpoint, Result: record.Setup}, true
}

// fromEngine runs the engine's checks in read mode with the person's
// tokens: the declaration from the team files, validated, then every step.
func (r *runner) fromEngine(ctx context.Context, owner, repo string) (*output, error) {
	token, source, err := auth.GitHubToken(ctx, r.flag.GithubTokenEnvVar)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	r.logger.Debugf("GitHub token from %s", source)

	client, err := githubclient.New(githubclient.Config{Logger: r.logger, AccessToken: token})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	gh := client.GetUnderlyingClient(ctx)
	remote := reposetup.Remote{GitHub: gh}

	var teams []string
	if r.flag.Team != "" {
		teams = []string{r.flag.Team}
	}
	teamFile, err := remote.FindEntry(ctx, repo, teams)
	if err != nil {
		if reposetup.IsEntryNotFound(err) {
			return nil, microerror.Maskf(notDeclaredError, "%s/%s is not declared (%v): a repository without its declaration is the drift the reconciler reports; declare it with `devctl repo create`", owner, repo, err)
		}
		return nil, microerror.Mask(err)
	}

	schema, err := reposetup.FetchSchema(ctx, client)
	if err != nil {
		r.logger.Warnf("cannot read the repositories schema from %s (%v): validating against the embedded copy", remote.Slug(), err)
		schema, err = reposetup.EmbeddedSchema()
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	// Existing mode, and the name is not checked: the repository exists,
	// that is the point.
	validator := reposetup.Validator{Schema: schema, Owner: owner}
	result, err := validator.Validate(ctx, reposetup.Request{TeamFile: teamFile.TeamFile, Names: []string{repo}, Mode: reposetup.ModeExisting})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	entry := result.Entries[0]
	if !entry.Accepted {
		var problems []string
		for _, p := range entry.Problems {
			problems = append(problems, p.String())
		}
		return nil, microerror.Maskf(invalidDeclarationError, "the entry for %s in %s is refused by the engine, fix it before its set-up state can be judged: %s", repo, teamFile.Path, strings.Join(problems, "; "))
	}
	declared, _ := teamFile.Entry(repo)
	fields, err := declared.Fields()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	runner := reconcile.Runner{GitHub: gh, Checks: client}
	if circleToken := os.Getenv(r.flag.CircleCITokenEnvVar); circleToken != "" {
		runner.CircleCI, err = circleciclient.New(circleciclient.Config{Token: circleToken, Logger: r.logger})
		if err != nil {
			return nil, microerror.Mask(err)
		}
	} else {
		r.logger.Infof("no CircleCI token in $%s: the CircleCI and release steps are skipped", r.flag.CircleCITokenEnvVar)
	}

	res, err := runner.Run(ctx, reconcile.Request{Owner: owner, Team: teamFile.Team, Entry: entry, Mode: reconcile.ModeCheck})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return &output{Source: sourceEngine, Result: res, Align: &fields.Align}, nil
}

// alignLine names the repository's opt-in to alignment and what it means
// for the reconciler's runs.
func alignLine(optedIn bool) string {
	if optedIn {
		return "opted in to alignment (align: true): the reconciler changes this repository to its declared set-up on every trigger"
	}
	return "not opted in to alignment: the reconciler checks this repository and changes nothing; opt in with align: true in its entry"
}

func (r *runner) print(out *output) error {
	if r.flag.Output == outputJSON {
		enc := json.NewEncoder(r.stdout)
		enc.SetIndent("", "  ")
		return microerror.Mask(enc.Encode(out))
	}

	res := out.Result
	from := out.Source
	if out.Endpoint != "" {
		from += " at " + out.Endpoint
	}
	fmt.Fprintf(r.stdout, "%s declared in %s (%s mode, from %s)\n", res.Repository, res.Team, res.Mode, from)
	if res.Declared != res.Repository {
		fmt.Fprintf(r.stdout, "declared as %s: renamed on GitHub\n", res.Declared)
	}
	if out.Align != nil {
		fmt.Fprintln(r.stdout, alignLine(*out.Align))
	}
	for _, step := range res.Steps {
		fmt.Fprintf(r.stdout, "  %-12s %-9s %s\n", step.Step, step.Verdict, step.Summary)
		for _, c := range step.Changes {
			fmt.Fprintf(r.stdout, "  %-12s %-9s would: %s\n", "", "", c)
		}
		for _, f := range step.Findings {
			kind := string(f.Kind)
			if f.Advisory {
				kind += " (advisory)"
			}
			fmt.Fprintf(r.stdout, "  %-12s %-9s %s: %s -- fix: %s\n", "", "", kind, f.Message, f.Fix)
		}
	}
	if res.Converged {
		fmt.Fprintln(r.stdout, "converged: set up as declared")
	} else {
		fmt.Fprintln(r.stdout, "not converged: drift, failed steps or findings to fix above")
	}
	return nil
}
