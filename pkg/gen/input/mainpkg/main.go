package mainpkg

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/params"
	"github.com/giantswarm/microerror"
)

type Config struct {
	Name string
}

type Main struct {
	params params.Params
}

func New(config Config) (*Main, error) {
	if config.Name == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Name must not be empty", config)
	}

	//type Subcommand struct {
	//	Alias       string
	//	Import      string
	//	ParentAlias string
	//}

	var subcommands []params.Subcommand

	// TODO unhappy with "cmd" not being const.
	err := filepath.Walk("cmd", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		if path == "cmd" {
			return nil
		}
		if !info.IsDir() {
			return nil
		}

		// TODO(PK): I need to think about something smarter here. It would be good to be able to generate that outside giantswarm.
		module := filepath.Join("github.com", "giantswarm", config.Name)

		segs := strings.Split(path, "/")[1:]

		parentAlias := "root"
		if len(segs) > 1 {
			parentAlias = strings.Join(segs[:len(segs)-1], "")
		}

		s := params.Subcommand{
			Alias:       strings.Join(segs, ""),
			Import:      filepath.Join(module, path),
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
			Name: config.Name,

			Subcommands: subcommands,
		},
	}

	return c, nil
}

func (m *Main) ZZMain() input.Input {
	return file.NewZZMainInput(m.params)
}
