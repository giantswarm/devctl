package status

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// callTimeout bounds the one call to the manager.
const callTimeout = 60 * time.Second

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	// open is client.Open; tests inject a session over a fake.
	open client.Opener
}

// output is the set-up state as the text output prints it, read from the
// manager's record.
type output struct {
	Endpoint string
	Result   *reconcile.Result
	// Align is the repository's opt-in to alignment as its entry declares
	// it; nil when the record carries no entry.
	Align *bool
	// DefaultBranch is the repository's own on GitHub when the entry keeps
	// it, otherwise the entry's default branch or the baseline's;
	// Flavours the entry's gen.flavours.
	DefaultBranch string
	Flavours      []string
	CheckedAt     time.Time
	LastRun       *manager.LastRun
	PendingRun    *manager.PendingRun
	MissingRun    *manager.MissingRun
	// Findings are the inventory's own findings, the ones the steps do not
	// carry.
	Findings []manager.Finding
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx, args[0]))
}

func (r *runner) run(ctx context.Context, arg string) error {
	r.logger.SetOutput(r.stderr)

	repository, err := client.Repository(arg)
	if err != nil {
		return microerror.Mask(err)
	}

	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolGetRepository, map[string]any{"repository": repository})
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.JSON() {
		return microerror.Mask(client.PrintJSON(r.stdout, payload))
	}

	record, err := client.Decode[manager.Record](manager.ToolGetRepository, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	out, err := fromRecord(record, session.Endpoint)
	if err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(r.print(out))
}

// fromRecord reads the set-up state out of the manager's record: the
// engine's checks, the declaration's alignment lines from the entry, the
// runs and the inventory's own findings.
func fromRecord(record *manager.Record, endpoint string) (*output, error) {
	if record.Declaration == nil {
		return nil, microerror.Maskf(notDeclaredError, "%s is not declared in any team file: a repository without its declaration is the drift the reconciler reports; declare it with `devctl repo adopt`, or create one with `devctl repo create`", record.Repository)
	}
	if record.Setup.Checks == nil {
		reason := record.Setup.CheckError
		if reason == "" {
			reason = "the inventory has not checked it yet; `devctl repo refresh` builds the record now"
		}
		return nil, microerror.Maskf(noSetupStateError, "%s has no set-up state in the inventory: %s", record.Repository, reason)
	}

	out := &output{
		Endpoint:   endpoint,
		Result:     record.Setup.Checks,
		CheckedAt:  record.Setup.CheckedAt,
		LastRun:    record.Setup.LastRun,
		PendingRun: record.Setup.PendingRun,
		MissingRun: record.Setup.MissingRun,
	}
	for _, f := range record.Findings {
		if f.Source != "engine" {
			out.Findings = append(out.Findings, f)
		}
	}
	if fields, ok := entryFields(record.Declaration.Entry); ok {
		align := fields.Align
		out.Align = &align
		out.DefaultBranch = fields.DefaultBranch
		if fields.KeepsDefaultBranch() && record.Reality != nil && record.Reality.DefaultBranch != "" {
			out.DefaultBranch = record.Reality.DefaultBranch
		}
		if out.DefaultBranch == "" {
			out.DefaultBranch = reconcile.DefaultBaseline().DefaultBranch
		}
		if fields.Gen != nil {
			out.Flavours = fields.Gen.Flavours
		}
	}
	return out, nil
}

// entryFields parses the entry as the team file carries it, one YAML list
// item; false when the record carries none or it does not parse.
func entryFields(entry string) (reposetup.Fields, bool) {
	if strings.TrimSpace(entry) == "" {
		return reposetup.Fields{}, false
	}
	var items []reposetup.Fields
	if err := yaml.Unmarshal([]byte(entry), &items); err != nil || len(items) != 1 {
		var one reposetup.Fields
		if err := yaml.Unmarshal([]byte(entry), &one); err != nil || one.Name == "" {
			return reposetup.Fields{}, false
		}
		return one, true
	}
	return items[0], true
}

// declarationLine names the declared default branch and flavours the
// reconciler follows.
func declarationLine(branch string, flavours []string) string {
	profile := "none"
	if len(flavours) > 0 {
		profile = strings.Join(flavours, ", ")
	}
	return fmt.Sprintf("default branch %s, flavours %s", branch, profile)
}

// alignLine names the repository's opt-in to alignment and what it means
// for the reconciler's runs.
func alignLine(optedIn bool) string {
	if optedIn {
		return "opted in to alignment (align: true): the reconciler changes this repository to its declared set-up on every trigger"
	}
	return "not opted in to alignment: the reconciler checks this repository and changes nothing; opt in with align: true in its entry"
}

const timeFormat = "2006-01-02T15:04:05Z"

func (r *runner) print(out *output) error {
	// The shared renderers do not return write errors. Render into a builder
	// first, then return the error from writing the complete text to stdout.
	w := &strings.Builder{}
	res := out.Result
	from := manager.Server
	if out.Endpoint != "" {
		from += " at " + out.Endpoint
	}
	checked := ""
	if !out.CheckedAt.IsZero() {
		checked = ", checked " + out.CheckedAt.UTC().Format(timeFormat)
	}
	fmt.Fprintf(w, "%s declared in %s (%s mode, from %s%s)\n", res.Repository, res.Team, res.Mode, from, checked)
	if res.Declared != res.Repository {
		fmt.Fprintf(w, "declared as %s: renamed on GitHub\n", res.Declared)
	}
	if out.Align != nil {
		fmt.Fprintln(w, alignLine(*out.Align))
	}
	if out.DefaultBranch != "" {
		fmt.Fprintln(w, declarationLine(out.DefaultBranch, out.Flavours))
	}
	client.PrintSteps(w, res)
	fmt.Fprintln(w, client.Verdict(res))
	if run := out.LastRun; run != nil {
		line := fmt.Sprintf("last run: %s at %s", run.RunURL, run.Timestamp.UTC().Format(timeFormat))
		if c := run.Change; c != nil {
			line += " (" + c.Kind
			if c.By != "" {
				line += " by " + c.By
			}
			if c.PullRequest != nil {
				line += ", " + c.PullRequest.URL
			}
			line += ")"
		}
		fmt.Fprintln(w, line)
	}
	client.PrintPendingRun(w, out.PendingRun)
	if m := out.MissingRun; m != nil {
		where := m.RunsURL
		if m.RunURL != "" {
			where = m.RunURL + " (" + m.Conclusion + ")"
		}
		fmt.Fprintf(w, "missing run: the run expected since %s by %s never reported: %s\n", m.DispatchedAt.UTC().Format(timeFormat), m.By, where)
	}
	client.PrintFindings(w, out.Findings)
	_, err := io.WriteString(r.stdout, w.String())
	return err
}
