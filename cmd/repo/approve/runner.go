package approve

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

// callTimeout bounds the call: an approval merges within it.
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
	number, err := pullRequest(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolApproveChange, client.WriteArgs(map[string]any{"pullRequest": number}, r.flag.DryRun))
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.JSON() {
		return microerror.Mask(client.PrintJSON(r.stdout, payload))
	}
	approval, err := client.Decode[manager.Approval](manager.ToolApproveChange, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	print(r.stdout, approval, r.flag.DryRun)
	return nil
}

func print(w io.Writer, a *manager.Approval, dryRun bool) {
	if dryRun {
		fmt.Fprintln(w, "dry run: nothing written")
	}
	member := "not a member"
	if a.Member {
		member = "a member"
	}
	line := fmt.Sprintf("pull request #%d of %s: you are %s", a.PullRequest, a.Team, member)
	if a.Login != "" {
		line += " (" + a.Login
		if len(a.Teams) > 0 {
			line += ", teams " + strings.Join(a.Teams, ", ")
		}
		line += ")"
	}
	if a.Author != "" {
		line += "; opened by " + a.Author
	}
	fmt.Fprintln(w, line)
	if a.Rerendered != nil {
		fmt.Fprintf(w, "re-rendered on %s first: %s in %s\n", a.Rerendered.Base, strings.Join(a.Rerendered.Entries, ", "), strings.Join(a.Rerendered.Files, ", "))
	}
	if a.RerenderError != "" {
		fmt.Fprintf(w, "the pull request conflicts with its base and could not be re-rendered: %s\n", a.RerenderError)
	}
	if a.ReviewURL != "" {
		fmt.Fprintf(w, "review: %s\n", a.ReviewURL)
	}
	if a.Message != "" {
		fmt.Fprintln(w, a.Message)
		return
	}
	switch {
	case a.Merged:
		fmt.Fprintln(w, "merged")
	case a.AutoMerge:
		fmt.Fprintln(w, "merges by itself once its checks pass (auto-merge)")
	}
}
