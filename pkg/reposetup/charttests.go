package reposetup

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"
)

// chartTestFiles are the chart tests a scaffold carries: what app-test-suite
// (ATS) runs in the generated execute-chart-tests job of every branch build
// of a chart repository. ATS picks the pytest executor from
// tests/ats/pyproject.toml, which `devctl gen circleci` writes for every
// chart repository on generated CI, and refuses a test directory without a
// Python file; its upgrade scenario refuses a repository without a released
// chart to upgrade from. The two files make a new repository's first pull
// request green: .ats/main.yaml skips the functional and the upgrade
// scenario, tests/ats/test_smoke.py is one smoke test that the job's kind
// cluster is reachable. Both are the repository's own, the shape the
// repositories with real ATS smokes carry: the team extends the test with the
// chart's own checks, and neither the align run nor a later devctl touches
// them.
//
//go:embed scaffold/ats
var chartTestFiles embed.FS

const (
	// atsDependenciesPath is the generated ATS dependency file, the signal
	// that the pipeline runs chart tests with the pytest executor. `devctl
	// gen circleci` emits it under exactly the condition that emits the
	// chart-test job -- the app flavour on generated CI, ATS 1.x, no
	// skipATS, not a template -- so its presence after the generators ran
	// is the one question the scaffold has to ask.
	atsDependenciesPath = "tests/ats/pyproject.toml"
	// atsConfigPath is app-test-suite's configuration, the repository's own.
	atsConfigPath = ".ats/main.yaml"
	// atsSmokeTestPath is the scaffold's smoke test, the repository's own.
	atsSmokeTestPath = "tests/ats/test_smoke.py"
	// pythonSuffix marks the files pytest collects tests from.
	pythonSuffix = ".py"
)

// writeChartTests writes the chart tests into the scaffold at dir after the
// generators ran, when they emitted the ATS dependencies
// (atsDependenciesPath): .ats/main.yaml when the scaffold has none, and
// tests/ats/test_smoke.py -- name is the chart's -- when tests/ats holds no
// Python file. A template that carries its own configuration or tests keeps
// them. A scaffold without the dependency file (no chart, skipATS, a 0.x ATS
// pin on the Pipfile layout, a template repository) gets nothing.
func writeChartTests(dir, name string) error {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(atsDependenciesPath))); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return microerror.Mask(err)
	}

	config := filepath.Join(dir, filepath.FromSlash(atsConfigPath))
	if _, err := os.Stat(config); os.IsNotExist(err) {
		data, err := chartTestFiles.ReadFile("scaffold/ats/main.yaml")
		if err != nil {
			return microerror.Mask(err)
		}
		if err := writeFile(config, data, fileMode); err != nil {
			return microerror.Mask(err)
		}
	} else if err != nil {
		return microerror.Mask(err)
	}

	smoke := filepath.Join(dir, filepath.FromSlash(atsSmokeTestPath))
	hasTests, err := hasPythonFile(filepath.Dir(smoke))
	if err != nil {
		return microerror.Mask(err)
	}
	if hasTests {
		return nil
	}
	data, err := chartTestFiles.ReadFile("scaffold/ats/test_smoke.py")
	if err != nil {
		return microerror.Mask(err)
	}
	test := strings.ReplaceAll(string(data), appNamePlaceholder, name)
	if err := writeFile(smoke, []byte(test), fileMode); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// hasPythonFile says whether dir holds a Python source file anywhere below
// it: what ATS looks for before it runs the pytest executor, and what pytest
// collects tests from. A missing dir holds none.
func hasPythonFile(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), pythonSuffix) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, microerror.Mask(err)
	}
	return found, nil
}
