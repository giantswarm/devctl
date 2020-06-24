package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

func NewMakefileInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile",
		TemplateBody: makefileTemplate,
		TemplateData: map[string]interface{}{
			"CurrentFlavour":  p.CurrentFlavour,
			"FlavourApp":      p.FlavourApp,
			"FlavourCLI":      p.FlavourCLI,
			"FlavourLibrary":  p.FlavourLibrary,
			"FlavourOperator": p.FlavourOperator,
		},
	}

	return i
}

var makefileTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen makefile
#

{{- if eq .CurrentFlavour .FlavourCLI}}

PACKAGE_DIR    := ./bin-dist

{{- end}}

APPLICATION    := $(shell basename $(shell go list .))
BUILDTIMESTAMP := $(shell date -u '+%FT%TZ')
GITSHA1        := $(shell git rev-parse --verify HEAD)
OS             := $(shell go env GOOS)
SOURCES        := $(shell find . -name '*.go')
VERSION        := $(shell architect project version)
LDFLAGS        ?= -w -linkmode 'auto' -extldflags '-static' \
  -X '$(shell go list .)/pkg/project.buildTimestamp=${BUILDTIMESTAMP}' \
  -X '$(shell go list .)/pkg/project.gitSHA=${GITSHA1}' \
  -X '$(shell go list .)/pkg/project.version=${VERSION}'
.DEFAULT_GOAL := build

.PHONY: build build-darwin build-linux
## build: builds a local binary
build: $(APPLICATION)
## build-darwin: builds a local binary for darwin/amd64
build-darwin: $(APPLICATION)-darwin
## build-linux: builds a local binary for linux/amd64
build-linux: $(APPLICATION)-linux

{{- if eq .CurrentFlavour .FlavourCLI}}

.PHONY: package-darwin package-linux
## package-darwin: prepares a packaged darwin/amd64 version
package-darwin: $(APPLICATION)-package-darwin
## package-linux: prepares a packaged linux/amd64 version
package-linux: $(APPLICATION)-package-linux

$(APPLICATION)-package-%: $(SOURCES)
	@$(MAKE) $(APPLICATION)-$*
	@$(MAKE) $(APPLICATION)-rename-binary-$*
	@$(MAKE) $(APPLICATION)-archive-$*

$(APPLICATION)-rename-binary-%:
	cp $(APPLICATION)-$* $(APPLICATION)-$(VERSION)-$*-amd64

$(APPLICATION)-archive-%:
	mkdir -p $(PACKAGE_DIR)/$(APPLICATION)-$(VERSION)-$*-amd64
	mv $(APPLICATION)-$(VERSION)-$*-amd64 $(PACKAGE_DIR)/$(APPLICATION)-$(VERSION)-$*-amd64
	cp ./{README.md,LICENSE} $(PACKAGE_DIR)/$(APPLICATION)-$(VERSION)-$*-amd64
	cd $(PACKAGE_DIR)/ && \
	tar -cvzf $(APPLICATION)-$(VERSION)-$*-amd64.tar.gz \
		$(APPLICATION)-$(VERSION)-$*-amd64
	rm -rf $(PACKAGE_DIR)/$(APPLICATION)-$(VERSION)-$*-amd64

{{- end}}

$(APPLICATION): $(APPLICATION)-$(OS)
	cp -a $< $@

$(APPLICATION)-%: $(SOURCES)
	GOOS=$* GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ .

.PHONY: install
## install: install the application
install:
	go install -ldflags "$(LDFLAGS)" .

.PHONY: run
## run: runs go run main.go
run:
	go run -ldflags "$(LDFLAGS)" -race .

.PHONY: clean
## clean: cleans the binary
clean:
	rm -f $(APPLICATION)*
	go clean

.PHONY: imports
## imports: runs goimports
imports:
	goimports -local $(shell go list .) -w .

.PHONY: lint
## lint: runs golangci-lint
lint:
	golangci-lint run -E gosec -E goconst --timeout=15m ./...

.PHONY: test
## test: runs go test with default values
test:
	go test -ldflags "$(LDFLAGS)" -race ./...

.PHONY: build-docker
## build-docker: builds docker image to registry
build-docker: build-linux
	docker build -t ${APPLICATION}:${VERSION} .

.PHONY: help
## help: prints this help message
help:
	@echo "Usage: \n"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'
`
