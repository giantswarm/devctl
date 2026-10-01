package info

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
	payload, err := session.Call(callCtx, manager.ToolGetInfo, nil)
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.JSON() {
		return microerror.Mask(client.PrintJSON(r.stdout, payload))
	}
	info, err := client.Decode[manager.Info](manager.ToolGetInfo, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	print(r.stdout, session.Endpoint, info)
	return nil
}

func print(w io.Writer, endpoint string, info *manager.Info) {
	fmt.Fprintf(w, "%s %s at %s (tools x_%s_*)\n", manager.Server, info.Version, endpoint, info.ToolPrefix)
	if c := info.Caller; c != nil {
		fmt.Fprintf(w, "caller: %s (id %d), %s", c.Login, c.ID, info.Auth.Mode)
		if info.Auth.AuthorizationServer != "" {
			fmt.Fprintf(w, " through %s", info.Auth.AuthorizationServer)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "caller: none (%s", info.Auth.Mode)
		if info.Auth.Reason != "" {
			fmt.Fprintf(w, ": %s", info.Auth.Reason)
		}
		fmt.Fprintln(w, ")")
	}
	tf := info.TeamFiles
	line := fmt.Sprintf("team files: %s@%s, readable %s", tf.Repository, tf.Ref, tf.Readable)
	if tf.Reason != "" {
		line += " (" + tf.Reason + ")"
	}
	fmt.Fprintln(w, line)
	inv := info.Inventory
	connected := "not connected"
	if inv.Connected {
		connected = fmt.Sprintf("%d records", inv.Records)
	}
	line = fmt.Sprintf("inventory: reads as %s, %s", inv.Identity, connected)
	if inv.Error != "" {
		line += ", error: " + inv.Error
	}
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, "circleci facts: %s\n", info.CircleCI.Source)
	reviews := "not configured"
	if info.Reviews.Configured {
		reviews = "configured"
		if info.Reviews.DebugChannel != "" {
			reviews += ", every ask and notice to #" + info.Reviews.DebugChannel
		}
	}
	fmt.Fprintf(w, "reviews (Slack asks): %s\n", reviews)
	fmt.Fprintf(w, "engine: %s %s (%s)\n", info.Engine.Module, info.Engine.Version, info.Engine.Package)
	caps := info.Capabilities
	modes := strings.Join(caps.Modes, ", ")
	if caps.ApplyRefused {
		modes += "; apply refused"
	}
	fmt.Fprintf(w, "writes: modes %s; tools %s\n", modes, strings.Join(caps.WriteTools, ", "))
}
