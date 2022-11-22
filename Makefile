PKG := github.com/lightninglabs/faraday

GOACC_PKG := github.com/ory/go-acc
GOIMPORTS_PKG := github.com/rinchsan/gosimports/cmd/gosimports
TOOLS_DIR := tools

GO_BIN := ${GOPATH}/bin
GOACC_BIN := $(GO_BIN)/go-acc
GOIMPORTS_BIN := $(GO_BIN)/gosimports

COMMIT := $(shell git describe --abbrev=40 --dirty)
LDFLAGS := -ldflags "-X $(PKG).Commit=$(COMMIT)"

GOBUILD := go build -v
GOINSTALL := go install -v
GOTEST := go test -v
GOMOD := go mod

GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -name "*pb.go" -not -name "*pb.gw.go" -not -name "*.pb.json.go")
GOLIST := go list -deps $(PKG)/... | grep '$(PKG)'| grep -v '/vendor/' | grep -v '/itest'
GOLISTCOVER := $(shell go list -deps -f '{{.ImportPath}}' ./... | grep '$(PKG)' | sed -e 's/^$(ESCPKG)/./')

RM := rm -f
CP := cp
MAKE := make
XARGS := xargs -L 1

include make/testing_flags.mk

LINT = $(LINT_BIN) run -v
DOCKER_TOOLS = docker run -v $$(pwd):/build faraday-tools

default: scratch

all: scratch check install

# ============
# DEPENDENCIES
# ============

$(GOACC_BIN):
	@$(call print, "Installing go-acc.")
	cd $(TOOLS_DIR); go install -trimpath -tags=tools $(GOACC_PKG)

$(GOIMPORTS_BIN):
	@$(call print, "Installing goimports.")
	cd $(TOOLS_DIR); go install -trimpath $(GOIMPORTS_PKG)

# ============
# INSTALLATION
# ============

build:
	@$(call print, "Building faraday.")
	$(GOBUILD) $(LDFLAGS) $(PKG)/cmd/faraday
	$(GOBUILD) $(LDFLAGS) $(PKG)/cmd/frcli

install:
	@$(call print, "Installing faraday.")
	$(GOINSTALL) $(LDFLAGS) $(PKG)/cmd/faraday
	$(GOINSTALL) $(LDFLAGS) $(PKG)/cmd/frcli

docker-tools:
	@$(call print, "Building tools docker image.")
	docker build -q -t faraday-tools $(TOOLS_DIR)

scratch: build

# =======
# TESTING
# =======

check: unit

itest:
	@$(call print, "Running integration tests.")
	./run_itest.sh

unit:
	@$(call print, "Running unit tests.")
	$(UNIT)

unit-cover: $(GOACC_BIN)
	@$(call print, "Running unit coverage tests.")
	$(GOACC_BIN) $(COVER_PKG)

unit-race:
	@$(call print, "Running unit race tests.")
	env CGO_ENABLED=1 GORACE="history_size=7 halt_on_errors=1" $(UNIT_RACE)


# =============
# FLAKE HUNTING
# =============
flake-unit:
	@$(call print, "Flake hunting unit tests.")
	while [ $$? -eq 0 ]; do GOTRACEBACK=all $(UNIT) -count=1; done

# =========
# UTILITIES
# =========
fmt: $(GOIMPORTS_BIN)
	@$(call print, "Fixing imports.")
	gosimports -w $(GOFILES_NOVENDOR)
	@$(call print, "Formatting source.")
	gofmt -l -w -s $(GOFILES_NOVENDOR)

lint: docker-tools
	@$(call print, "Linting source.")
	$(DOCKER_TOOLS) golangci-lint run -v $(LINT_WORKERS)

mod:
	@$(call print, "Tidying modules.")
	$(GOMOD) tidy

mod-check:
	@$(call print, "Checking modules.")
	$(GOMOD) tidy
	if test -n "$$(git status | grep -e "go.mod\|go.sum")"; then echo "Running go mod tidy changes go.mod/go.sum"; git status; git diff; exit 1; fi

rpc:
	@$(call print, "Compiling protos.")
	cd ./frdrpc; ./gen_protos_docker.sh

rpc-check: rpc
	@$(call print, "Verifying protos.")
	if test -n "$$(git describe --dirty | grep dirty)"; then echo "Protos not properly formatted or not compiled with v3.4.0"; git status; git diff; exit 1; fi

rpc-format:
	@$(call print, "Formatting protos.")
	cd ./frdrpc; find . -name "*.proto" | xargs clang-format --style=file -i

rpc-js-compile:
	@$(call print, "Compiling JSON/WASM stubs.")
	GOOS=js GOARCH=wasm $(GOBUILD) $(PKG)/frdrpc

list:
	@$(call print, "Listing commands.")
	@$(MAKE)  -qp | \
		awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {split($$1,A,/ /);for(i in A)print A[i]}' | \
		grep -v Makefile | \
		sort

clean:
	@$(call print, "Cleaning source.$(NC)")
	$(RM) ./faraday
	$(RM) ./frcli
	$(RM) coverage.txt

# Instruct make to not interpret these as file/folder related targets, otherwise
# it will behave weirdly if a file or folder exists with that name.
.PHONY: itest
