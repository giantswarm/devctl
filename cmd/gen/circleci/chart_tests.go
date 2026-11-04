package circleci

import (
	"os"

	"github.com/giantswarm/microerror"
	"sigs.k8s.io/yaml"
)

// generatorCustomConfigPath is generator input, separate from custom.yml,
// which CircleCI merges into the pipeline at runtime.
const generatorCustomConfigPath = ".circleci/config.custom.yml"

func readChartTestRequires() ([]string, error) {
	data, err := os.ReadFile(generatorCustomConfigPath)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, microerror.Mask(err)
	}

	var custom struct {
		ChartTests struct {
			Requires []string `json:"requires"`
		} `json:"chart_tests"`
	}
	if err := yaml.UnmarshalStrict(data, &custom); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "parse %s: %v", generatorCustomConfigPath, err)
	}

	return custom.ChartTests.Requires, nil
}
