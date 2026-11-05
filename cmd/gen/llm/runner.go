package llm

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return microerror.Mask(err)
	}

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var err error

	// A fork line carries its upstream's files: nothing is generated for it.
	if !r.flag.Flavours.Generates() {
		if r.flag.DryRun {
			_, err = fmt.Fprintln(r.stdout, "gen llm would do nothing: nothing is generated for a fork")
		}
		return microerror.Mask(err)
	}

	mode := llm.DefaultMode
	if r.flag.MigrateInstructions {
		mode = llm.MigrationMode
	}
	changes, report, err := llm.Plan(ctx, ".", mode)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(report.Findings) > 0 {
		// Alignment runs every generator and fails the repository on any
		// error, so findings are warnings and the command succeeds.
		out, heading := r.stderr, "gen llm skipped this repository:"
		if r.flag.DryRun {
			out, heading = r.stdout, "gen llm would skip this repository:"
		}
		var b strings.Builder
		fmt.Fprintln(&b, heading)
		for _, f := range report.Findings {
			fmt.Fprintf(&b, "%s: %s\n  fix: %s\n", f.Path, f.Problem, f.Fix)
		}
		_, err = io.WriteString(out, b.String())
		return microerror.Mask(err)
	}

	if r.flag.DryRun {
		var b strings.Builder
		fmt.Fprintln(&b, "gen llm would make these changes:")
		for _, step := range changes.Steps() {
			fmt.Fprintf(&b, "- %s\n", step)
		}
		_, err = io.WriteString(r.stdout, b.String())
		return microerror.Mask(err)
	}

	llmInput, err := llm.New(llm.Config{Flavours: r.flag.Flavours, Language: r.flag.Language})
	if err != nil {
		return microerror.Mask(err)
	}
	body, err := llmInput.BlockBody()
	if err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(changes.Apply(ctx, body))
}
