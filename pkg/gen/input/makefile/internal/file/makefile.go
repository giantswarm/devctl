package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/makefile/internal/params"
)

func NewMakefileInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "Makefile"),
		TemplateBody: makefileTemplate,
		TemplateData: map[string]interface{}{
			"Application": params.Application(p),
		},
	}

	return i
}

var makefileTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen makefile
#

APPLICATION    ?= {{.Application}}
VERSION        ?= $(shell architect project version)
GITSHA1        = $(shell git rev-parse --verify HEAD)
BUILDTIMESTAMP = $(shell date -u '+%FT%TZ')
LDFLAGS        ?= -w -linkmode 'auto' -extldflags '-static' \
  -X '$(go list .)/pkg/project.buildTimestamp=${BUILDTIMESTAMP}' \
  -X '$(go list .)/pkg/project.gitSHA=${GITSHA1}'
default: build

.PHONY: build
## build: builds a local binary
build: clean
	CGO_ENABLED=0 go build -o ${APPLICATION} .

.PHONY: build-linux
## build-linux: builds binary for linux/amd64
build-linux: clean
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build .

.PHONY: build-darwin
## build-darwin: builds binary for darwin/amd64
build-darwin: clean
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build .

.PHONY: install
## install: install the application
install:
	go install .

.PHONY: run
## run: runs go run main.go
run:
	go run -race .

.PHONY: clean
## clean: cleans the binary
clean:
	go clean

.PHONY: lint
## lint: runs golangci-lint
lint:
	golangci-lint run -E gosec -E goconst --timeout=15m ./...

.PHONY: test
## test: runs go test with default values
test:
	go test -ldflags $(LDFLAGS) -race -cover ./...

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
