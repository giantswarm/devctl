package sweep

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
	payload, err := session.Call(callCtx, manager.ToolSweepInventory, nil)
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.JSON() {
		return microerror.Mask(client.PrintJSON(r.stdout, payload))
	}
	sweep, err := client.Decode[manager.Sweep](manager.ToolSweepInventory, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	print(r.stdout, sweep)
	return nil
}

func print(w io.Writer, s *manager.Sweep) {
	switch {
	case s.Started:
		fmt.Fprintf(w, "sweep started as %s (a member of %s); `devctl repo list` shows the new records when it is done\n", s.Login, strings.Join(s.Teams, ", "))
	case s.Running:
		fmt.Fprintf(w, "a sweep is already running; none started (asked as %s)\n", s.Login)
	default:
		fmt.Fprintf(w, "no sweep started (asked as %s)\n", s.Login)
	}
	PrintSummary(w, s.Last)
}

// PrintSummary writes the last sweep's summary, when there is one.
func PrintSummary(w io.Writer, last *manager.SweepSummary) {
	if last == nil {
		fmt.Fprintln(w, "last sweep: none yet")
		return
	}
	line := fmt.Sprintf("last sweep: finished %s in %s: %d repositories (%d declared, %d undeclared, %d gone, %d archived), %d engine checks",
		last.FinishedAt.UTC().Format("2006-01-02T15:04:05Z"), last.Duration, last.Repositories, last.Declared, last.Undeclared, last.Gone, last.Archived, last.EngineChecks)
	if len(last.Errors) > 0 {
		line += fmt.Sprintf(", %d errors", len(last.Errors))
	}
	fmt.Fprintln(w, line)
}
