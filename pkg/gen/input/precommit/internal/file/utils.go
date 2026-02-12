package file

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// findHelmCharts looks for subdirectories in helm/ that contain Chart.yaml
func findHelmCharts(dir string) ([]string, error) {
	helmDir := filepath.Join(dir, "helm")

	// Check if helm directory exists
	if _, err := os.Stat(helmDir); os.IsNotExist(err) {
		// No helm directory, return empty list
		return []string{}, nil
	} else if err != nil {
		return nil, microerror.Mask(err)
	}

	var charts []string

	// Read all subdirectories in helm/
	entries, err := os.ReadDir(helmDir)
	if err != nil {
		return nil, microerror.Mask(err)
	}

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
