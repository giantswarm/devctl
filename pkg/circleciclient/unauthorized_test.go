package circleciclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// TestUnauthorizedNamesTheTokenSource: CircleCI's 401 names the source the
// token was resolved from, the variable or the devctl keychain, and never a
// variable the command did not read.
func TestUnauthorizedNamesTheTokenSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "Invalid token provided."}`))
	}))
	defer srv.Close()

	cases := []struct {
		name  string
		token authstore.Token
		want  []string
	}{
		{
			name:  "env",
			token: authstore.Token{Value: "cci_bad", Source: "$" + authstore.CircleCIEnvVar},
			want:  []string{"HTTP 401 Invalid token provided.", "check $CIRCLECI_TOKEN:", "surrounding quotes"},
		},
		{
			name:  "keychain",
			token: authstore.Token{Value: "cci_bad", Source: authstore.SourceKeychain},
			want:  []string{"HTTP 401 Invalid token provided.", "check the token from the devctl keychain (`devctl auth login`)"},
		},
		{
			name:  "unnamed",
			token: authstore.Token{Value: "cci_bad"},
			want:  []string{"HTTP 401 Invalid token provided.", "check the CircleCI token"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := circleciclient.New(circleciclient.Config{Token: tc.token.Value, TokenSource: tc.token.SourceName(), BaseURL: srv.URL})
			if err != nil {
				t.Fatalf("client: %v", err)
			}
			_, err = c.ListPipelines(context.Background(), "giantswarm", "devctl", "")
			if err == nil {
				t.Fatal("ListPipelines: want the 401 error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "CIRCLECI_API_TOKEN") {
				t.Errorf("error %q names CIRCLECI_API_TOKEN, a variable devctl does not read", err)
			}
		})
	}
}
