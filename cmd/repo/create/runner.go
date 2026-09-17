package create

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/auth"
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

// output is what the command prints with --output json: the dry run, the
// creation (its plan under --dry-run) and, once opened, the pull request.
type output struct {
	DryRun      *reposetup.Result       `json:"dryRun"`
	Create      *reconcile.CreateResult `json:"create,omitempty"`
	PullRequest string                  `json:"pullRequest,omitempty"`
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx))
}

// run is the command, pull request last: the dry run, the repository
// created as the person, the scaffold pushed, then the declaration's pull
// request for a repository that exists and is the author's.
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
	gh := client.GetUnderlyingClient(ctx)
	remote := reposetup.Remote{GitHub: gh}

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
	validate := func(mode reposetup.Mode) (*reposetup.Result, error) {
		result, err := validator.Validate(ctx, reposetup.Request{
			TeamFile:    changed,
			Names:       []string{r.flag.Name},
			Mode:        mode,
			Author:      person.Login,
			AuthorTeams: person.Teams,
		})
		return result, microerror.Mask(err)
	}

	// The dry run: the schema, the creation rules, the name free on GitHub.
	dryRun, err := validate(reposetup.ModeCreate)
	if err != nil {
		return microerror.Mask(err)
	}
	out := output{DryRun: dryRun}
	if r.flag.Output == outputText {
		r.printDryRun(teamFile, dryRun)
	}

	entry := dryRun.Entries[0]
	if !dryRun.Accepted {
		// A taken name that is the caller's own repository is a creation
		// to resume, validated for a repository that exists.
		resume, err := r.resumable(ctx, gh, entry)
		if err != nil {
			return microerror.Mask(err)
		}
		if !resume {
			r.print(out)
			return microerror.Maskf(refusedError, "%s is refused; the problems name the fields, nothing was created and no pull request was opened", r.flag.Name)
		}
		r.logger.Infof("%s/%s exists and you administer it: resuming its creation", r.flag.Owner, r.flag.Name)
		existing, err := validate(reposetup.ModeExisting)
		if err != nil {
			return microerror.Mask(err)
		}
		out.DryRun = existing
		if r.flag.Output == outputText {
			fmt.Fprintf(r.stdout, "resuming:   %s/%s exists and you administer it; the declaration validated for an existing repository is %s\n\n", r.flag.Owner, r.flag.Name, verdictWord(existing.Accepted))
		}
		if !existing.Accepted {
			r.print(out)
			return microerror.Maskf(refusedError, "%s is refused; the problems name the fields, nothing was created and no pull request was opened", r.flag.Name)
		}
		entry = existing.Entries[0]
	}

	// The repository and its scaffold, as the person, through the engine's
	// own steps; the caller's role is read before anything is written.
	mode := reconcile.ModeRepair
	if r.flag.DryRun {
		mode = reconcile.ModeCheck
	}
	runner := reconcile.Runner{
		GitHub: gh,
		// The token downloads the templates: giantswarm/template is private.
		Renderer: reposetup.Renderer{
			Templates: reposetup.GitHubTemplates{Token: token},
			Log:       engine.LogWriter(r.logger),
		},
		Log: engine.LogWriter(r.logger),
	}
	created, err := runner.Create(ctx, reconcile.CreateRequest{Owner: r.flag.Owner, Team: teamFile.Team, Entry: entry, Mode: mode})
	if err != nil {
		r.print(out)
		return microerror.Mask(err)
	}
	out.Create = created
	if r.flag.Output == outputText {
		r.printCreate(created)
	}
	if failed := created.Failed(); len(failed) > 0 {
		r.print(out)
		return microerror.Maskf(stepFailedError, "%s: %s -- fix the cause and rerun; the command resumes where it stopped", failed[0].Step, failed[0].Summary)
	}
	if r.flag.DryRun {
		if r.flag.Output == outputText {
			fmt.Fprintln(r.stdout, "dry run: nothing created, no pull request opened")
		}
		r.print(out)
		return nil
	}

	// The declaration last, validated for the repository that exists now.
	// The notices are the dry run's: the guard is about the author.
	existing, err := validate(reposetup.ModeExisting)
	if err != nil {
		return microerror.Mask(err)
	}
	if !existing.Accepted {
		r.print(out)
		return microerror.Maskf(refusedError, "the declaration of %s is refused; the problems name the fields, no pull request was opened", created.URL)
	}
	existing.Notices = dryRun.Notices
	spec := reposetup.CreationPullRequest(teamFile, content, existing, reposetup.CreatedRepository{URL: created.URL, ScaffoldCommit: created.ScaffoldCommit})
	pr, err := remote.OpenPullRequest(ctx, spec)
	state := "opened"
	if reposetup.IsBranchExists(err) {
		// A rerun after the pull request was opened: report the open one.
		pr, err = remote.FindPullRequest(ctx, spec.Branch)
		if err == nil && pr == nil {
			err = microerror.Maskf(branchWithoutPullRequestError, "branch %s exists in %s without an open pull request: open the pull request from it or delete the branch, then rerun", spec.Branch, remote.Slug())
		}
		state = "open already"
	}
	if err != nil {
		r.print(out)
		return microerror.Mask(err)
	}
	out.PullRequest = pr.GetHTMLURL()
	if r.flag.Output == outputText {
		fmt.Fprintf(r.stdout, "pull request: %s (%s)\n", out.PullRequest, state)
	}
	r.print(out)

	return nil
}

// resumable says whether a refused dry run is a creation of the caller's
// own to resume: the one refusal is the taken name, and the repository under
// it is theirs — the declared name itself, no redirect, and they administer
// it, as the creator of an organization repository does. Anyone else's
// repository stays a refusal.
func (r *runner) resumable(ctx context.Context, gh *github.Client, entry reposetup.Entry) (bool, error) {
	if !entry.RefusedForTakenName() {
		return false, nil
	}
	repo, resp, err := gh.Repositories.Get(ctx, r.flag.Owner, r.flag.Name)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, microerror.Mask(err)
	}
	if !strings.EqualFold(repo.GetFullName(), r.flag.Owner+"/"+r.flag.Name) {
		return false, nil
	}
	return repo.GetPermissions().GetAdmin(), nil
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

// printCreate writes the creation as text: one line per step — what was
// created or found, the scaffold commit, the plan of a dry run, a failure —
// then the scaffold's findings for a person.
func (r *runner) printCreate(res *reconcile.CreateResult) {
	for _, s := range res.Steps {
		var detail string
		switch {
		case s.Verdict == reconcile.VerdictFailed:
			detail = "failed -- " + s.Summary
		case s.Verdict == reconcile.VerdictDrift:
			detail = "dry run: would " + strings.Join(s.Changes, "; ")
		case s.Verdict == reconcile.VerdictSkipped:
			detail = "skipped -- " + s.Summary
		case s.Step == reconcile.StepCreate && res.Created:
			detail = "created " + res.URL
		case s.Step == reconcile.StepCreate:
			detail = "exists " + res.URL + " (resumed)"
		case s.Step == reconcile.StepScaffold && s.Verdict == reconcile.VerdictRepaired:
			detail = "pushed " + res.ScaffoldCommit
		case s.Step == reconcile.StepScaffold:
			detail = s.Summary + " " + res.ScaffoldCommit
		default:
			detail = string(s.Verdict) + " " + s.Summary
		}
		fmt.Fprintf(r.stdout, "%-12s%s\n", string(s.Step)+":", strings.TrimSpace(detail))
		for _, f := range s.Findings {
			fmt.Fprintf(r.stdout, "  [%s] %s\n  fix: %s\n", f.Kind, f.Message, f.Fix)
		}
	}
	fmt.Fprintln(r.stdout)
}

func verdictWord(accepted bool) string {
	if accepted {
		return "accepted"
	}
	return "refused"
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
