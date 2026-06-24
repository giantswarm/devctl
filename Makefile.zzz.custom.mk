.PHONY: generate-go
generate-go: # Generate template files needed to run
	go generate ./...

$(SOURCES): generate-go

install: generate-go

# `go generate` writes gitignored *.template.sha provenance files that are pulled
# in via //go:embed, so a clean checkout cannot compile (or run tests) until they
# exist. Make `make test` regenerate them first — this is what lets the generated
# CircleCI go-build (test_target: test) build devctl from a clean CI checkout.
test: generate-go
