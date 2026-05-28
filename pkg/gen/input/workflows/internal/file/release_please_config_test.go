package file

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"text/template"
)

type releasePleaseExtraFile struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type releasePleaseSection struct {
	Type    string `json:"type"`
	Section string `json:"section"`
	Hidden  *bool  `json:"hidden,omitempty"`
}

type releasePleaseConfig struct {
	ReleaseType       string                            `json:"release-type"`
	ExtraFiles        []releasePleaseExtraFile          `json:"extra-files"`
	ChangelogSections []releasePleaseSection            `json:"changelog-sections"`
	Packages          map[string]map[string]interface{} `json:"packages"`
}

func Test_NewReleasePleaseConfigInput(t *testing.T) {
	testCases := []struct {
		name             string
		changelogStyle   string
		hasProjectGo     bool
		expectedSections map[string]string
		expectExtraFiles []releasePleaseExtraFile
	}{
		{
			name:           "release-please style without project.go",
			changelogStyle: "release-please",
			hasProjectGo:   false,
			expectedSections: map[string]string{
				"feat":     "### Added",
				"fix":      "### Fixed",
				"perf":     "### Changed",
				"revert":   "### Changed",
				"refactor": "### Changed",
				"docs":     "### Changed",
				"style":    "### Changed",
				"test":     "### Changed",
				"build":    "### Changed",
				"ci":       "### Changed",
				"chore":    "### Changed",
				"security": "### Security",
			},
		},
		{
			name:           "release-please style with project.go",
			changelogStyle: "release-please",
			hasProjectGo:   true,
			expectedSections: map[string]string{
				"feat":     "### Added",
				"fix":      "### Fixed",
				"perf":     "### Changed",
				"revert":   "### Changed",
				"refactor": "### Changed",
				"docs":     "### Changed",
				"style":    "### Changed",
				"test":     "### Changed",
				"build":    "### Changed",
				"ci":       "### Changed",
				"chore":    "### Changed",
				"security": "### Security",
			},
			expectExtraFiles: []releasePleaseExtraFile{
				{Type: "generic", Path: "pkg/project/project.go"},
			},
		},
		{
			name:           "legacy style without project.go",
			changelogStyle: "legacy",
			hasProjectGo:   false,
			expectedSections: map[string]string{
				"feat":     "### Added",
				"fix":      "### Fixed",
				"refactor": "### Changed",
				"perf":     "### Changed",
				"docs":     "### Changed",
				"chore":    "### Changed",
				"test":     "### Changed",
				"build":    "### Changed",
				"ci":       "### Changed",
				"security": "### Security",
			},
		},
		{
			name:           "legacy style with project.go",
			changelogStyle: "legacy",
			hasProjectGo:   true,
			expectedSections: map[string]string{
				"feat":     "### Added",
				"fix":      "### Fixed",
				"refactor": "### Changed",
				"perf":     "### Changed",
				"docs":     "### Changed",
				"chore":    "### Changed",
				"test":     "### Changed",
				"build":    "### Changed",
				"ci":       "### Changed",
				"security": "### Security",
			},
			expectExtraFiles: []releasePleaseExtraFile{
				{Type: "generic", Path: "pkg/project/project.go"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := NewReleasePleaseConfigInput(tc.changelogStyle, tc.hasProjectGo)

			tpl, err := template.New("release-please-config").
				Delims(in.TemplateDelims.Left, in.TemplateDelims.Right).
				Parse(in.TemplateBody)
			if err != nil {
				t.Fatalf("parse template: %v", err)
			}

			var rendered bytes.Buffer
			if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
				t.Fatalf("execute template: %v", err)
			}

			var got releasePleaseConfig
			if err := json.Unmarshal(rendered.Bytes(), &got); err != nil {
				t.Fatalf("rendered template is not valid JSON: %v\n%s", err, rendered.String())
			}

			if got.ReleaseType != "simple" {
				t.Errorf("release-type: expected %q, got %q", "simple", got.ReleaseType)
			}

			gotSections := map[string]string{}
			for _, s := range got.ChangelogSections {
				if s.Hidden != nil {
					t.Errorf("type %q must not set hidden, got %v", s.Type, *s.Hidden)
				}
				gotSections[s.Type] = s.Section
			}
			if !reflect.DeepEqual(gotSections, tc.expectedSections) {
				t.Errorf("changelog-sections mismatch\ngot:  %#v\nwant: %#v", gotSections, tc.expectedSections)
			}

			if !reflect.DeepEqual(got.ExtraFiles, tc.expectExtraFiles) {
				t.Errorf("extra-files mismatch\ngot:  %#v\nwant: %#v", got.ExtraFiles, tc.expectExtraFiles)
			}

			// `packages` is required at the top level of release-please-config.json
			// per the official schema. Without it release-please loads the config,
			// finds no packages to release, and exits without opening a PR.
			if _, ok := got.Packages["."]; !ok {
				t.Errorf("packages must declare the root \".\" entry; got %#v", got.Packages)
			}
		})
	}
}
