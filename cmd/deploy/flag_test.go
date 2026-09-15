package deploy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// validFlag returns a flag set that Validate accepts, as a base for the cases
// below to spoil one field at a time.
func validFlag() *flag {
	return &flag{
		GitOpsRepo:        "giantswarm/workload-clusters-fleet",
		ManagementCluster: "gazelle",
		Organization:      "giantswarm",
		WorkloadCluster:   "operations",
		AppName:           "prometheus-operator",
		AppVersion:        "v1.2.3",
		AppCatalog:        "giantswarm",
		AppNamespace:      "monitoring",
		Timeout:           300 * time.Second,
	}
}

func Test_flag_Validate(t *testing.T) {
	t.Run("accepts a well-formed flag set", func(t *testing.T) {
		require.NoError(t, validFlag().Validate())
	})

	// Every value below reaches the argument list of kubectl, so a value that
	// reads as a flag or climbs a path must be refused.
	testCases := []struct {
		name  string
		spoil func(*flag)
	}{
		{"empty app name", func(f *flag) { f.AppName = "" }},
		{"empty app version", func(f *flag) { f.AppVersion = "" }},
		{"app name reads as a flag", func(f *flag) { f.AppName = "--kubeconfig=/tmp/evil" }},
		{"app catalog reads as a flag", func(f *flag) { f.AppCatalog = "-n" }},
		{"namespace reads as a flag", func(f *flag) { f.AppNamespace = "--as=system:admin" }},
		{"management cluster reads as a flag", func(f *flag) { f.ManagementCluster = "--token=x" }},
		{"organization reads as a flag", func(f *flag) { f.Organization = "-o" }},
		{"workload cluster reads as a flag", func(f *flag) { f.WorkloadCluster = "--server=http://evil" }},
		{"app version reads as a flag", func(f *flag) { f.AppVersion = "-1.2.3" }},
		{"app name escapes a path", func(f *flag) { f.AppName = "../../etc/passwd" }},
		{"app name carries a separator", func(f *flag) { f.AppName = "app;id" }},
		{"app version carries a separator", func(f *flag) { f.AppVersion = "1.2.3 --force" }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := validFlag()
			tc.spoil(f)
			require.Error(t, f.Validate())
		})
	}
}
