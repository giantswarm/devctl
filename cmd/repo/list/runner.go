package list

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/sweep"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

const callTimeout = 60 * time.Second

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
	return microerror.Mask(r.run(context.Background()))
}

func (r *runner) run(ctx context.Context) error {
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolListRepositories, r.flag.args())
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.JSON() {
		return microerror.Mask(client.PrintJSON(r.stdout, payload))
	}
	listing, err := client.Decode[manager.Listing](manager.ToolListRepositories, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	print(r.stdout, listing)
	return nil
}

func print(w io.Writer, l *manager.Listing) {
	head := fmt.Sprintf("scope %s", l.Scope)
	if len(l.Teams) > 0 {
		head += fmt.Sprintf(" (teams %s, from %s)", strings.Join(l.Teams, ", "), l.TeamsSource)
	}
	head += fmt.Sprintf(": %d shown of %d matched, %d in the inventory", l.Shown, l.Matched, l.Total)
	if l.SweepRunning {
		head += "; a sweep is running"
	}
	fmt.Fprintln(w, head)
	if l.Note != "" {
		fmt.Fprintf(w, "note: %s\n", l.Note)
	}
	sweep.PrintSummary(w, l.Sweep)
	if len(l.Repositories) == 0 {
		fmt.Fprintln(w, "no repositories match")
		return
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REPOSITORY\tTEAM\tLIFECYCLE\tRENOVATE\tSET-UP\tLAST PERSON COMMIT\tFINDINGS")
	for _, row := range l.Repositories {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Repository, dash(row.Team), lifecycle(row), dash(row.Renovate), setup(row), dash(row.LastPersonCommit), dash(strings.Join(row.Findings, ",")))
	}
	_ = tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// lifecycle is the row's lifecycle, with what GitHub says of it.
func lifecycle(row manager.Row) string {
	l := row.Lifecycle
	if l == "" {
		l = "active"
	}
	switch {
	case row.Gone:
		return l + " (gone)"
	case row.Archived && l != "archived" && l != "deleted":
		return l + " (archived on GitHub)"
	}
	return l
}

// setup is the row's set-up state in one word: the marks the page shows.
func setup(row manager.Row) string {
	s := row.Setup
	switch {
	case row.Team == "":
		return "undeclared"
	case s.Refused:
		return "refused"
	case s.PendingRun != nil:
		return "run pending"
	case s.Error != "":
		return "unchecked"
	case s.Converged == nil:
		return "unchecked"
	case *s.Converged:
		return "in sync"
	default:
		return "not in sync"
	}
}
