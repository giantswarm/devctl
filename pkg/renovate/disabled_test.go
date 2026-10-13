package renovate

import "testing"

func TestDisables(t *testing.T) {
	testCases := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "JSON5 opt-out", config: "{\n  reviewers: ['teemow'],\n  enabled: false,\n}\n", want: true},
		{name: "JSON opt-out", config: "{\n  \"enabled\": false\n}\n", want: true},
		{name: "a package rule's enabled: false", config: "{\n  packageRules: [\n    {\n      enabled: false,\n    },\n  ],\n}\n"},
		{name: "enabled", config: "{\n  enabled: true,\n}\n"},
		{name: "no file", config: ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Disables([]byte(tc.config)); got != tc.want {
				t.Errorf("Disables() = %v, want %v", got, tc.want)
			}
		})
	}
}
