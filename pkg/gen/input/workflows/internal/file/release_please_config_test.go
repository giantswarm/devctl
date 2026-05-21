package file

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"text/template"
)

type releasePleaseSection struct {
	Type    string `json:"type"`
	Section string `json:"section"`
	Hidden  *bool  `json:"hidden,omitempty"`
}

type releasePleaseConfig struct {
	ReleaseType       string                 `json:"release-type"`
	ChangelogSections []releasePleaseSection `json:"changelog-sections"`
}

func Test_NewReleasePleaseConfigInput(t *testing.T) {
	testCases := []struct {
		name             string
		changelogStyle   string
		expectedSections map[string]string
	}{
		{
			name:           "release-please style writes full Angular-to-KaC mapping plus security",
			changelogStyle: "release-please",
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
			name:           "legacy style keeps existing types and adds security",
			changelogStyle: "legacy",
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := NewReleasePleaseConfigInput(tc.changelogStyle)

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
		})
	}
}
