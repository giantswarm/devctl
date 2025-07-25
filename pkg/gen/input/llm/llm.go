package llm

import (
	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/llm/internal/file"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/llm/internal/params"
)

type Config struct {
	Flavours gen.FlavourSlice
	Language string
}

type LLM struct {
	params params.Params
}

func New(config Config) (*LLM, error) {
	l := &LLM{
		params: params.Params{
			Dir: ".cursor/rules",

			Flavours: config.Flavours,
			Language: config.Language,
		},
	}

	return l, nil
}

func (l *LLM) BaseLLMRules() input.Input {
	return file.NewBaseLLMRulesInput(l.params)
}

func (l *LLM) GoLLMRules() input.Input {
	return file.NewGoLLMRulesInput(l.params)
}
