// Package client is what the repo commands that call giantswarm-repo-manager
// share: the muster token from the keychain, the manager client over the
// person's muster endpoint, the output flag, and the text renderers of the
// answers the verbs have in common. Each verb is one tool of the manager;
// `-o json` prints the tool's answer as it came.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/project"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// The output formats.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// Flags are the flags every manager-backed verb takes.
type Flags struct {
	Output string
}

// Init registers -o/--output.
func (f *Flags) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Output, "output", "o", OutputText, "Output format: text, or json for the manager's answer as it came.")
}

// Validate checks the output format.
func (f *Flags) Validate() error {
	if f.Output != OutputText && f.Output != OutputJSON {
		return microerror.Maskf(InvalidFlagError, "--output must be %s or %s", OutputText, OutputJSON)
	}
	return nil
}

// JSON says whether the answer is printed as it came.
func (f *Flags) JSON() bool {
	return f.Output == OutputJSON
}

// Session is the manager reached as the person: the muster token of the
// keychain on every call.
type Session struct {
	Caller   manager.Caller
	Endpoint string
	// Login is the account the muster token acts as, as the sign-in read it.
	Login string
}

// Opener opens a Session; the commands use Open, tests a fake.
type Opener func(ctx context.Context) (*Session, error)

// Open reads the muster token from the keychain, refreshed when it expired,
// and returns the session; without a usable token it is the auth store's
// error naming `devctl auth login --muster-only`.
func Open(ctx context.Context) (*Session, error) {
	token, err := authstore.RequireMuster(ctx)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return &Session{
		Caller:   &manager.Client{Endpoint: token.Endpoint, Token: token.Value, Version: project.Version()},
		Endpoint: token.Endpoint,
		Login:    token.Login,
	}, nil
}

// Call calls one tool of the manager and returns its answer; a bearer
// muster refuses is the auth store's error, so the person is sent to the
// login.
func (s *Session) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	payload, err := s.Caller.Call(ctx, tool, args)
	if manager.IsAuthRequired(err) {
		return nil, &authstore.AuthRequiredError{Identity: "muster", Cause: "muster at " + s.Endpoint + " refused the token", Hint: "devctl auth login --muster-only"}
	}
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return payload, nil
}

// Decode reads a tool's answer into the manager's type.
func Decode[T any](tool string, payload json.RawMessage) (*T, error) {
	var v T
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, microerror.Maskf(UnexpectedAnswerError, "%s: the answer is not what devctl expects: %v", tool, err)
	}
	return &v, nil
}

// WriteArgs completes a write's arguments: dryRun: true, or mode: commit.
func WriteArgs(args map[string]any, dryRun bool) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	if dryRun {
		args["dryRun"] = true
	} else {
		args["mode"] = "commit"
	}
	return args
}

// PrintJSON writes the answer as it came, indented.
func PrintJSON(w io.Writer, payload json.RawMessage) error {
	var buf strings.Builder
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		// Not JSON: a text answer, printed as such.
		_, err := fmt.Fprintln(w, string(payload))
		return err
	}
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return microerror.Mask(err)
	}
	_, err := io.WriteString(w, buf.String())
	return err
}

// Repository checks the [OWNER/]NAME argument; the manager takes the name
// with or without the organisation.
func Repository(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.HasSuffix(arg, "/") || strings.HasPrefix(arg, "/") {
		return "", microerror.Maskf(InvalidFlagError, "expected [OWNER/]REPOSITORY, got %q", arg)
	}
	return arg, nil
}

// Indent prefixes every line of s.
func Indent(s, prefix string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// PrintSteps writes the engine's steps the way `repo status` does: step,
// verdict, summary, then the changes a repair would make and the findings
// with their fix.
func PrintSteps(w io.Writer, res *reconcile.Result) {
	for _, step := range res.Steps {
		fmt.Fprintf(w, "  %-12s %-9s %s\n", step.Step, step.Verdict, step.Summary)
		for _, c := range step.Changes {
			fmt.Fprintf(w, "  %-12s %-9s would: %s\n", "", "", c)
		}
		for _, f := range step.Findings {
			kind := string(f.Kind)
			if f.Advisory {
				kind += " (advisory)"
			}
			fmt.Fprintf(w, "  %-12s %-9s %s: %s -- fix: %s\n", "", "", kind, f.Message, f.Fix)
		}
	}
}

// Verdict is the one line that closes a set-up state.
func Verdict(res *reconcile.Result) string {
	switch {
	case res.Refused():
		return "not converged: the entry is refused and nothing was checked; fix the declaration as the findings above say"
	case res.Converged:
		return "converged: set up as declared"
	default:
		return "not converged: drift, failed steps or findings to fix above"
	}
}

// PrintPlan writes a write's dry run.
func PrintPlan(w io.Writer, plan *manager.Plan) {
	fmt.Fprintln(w, "dry run: nothing written")
	team := plan.Team
	if plan.FromTeam != "" {
		team = plan.FromTeam + " -> " + plan.Team
	}
	verdict := "accepted"
	if !plan.Accepted {
		verdict = "refused"
	}
	fmt.Fprintf(w, "%s in %s: %s\n", plan.Repository, team, verdict)
	for _, p := range plan.Problems {
		fmt.Fprintf(w, "  %s\n", p.String())
	}
	if plan.Before != "" {
		fmt.Fprintf(w, "entry before:\n%s\n", Indent(plan.Before, "  "))
	}
	if plan.Entry != "" {
		fmt.Fprintf(w, "entry after:\n%s\n", Indent(plan.Entry, "  "))
	}
	pr := plan.PullRequest
	fmt.Fprintf(w, "pull request: %s (branch %s, %s) as %s\n", pr.Title, pr.Branch, strings.Join(pr.Files, ", "), pr.As)
	printPlannedMessage(w, "ask", plan.Ask)
	printPlannedMessage(w, "notice", plan.Notice)
}

func printPlannedMessage(w io.Writer, what string, m *manager.PlannedMessage) {
	if m == nil {
		return
	}
	where := m.Team + channelText(m.ChannelName, m.Channel)
	if m.Deliverable {
		fmt.Fprintf(w, "%s: to %s\n", what, where)
		return
	}
	fmt.Fprintf(w, "%s: to %s, not deliverable: %s\n", what, where, m.Reason)
}

// channelText names a Slack channel as " in #<name> (<ID>)", or
// " in #<ID>" when the manager's answer carries no name.
func channelText(name, id string) string {
	switch {
	case id == "":
		return ""
	case name == "":
		return " in #" + id
	default:
		return " in #" + name + " (" + id + ")"
	}
}

// PrintCommitted writes a write's outcome in mode commit.
func PrintCommitted(w io.Writer, c *manager.Committed) {
	if pr := c.PullRequest; pr != nil {
		state := "opened"
		if pr.Existing {
			state = "already open"
		}
		if pr.AutoMerge {
			state += ", auto-merge armed"
		}
		fmt.Fprintf(w, "pull request: %s (%s; %s)\n", pr.URL, pr.Title, state)
	}
	printDelivery(w, "ask", c.Ask)
	printDelivery(w, "notice", c.Notice)
	PrintPendingRun(w, c.PendingRun)
}

func printDelivery(w io.Writer, what string, d *manager.Delivery) {
	if d == nil {
		return
	}
	where := d.Team
	if d.IntendedChannel != "" {
		where += channelText(d.ChannelName, d.IntendedChannel) + ", redirected to " + d.Channel
	} else {
		where += channelText(d.ChannelName, d.Channel)
	}
	if d.Delivered {
		fmt.Fprintf(w, "%s: delivered to %s\n", what, where)
		return
	}
	fmt.Fprintf(w, "%s: not delivered to %s: %s\n", what, where, d.Error)
}

// PrintPendingRun writes the reconciler run a record expects.
func PrintPendingRun(w io.Writer, p *manager.PendingRun) {
	if p == nil {
		return
	}
	line := fmt.Sprintf("pending run: expected since %s by %s", p.DispatchedAt.UTC().Format("2006-01-02T15:04:05Z"), p.By)
	if p.Kind != "" {
		line += " (" + p.Kind
		if p.PullRequest != nil {
			line += fmt.Sprintf(", %s", p.PullRequest.URL)
		}
		line += ")"
	}
	if p.MergedAt != nil {
		line += ", merged " + p.MergedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if p.ConflictsSince != nil {
		line += ", the pull request conflicts with its base since " + p.ConflictsSince.UTC().Format("2006-01-02T15:04:05Z")
	}
	fmt.Fprintln(w, line)
}

// PrintFindings writes the record's findings with their fix and source.
func PrintFindings(w io.Writer, findings []manager.Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintln(w, "findings:")
	for _, f := range findings {
		line := fmt.Sprintf("  %s: %s", f.Kind, f.Message)
		if f.Fix != "" {
			line += " -- fix: " + f.Fix
		}
		if f.Source != "" {
			line += " (" + f.Source + ")"
		}
		fmt.Fprintln(w, line)
	}
}
