package file

import (
	"os"
	"path/filepath"
	"testing"
)

func Test_FindHelmCharts(t *testing.T) {
	testCases := []struct {
		name     string
		setup    func(dir string) error
		expected []string
	}{
		{
			name:     "case 1: no helm directory",
			setup:    func(dir string) error { return nil },
			expected: []string{},
		},
		{
			name: "case 2: helm dir exists but no charts",
			setup: func(dir string) error {
				return os.MkdirAll(filepath.Join(dir, "helm", "not-a-chart"), 0755)
			},
			expected: nil,
		},
		{
			name: "case 3: single chart with Chart.yaml",
			setup: func(dir string) error {
				chartDir := filepath.Join(dir, "helm", "my-app")
				if err := os.MkdirAll(chartDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: my-app\n"), 0644)
			},
			expected: []string{"my-app"},
		},
		{
			name: "case 4: multiple charts",
			setup: func(dir string) error {
				for _, name := range []string{"chart-a", "chart-b"} {
					chartDir := filepath.Join(dir, "helm", name)
					if err := os.MkdirAll(chartDir, 0755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: "+name+"\n"), 0644); err != nil {
						return err
					}
				}
				return nil
			},
			expected: []string{"chart-a", "chart-b"},
		},
		{
			name: "case 5: mix of charts and non-charts",
			setup: func(dir string) error {
				if err := os.MkdirAll(filepath.Join(dir, "helm", "not-a-chart"), 0755); err != nil {
					return err
				}
				chartDir := filepath.Join(dir, "helm", "real-chart")
				if err := os.MkdirAll(chartDir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: real-chart\n"), 0644)
			},
			expected: []string{"real-chart"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			if err := tc.setup(dir); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got, err := FindHelmCharts(dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tc.expected) {
				t.Fatalf("expected %v charts, got %v: %v", len(tc.expected), len(got), got)
			}
			for i, name := range tc.expected {
				if got[i] != name {
					t.Errorf("chart[%d]: expected %q, got %q", i, name, got[i])
				}
			}
		})
	}
}
