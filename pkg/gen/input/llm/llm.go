package llm

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/params"
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
			Flavours: config.Flavours,
			Language: config.Language,
		},
	}

	return l, nil
}

// BlockBody returns the content of the devctl-managed block in AGENTS.md.
func (l *LLM) BlockBody() (string, error) {
	body, err := file.AgentsBlockBody(l.params)
	return body, microerror.Mask(err)
}
