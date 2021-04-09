package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

func NewMakefileGenAppMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.app.mk",
		TemplateBody: makefileGenAppMkTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}

var makefileGenAppMkTemplate = `{{ .Header }}

##@ App

.PHONY: lint-chart
lint-chart: IMAGE := giantswarm/helm-chart-testing:v3.0.0-rc.1
lint-chart: ## Runs ct against the default chart.
	@echo "====> $@"
	rm -rf /tmp/$(APPLICATION)-test
	mkdir -p /tmp/$(APPLICATION)-test/helm
	cp -a ./helm/$(APPLICATION) /tmp/$(APPLICATION)-test/helm/
	architect helm template --dir /tmp/$(APPLICATION)-test/helm/$(APPLICATION)
	docker run -it --rm -v /tmp/$(APPLICATION)-test:/wd --workdir=/wd --name ct $(IMAGE) ct lint --validate-maintainers=false --charts="helm/$(APPLICATION)"
	rm -rf /tmp/$(APPLICATION)-test
`
