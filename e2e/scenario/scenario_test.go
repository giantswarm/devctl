package scenario

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decode(t *testing.T, doc string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(doc), &v))
	return v
}

func TestMatch(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		actual   string
		want     string
	}{
		{"equal scalars", `{"a":1,"b":"x","c":true,"d":null}`, `{"a":1,"b":"x","c":true,"d":null}`, ""},
		{"wildcard string", `{"startedAt":"*"}`, `{"startedAt":"2026-01-01T00:00:00Z"}`, ""},
		{"wildcard any type", `{"n":"*","o":"*","l":"*","z":"*"}`, `{"n":3,"o":{"k":1},"l":[1],"z":null}`, ""},
		{"nested", `{"a":{"b":[{"c":"*"},{"c":2}]}}`, `{"a":{"b":[{"c":1},{"c":2}]}}`, ""},
		{"scalar differs", `{"a":1}`, `{"a":2}`, "at $.a: want 1, got 2"},
		{"string differs", `{"a":"x"}`, `{"a":"y"}`, `at $.a: want "x", got "y"`},
		{"type differs", `{"a":"1"}`, `{"a":1}`, `at $.a: want "1", got 1`},
		{"missing key", `{"a":1,"b":2}`, `{"a":1}`, "at $.b: key missing, want 2"},
		{"extra key", `{"a":1}`, `{"a":1,"b":2}`, "at $.b: key not expected, value 2"},
		{"array length", `{"a":[1,2]}`, `{"a":[1]}`, "at $.a: want 2 elements, got 1"},
		{"array element", `{"a":[1,2]}`, `{"a":[1,3]}`, "at $.a[1]: want 2, got 3"},
		{"object wanted", `{"a":{"b":1}}`, `{"a":[1]}`, `at $.a: want {"b":1}, got [1]`},
		{"null wanted", `{"a":null}`, `{"a":1}`, "at $.a: want null, got 1"},
		{"top-level array", `[{"x":"*"}]`, `[{"x":5}]`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Match(decode(t, tc.expected), decode(t, tc.actual))
			if tc.want == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ScenarioFile), `
description: a pull request whose checks finish on the second poll
args: [pr, wait, giantswarm/devctl, "42"]
env:
  DEVCTL_TIME_SCALE: "0.01"
timeout: 5s
keyring:
  github:
    token: ghu_test
github:
  routes:
    "GET /repos/giantswarm/devctl/pulls/42":
      - body: {number: 42, head: {sha: abc}}
    "GET /repos/giantswarm/devctl/commits/abc/check-runs":
      - body: {total_count: 0, check_runs: []}
      - status: 200
        headers: {X-RateLimit-Remaining: "10"}
        body: {total_count: 1, check_runs: [{name: ci, status: completed, conclusion: success}]}
circleci:
  routes:
    "GET /api/v2/project/gh/giantswarm/devctl/pipeline?branch=main":
      - body: {items: []}
registry:
  routes:
    "HEAD /v2/giantswarm/devctl/manifests/v1.0.0":
      - status: 404
      - status: 200
privateRegistry:
  staleLogin: true
`)
	write(t, filepath.Join(dir, ExpectedFile), `{"exitCode": 2, "json": {"verdict": "timeout", "finishedAt": "*"}}`)

	sc, expected, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"pr", "wait", "giantswarm/devctl", "42"}, sc.Args)
	assert.Equal(t, map[string]string{"DEVCTL_TIME_SCALE": "0.01"}, sc.Env)
	assert.Equal(t, 5*time.Second, sc.Timeout)
	assert.Equal(t, map[string]any{"github": map[string]any{"token": "ghu_test"}}, sc.Keyring)
	require.Len(t, sc.GitHub.Routes["GET /repos/giantswarm/devctl/commits/abc/check-runs"], 2)
	second := sc.GitHub.Routes["GET /repos/giantswarm/devctl/commits/abc/check-runs"][1]
	assert.Equal(t, 200, second.Status)
	assert.Equal(t, "10", second.Headers["X-RateLimit-Remaining"])
	body, err := second.Bytes()
	require.NoError(t, err)
	assert.JSONEq(t, `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success"}]}`, string(body))
	assert.Len(t, sc.CircleCI.Routes, 1)
	assert.Len(t, sc.Registry.Routes["HEAD /v2/giantswarm/devctl/manifests/v1.0.0"], 2)
	assert.False(t, sc.Registry.StaleLogin)
	assert.True(t, sc.PrivateRegistry.StaleLogin)
	assert.Empty(t, sc.PrivateRegistry.Routes)

	assert.Equal(t, 2, expected.ExitCode)
	assert.True(t, expected.ComparesJSON())
	assert.JSONEq(t, `{"verdict":"timeout","finishedAt":"*"}`, string(expected.JSON))
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ScenarioFile), "args: [version]\n")
	write(t, filepath.Join(dir, ExpectedFile), `{"exitCode": 0}`)

	sc, expected, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, DefaultTimeout, sc.Timeout)
	assert.Nil(t, sc.Keyring)
	assert.False(t, expected.ComparesJSON(), "without json stdout is not compared")

	write(t, filepath.Join(dir, ExpectedFile), `{"exitCode": 0, "json": null}`)
	_, expected, err = Load(dir)
	require.NoError(t, err)
	assert.False(t, expected.ComparesJSON())
}

func TestLoadRefusesUnknownFieldsAndMissingArgs(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ExpectedFile), `{"exitCode": 0}`)

	write(t, filepath.Join(dir, ScenarioFile), "args: [version]\nargz: []\n")
	_, _, err := Load(dir)
	assert.ErrorContains(t, err, "argz")

	write(t, filepath.Join(dir, ScenarioFile), "description: no command\n")
	_, _, err = Load(dir)
	assert.ErrorContains(t, err, "args must name the command")

	write(t, filepath.Join(dir, ScenarioFile), "args: [version]\n")
	write(t, filepath.Join(dir, ExpectedFile), `{"exitCode": 0, "stdout": ""}`)
	_, _, err = Load(dir)
	assert.ErrorContains(t, err, "stdout")

	require.NoError(t, os.Remove(filepath.Join(dir, ExpectedFile)))
	_, _, err = Load(dir)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	for _, slug := range []string{"b-second", "a-first"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, slug), 0o750))
		write(t, filepath.Join(root, slug, ScenarioFile), "args: [version]\n")
	}
	require.NoError(t, os.Mkdir(filepath.Join(root, "not-a-scenario"), 0o750))

	dirs, err := Discover(root)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(root, "a-first"), filepath.Join(root, "b-second")}, dirs)
}
