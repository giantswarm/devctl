package align

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// callTimeout bounds the call: a commit opens a pull request within it.
const callTimeout = 2 * time.Minute

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	open   client.Opener
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(r.run(context.Background(), args[0]))
}

func (r *runner) run(ctx context.Context, arg string) error {
	repository, err := client.Repository(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	args := map[string]any{"repository": repository}
	if r.flag.Team != "" {
		args["team"] = r.flag.Team
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolAlignRepository, client.WriteArgs(args, r.flag.DryRun))
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.JSON() {
		return microerror.Mask(client.PrintJSON(r.stdout, payload))
	}
	dispatch, err := client.Decode[manager.Dispatch](manager.ToolAlignRepository, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	print(r.stdout, repository, dispatch, r.flag.DryRun)
	return nil
}

func print(w io.Writer, repository string, d *manager.Dispatch, dryRun bool) {
	if dryRun {
		fmt.Fprintln(w, "dry run: nothing dispatched, nothing opened")
	}
	declared := "not declared"
	if d.Declared {
		declared = "declared"
		if d.Team != "" {
			declared += " in " + d.Team
		}
		if d.OptedIn {
			declared += ", opted in to alignment"
		} else {
			declared += ", not opted in"
		}
	} else if d.Team != "" {
		declared += "; checked from " + d.Team
	}
	fmt.Fprintf(w, "%s: mode %s (%s)\n", repository, d.Mode, declared)
	if d.Warning != "" {
		fmt.Fprintln(w, d.Warning)
	}
	for _, f := range d.Findings {
		fmt.Fprintf(w, "  %s: %s -- fix: %s\n", f.Kind, f.Message, f.Fix)
	}
	if len(d.Planned) > 0 {
		checked := ""
		if d.CheckedAt != "" {
			checked = " (checked " + d.CheckedAt + ")"
		}
		fmt.Fprintf(w, "planned changes%s:\n", checked)
		for _, step := range d.Planned {
			fmt.Fprintf(w, "  %-12s %s\n", step.Step, strings.Join(step.Changes, "; "))
		}
	} else if d.Mode != "opt-in" && d.Declared {
		fmt.Fprintln(w, "planned changes: none from the last check")
	}
	if d.OptIn != nil {
		fmt.Fprintln(w, "opt-in pull request:")
		client.PrintPlan(w, &d.OptIn.Plan)
		if d.OptIn.Committed != nil {
			client.PrintCommitted(w, d.OptIn.Committed)
		}
	}
	switch {
	case d.Dispatched:
		fmt.Fprintf(w, "dispatched %s as %s: %s\n", d.Workflow, d.As, d.RunsURL)
	case !dryRun && d.Mode != "opt-in":
		fmt.Fprintln(w, "nothing dispatched")
	}
	client.PrintPendingRun(w, d.PendingRun)
	if d.Then != "" {
		fmt.Fprintf(w, "then: %s\n", d.Then)
	}
}
