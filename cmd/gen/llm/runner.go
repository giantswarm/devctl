package llm

import (
	"context"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
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

	if r.flag.DryRun {
		return microerror.Mask(r.dryRun(ctx))
	}

	var llmInput *llm.LLM
	{
		c := llm.Config{
			Flavours: r.flag.Flavours,
			Language: r.flag.Language,
		}

		llmInput, err = llm.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	inputs := []input.Input{
		llmInput.BaseLLMRules(),
	}

	// Add additional rules files for different flavours and languages
	if r.flag.Language == "go" {
		inputs = append(inputs, llmInput.GoLLMRules())
	}

	err = gen.Execute(
		ctx,
		inputs...,
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) dryRun(ctx context.Context) error {
	mode := llm.DefaultMode
	if r.flag.MigrateInstructions {
		mode = llm.MigrationMode
	}
	report, err := llm.Preflight(ctx, ".", mode)
	if err != nil {
		return microerror.Mask(err)
	}

	if len(report.Findings) == 0 {
		_, err = fmt.Fprintln(r.stdout, "gen llm would proceed: no findings")
		return microerror.Mask(err)
	}
	if _, err := fmt.Fprintln(r.stdout, "gen llm would skip this repository:"); err != nil {
		return microerror.Mask(err)
	}
	for _, f := range report.Findings {
		if _, err := fmt.Fprintf(r.stdout, "%s: %s\n  fix: %s\n", f.Path, f.Problem, f.Fix); err != nil {
			return microerror.Mask(err)
		}
	}
	return nil
}
