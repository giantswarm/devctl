package mainpkg

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/params"
)

type Config struct {
	GoModule string
}

type Main struct {
	params params.Params
}

func New(config Config) (*Main, error) {
	if config.GoModule == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.GoModule must not be empty", config)
	}

	var subcommands []params.Subcommand

	err := os.MkdirAll(params.RootCmdDir, 0755)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	err = filepath.Walk(params.RootCmdDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		if path == params.RootCmdDir {
			return nil
		}
		if !info.IsDir() {
			return nil
		}

		segs := strings.Split(path, "/")[1:]

		parentAlias := "root"
		if len(segs) > 1 {
			parentAlias = strings.Join(segs[:len(segs)-1], "")
		}

		s := params.Subcommand{
			Alias:       strings.Join(segs, ""),
			Import:      filepath.Join(config.GoModule, path),
			ParentAlias: parentAlias,
		}

		subcommands = append(subcommands, s)

		return nil
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	c := &Main{
		params: params.Params{
			GoModule: config.GoModule,

			Subcommands: subcommands,
		},
	}

	return c, nil
}

func (m *Main) ZZMain() input.Input {
	return file.NewZZMainInput(m.params)
}
