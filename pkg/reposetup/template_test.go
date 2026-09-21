package reposetup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveTemplate(t *testing.T) {
	tests := []struct {
		name          string
		componentType string
		flavours      []string
		language      string
		want          Template
		unavailable   bool
		wantErr       bool
	}{
		{name: "go service with chart", componentType: "service", flavours: []string{"app"}, language: "go", want: TemplateGo},
		{name: "go cli", componentType: "cli", flavours: []string{"cli"}, language: "go", want: TemplateGo},
		{name: "go library", componentType: "library", flavours: []string{"generic"}, language: "go", want: TemplateGo},
		{name: "go k8s api", componentType: "service", flavours: []string{"app", "k8sapi"}, language: "go", want: TemplateGo},
		{name: "chart-only app", componentType: "service", flavours: []string{"app"}, language: "generic", want: TemplateChart},
		{name: "chart-only configuration component", componentType: "configuration", flavours: []string{"app"}, language: "generic", want: TemplateChart},
		{name: "cluster app chart", componentType: "service", flavours: []string{"cluster-app"}, language: "generic", want: TemplateMinimal},
		{name: "generic configuration", componentType: "configuration", flavours: []string{"generic"}, language: "generic", want: TemplateMinimal},
		{name: "fleet configuration", componentType: "configuration", flavours: []string{"fleet"}, language: "generic", want: TemplateMinimal},
		{name: "customer by component type", componentType: "customer", flavours: []string{"app"}, language: "go", want: TemplateMinimal},
		{name: "customer by flavour", componentType: "configuration", flavours: []string{"customer"}, language: "generic", want: TemplateMinimal},
		{name: "fork line", componentType: "service", flavours: []string{"fork"}, language: "go", want: TemplateMinimal},
		{name: "python", componentType: "cli", flavours: []string{"generic"}, language: "python", want: TemplateMinimal},
		{name: "kyverno policy", componentType: "configuration", flavours: []string{"generic"}, language: "kyverno-policy", want: TemplateMinimal},
		{name: "node is deferred", componentType: "service", flavours: []string{"generic"}, language: "node", unavailable: true},
		{name: "unknown language", componentType: "service", flavours: []string{"generic"}, language: "rust", wantErr: true},
		{name: "unknown flavour", componentType: "service", flavours: []string{"helmchart"}, language: "generic", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveTemplate(tc.componentType, tc.flavours, tc.language)
			switch {
			case tc.unavailable:
				require.True(t, IsTemplateUnavailable(err), "%v", err)
				require.Contains(t, err.Error(), "the Node template is not available yet")
			case tc.wantErr:
				require.Error(t, err)
				require.False(t, IsTemplateUnavailable(err))
			default:
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestTemplateRepository(t *testing.T) {
	require.Equal(t, "giantswarm/template", TemplateGo.Repository())
	require.Equal(t, "giantswarm/template-app", TemplateChart.Repository())
	require.Equal(t, "", TemplateMinimal.Repository())
	require.Equal(t, "minimal", TemplateMinimal.String())
}

// TestDeriveChart: a template without a chart of its own gets the chart
// template's chart when the flavours produce one; the chart template and a
// declaration without a chart get none.
func TestDeriveChart(t *testing.T) {
	app := []string{"app"}
	require.Equal(t, TemplateChart, DeriveChart(TemplateGo, app), "the Go template has no chart: the chart template's is added")
	require.Equal(t, TemplateChart, DeriveChart(TemplateMinimal, []string{"cluster-app"}))
	require.Equal(t, Template(""), DeriveChart(TemplateChart, app), "the chart template carries its chart")
	require.Equal(t, Template(""), DeriveChart(TemplateGo, []string{"generic"}), "no chart declared")
	require.Equal(t, Template(""), DeriveChart(TemplateGo, nil))
}
