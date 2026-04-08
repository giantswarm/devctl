package file

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// FindHelmCharts looks for subdirectories in helm/ that contain Chart.yaml
func FindHelmCharts(dir string) ([]string, error) {
	helmDir := filepath.Join(dir, "helm")

	entries, err := os.ReadDir(helmDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, microerror.Mask(err)
	}

	var charts []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if Chart.yaml exists in this subdirectory
		chartYamlPath := filepath.Join(helmDir, entry.Name(), "Chart.yaml")
		if _, err := os.Stat(chartYamlPath); err == nil {
			charts = append(charts, entry.Name())
		}
	}

	return charts, nil
}
