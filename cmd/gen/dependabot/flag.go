package dependabot

import (
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagEcosystems = "ecosystems"
	flagInterval   = "interval"
	flagReviewers  = "reviewers"
)

type flag struct {
	Interval   string
	Reviewers  []string
	Ecosystems []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Interval, flagInterval, "i", "weekly", "Check for daily, weekly or monthly updates (default: weekly).")
	cmd.Flags().StringSliceVarP(&f.Reviewers, flagReviewers, "r", []string{}, "Reviewers you want to assign automatically when Dependabot creates a PR, e.g. giantswarm/team-firecracker.")
	cmd.Flags().StringSliceVarP(&f.Ecosystems, flagEcosystems, "e", []string{}, "Ecosystem for each one package manager that you want GitHub Dependabot to monitor for new versions , e.g. go, docker. Setting this flag disables autodetection of files.")
}

func (f *flag) Validate() error {
	if !gen.IsValidSchedule(f.Interval) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagInterval, strings.Join(gen.AllowedSchedule(), "|"))
	}

	// in case ecosystem was not set specifically, we autodetect files
	if len(f.Ecosystems) == 0 {
		for _, e := range gen.AllowedEcosystems() {
			switch e {
			case "docker":
				if exists("Dockerfile") {
					f.Ecosystems = append(f.Ecosystems, "docker")
				}
			case "github-actions":
				if exists(".github/workflows/") {
					f.Ecosystems = append(f.Ecosystems, "github-actions")
				}
			case "go":
				if exists("go.mod") && exists("go.sum") {
					f.Ecosystems = append(f.Ecosystems, "go")
				}
			}
		}

	}

	if !gen.IsValidEcoSystem(f.Ecosystems) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagEcosystems, strings.Join(gen.AllowedEcosystems(), "|"))
	}

	return nil
}

func exists(file string) bool {
	if _, err := os.Stat(file); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}
