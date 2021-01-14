package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

func NewMakefileGenGoMkInput(p params.Params) input.Input {
	i := input.Input{
		Path:         "Makefile.gen.go.mk",
		TemplateBody: makefileGenGoMkTemplate,
		TemplateData: map[string]interface{}{
			"IsFlavourApp": params.IsFlavourApp(p),
			"IsFlavourCLI": params.IsFlavourCLI(p),
			"Header":       params.Header("#"),
		},
	}

	return i
}

var makefileGenGoMkTemplate = `{{ .Header }}

{{- if .IsFlavourCLI }}

PACKAGE_DIR    := ./bin-dist

{{- end }}

APPLICATION    := $(shell go list . | cut -d '/' -f 3)
BUILDTIMESTAMP := $(shell date -u '+%FT%TZ')
GITSHA1        := $(shell git rev-parse --verify HEAD)
OS             := $(shell go env GOOS)
SOURCES        := $(shell find . -name '*.go')
VERSION        := $(shell architect project version)
ifeq ($(OS), linux)
EXTLDFLAGS := -static
endif
LDFLAGS        ?= -w -linkmode 'auto' -extldflags '$(EXTLDFLAGS)' \
  -X '$(shell go list .)/pkg/project.buildTimestamp=${BUILDTIMESTAMP}' \
  -X '$(shell go list .)/pkg/project.gitSHA=${GITSHA1}'
.DEFAULT_GOAL := build

.PHONY: build build-darwin build-linux
## build: builds a local binary
build: $(APPLICATION)
	@echo "====> $@"
## build-darwin: builds a local binary for darwin/amd64
build-darwin: $(APPLICATION)-darwin
	@echo "====> $@"
## build-linux: builds a local binary for linux/amd64
build-linux: $(APPLICATION)-linux
	@echo "====> $@"

$(APPLICATION): $(APPLICATION)-v$(VERSION)-$(OS)-amd64
	@echo "====> $@"
	cp -a $< $@

$(APPLICATION)-darwin: $(APPLICATION)-v$(VERSION)-darwin-amd64
	@echo "====> $@"
	cp -a $< $@

$(APPLICATION)-linux: $(APPLICATION)-v$(VERSION)-linux-amd64
	@echo "====> $@"
	cp -a $< $@

$(APPLICATION)-v$(VERSION)-%-amd64: $(SOURCES)
	@echo "====> $@"
	CGO_ENABLED=0 GOOS=$* GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ .

{{- if .IsFlavourCLI }}

.PHONY: package-darwin package-linux
## package-darwin: prepares a packaged darwin/amd64 version
package-darwin: $(PACKAGE_DIR)/$(APPLICATION)-v$(VERSION)-darwin-amd64.tar.gz
	@echo "====> $@"
## package-linux: prepares a packaged linux/amd64 version
package-linux: $(PACKAGE_DIR)/$(APPLICATION)-v$(VERSION)-linux-amd64.tar.gz
	@echo "====> $@"

$(PACKAGE_DIR)/$(APPLICATION)-v$(VERSION)-%-amd64.tar.gz: DIR=$(PACKAGE_DIR)/$<
$(PACKAGE_DIR)/$(APPLICATION)-v$(VERSION)-%-amd64.tar.gz: $(APPLICATION)-v$(VERSION)-%-amd64
	@echo "====> $@"
	mkdir -p $(DIR)
	cp $< $(DIR)/$(APPLICATION)
	cp README.md LICENSE $(DIR)
	tar -C $(PACKAGE_DIR) -cvzf $(PACKAGE_DIR)/$<.tar.gz $<
	rm -rf $(DIR)
	rm -rf $<

{{- end }}

.PHONY: install
## install: install the application
install:
	@echo "====> $@"
	go install -ldflags "$(LDFLAGS)" .

.PHONY: run
## run: runs go run main.go
run:
	@echo "====> $@"
	go run -ldflags "$(LDFLAGS)" -race .

.PHONY: clean
## clean: cleans the binary
clean:
	@echo "====> $@"
	rm -f $(APPLICATION)*
	go clean

.PHONY: imports
## imports: runs goimports
imports:
	@echo "====> $@"
	goimports -local $(shell go list .) -w .

.PHONY: lint
## lint: runs golangci-lint
lint:
	@echo "====> $@"
	golangci-lint run -E gosec -E goconst --timeout=15m ./...

.PHONY: test
## test: runs go test with default values
test:
	@echo "====> $@"
	go test -ldflags "$(LDFLAGS)" -race ./...

.PHONY: build-docker
## build-docker: builds docker image to registry
build-docker: build-linux
	@echo "====> $@"
	docker build -t ${APPLICATION}:${VERSION} .
`
