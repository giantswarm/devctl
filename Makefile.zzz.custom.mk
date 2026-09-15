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

# The cliff.toml cases in pkg/gen/input/workflows run the real git-cliff, so
# `make test` puts one on PATH. Pinned to the version the generated
# auto-release workflow installs (pkg/gen/input/workflows/internal/file/
# auto_release.yaml.template), because what the cases assert is how that
# version reads the rendered config.
GIT_CLIFF_VERSION := 2.13.1
GIT_CLIFF_DIR := $(CURDIR)/.build/git-cliff/$(GIT_CLIFF_VERSION)
GIT_CLIFF_TARGET := $(shell uname -m | sed 's/arm64/aarch64/')-$(shell uname -s | sed 's/Linux/unknown-linux-gnu/;s/Darwin/apple-darwin/')
GIT_CLIFF_URL := https://github.com/orhun/git-cliff/releases/download/v$(GIT_CLIFF_VERSION)/git-cliff-$(GIT_CLIFF_VERSION)-$(GIT_CLIFF_TARGET).tar.gz

export PATH := $(GIT_CLIFF_DIR):$(PATH)

.PHONY: git-cliff
git-cliff: $(GIT_CLIFF_DIR)/git-cliff ## Install the pinned git-cliff under .build.

# A download that fails in CI fails the build: the cases would otherwise skip
# and report green, which is the one place they have to run. Off CI it only
# warns, so an offline checkout still runs the rest of the suite.
$(GIT_CLIFF_DIR)/git-cliff:
	@mkdir -p $(GIT_CLIFF_DIR)
	@curl -sSfL $(GIT_CLIFF_URL) \
	  | tar -xz -C $(GIT_CLIFF_DIR) --strip-components=1 git-cliff-$(GIT_CLIFF_VERSION)/git-cliff \
	  || { [ -z "$$CI" ] || exit 1; echo "warning: cannot download git-cliff from $(GIT_CLIFF_URL); its tests will skip"; }

test: git-cliff
