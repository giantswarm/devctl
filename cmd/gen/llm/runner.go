package llm

import (
	"context"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/llm"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

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
