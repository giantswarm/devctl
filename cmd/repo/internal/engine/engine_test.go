package engine

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// TestCircleCIClient: the reconcile's CircleCI client takes the token from
// $CIRCLECI_TOKEN when set and from the devctl keychain otherwise, says which
// in the log, and never logs the token.
func TestCircleCIClient(t *testing.T) {
	const envValue, keychainValue = "cci-env-value", "cci-keychain-value"

	cases := []struct {
		name     string
		env      string
		keychain bool
		ci       bool

		wantClient bool
		wantLog    string
	}{
		{name: "keychain only", keychain: true,
			wantClient: true, wantLog: "CircleCI token from the devctl keychain"},
		{name: "variable wins over the keychain", env: envValue, keychain: true,
			wantClient: true, wantLog: "CircleCI token from $" + DefaultCircleCIEnvVar},
		{name: "variable only", env: envValue,
			wantClient: true, wantLog: "CircleCI token from $" + DefaultCircleCIEnvVar},
		{name: "neither", wantLog: "the CircleCI and release steps are skipped"},
		{name: "keychain only with CI", keychain: true, ci: true, wantLog: "the CircleCI and release steps are skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyring := filepath.Join(t.TempDir(), "keyring.json")
			if tc.keychain {
				store := &authstore.FileStore{Path: keyring}
				if err := store.Set(authstore.UserCircleCI, authstore.Record{Login: "octocat", Token: keychainValue, ExpiresAt: time.Now().Add(60 * 24 * time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(agentcli.EnvKeyringFile, keyring)
			t.Setenv(DefaultCircleCIEnvVar, tc.env)
			t.Setenv(authstore.EnvCI, "")
			if tc.ci {
				t.Setenv(authstore.EnvCI, "true")
			}
			var log bytes.Buffer
			logger := logrus.New()
			logger.SetOutput(&log)

			client, err := CircleCIClient(context.Background(), logger, DefaultCircleCIEnvVar, nil)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if (client != nil) != tc.wantClient {
				t.Fatalf("client = %v, want one: %v", client, tc.wantClient)
			}
			if !strings.Contains(log.String(), tc.wantLog) {
				t.Errorf("log %q lacks %q", log.String(), tc.wantLog)
			}
			for _, secret := range []string{envValue, keychainValue} {
				if strings.Contains(log.String(), secret) {
					t.Errorf("log carries a token: %q", log.String())
				}
			}
		})
	}
}
