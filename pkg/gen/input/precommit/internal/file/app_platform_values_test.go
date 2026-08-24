package file

import (
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// The app-platform values file exists so the generated values.schema.json
// (schemaRoot.additionalProperties: false) accepts the keys the app platform
// merges into every App via the <cluster>-cluster-values ConfigMap.
func Test_NewCreateAppPlatformValuesInput(t *testing.T) {
	got := NewCreateAppPlatformValuesInput(params.Params{Dir: ""}, "my-app")

	if got.Path != "helm/my-app/zz_generated.app-platform.values.yaml" {
		t.Errorf("path: expected %q, got %q", "helm/my-app/zz_generated.app-platform.values.yaml", got.Path)
	}

	// All 12 cluster-values keys must be present; object keys must stay open
	// (skipProperties + additionalProperties) since their content is
	// provider-dependent and owned by the platform, not the chart.
	for _, want := range []string{
		"baseDomain:",
		"bootstrapMode: {}  # @schema skipProperties: true; additionalProperties: true",
		"chartOperator: {}  # @schema skipProperties: true; additionalProperties: true",
		"ciliumNetworkPolicy: {}  # @schema skipProperties: true; additionalProperties: true",
		"cluster: {}  # @schema skipProperties: true; additionalProperties: true",
		"clusterCA:",
		"clusterCIDR:",
		"clusterDNSIP:",
		"clusterID:",
		"gcpProject:",
		"provider:",
		"subscriptionID:",
	} {
		if !strings.Contains(got.TemplateBody, want) {
			t.Errorf("expected %q in template body", want)
		}
	}
}
