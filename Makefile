##### Main Build and Test Variables #####
# Version Variables that are frequently modified
DOCKER_LINT_IMAGE?=golangci/golangci-lint:v2.12.2
DOCKER_BUILD_IMAGE?=golang:1.26.6
# In GitHub actions where we make the official image, the runtime base is gcr.io/distroless/static to make a slim
# container, however, here we use the full alpine image because the containers that come from the Makefile are presumed
# to mostly be used for debugging and testing rather than distribution.
RUNTIME_BASE?=alpine:3.24

# Other build / test variables
NAME?=rtorrent-exporter
BUILD_DATE?=$(shell date +%Y-%m-%dT%H:%M:%S%z)
BUILD_IN_DOCKER?=true
GO_TEST_NO_CACHE?=false
GO_TEST_FLAGS?=$(if $(GO_TEST_NO_CACHE),-count=1)
IS_ROOT=$(filter 0,$(shell id -u))

# Docker variables
IN_DOCKER_GROUP=$(filter docker,$(shell groups))
DOCKER=$(if $(or $(IN_DOCKER_GROUP),$(IS_ROOT),$(OSX)),docker,sudo docker)

# Git Variables
GIT_COMMIT=$(shell git -c safe.directory="*" describe --tags --dirty)
GIT_BRANCH?=$(shell git rev-parse --abbrev-ref HEAD)

# Go Variables
GO_MOD_CACHE?=$(shell go env GOMODCACHE)
GO_CACHE?=$(shell go env GOCACHE)
GOARCH?=$(shell go env GOARCH)


##### Container variables #####
# Go arch container tag variables
ifeq ($(GOARCH), arm)
ARCH_TAG_PREFIX=$(GOARCH)
FILE_ARCH=ARM
DOCKER_ARCH=arm32v6/
else ifeq ($(GOARCH), arm64)
ARCH_TAG_PREFIX=$(GOARCH)
FILE_ARCH=ARM aarch64
DOCKER_ARCH=arm64v8/
else ifeq ($(GOARCH), s390x)
ARCH_TAG_PREFIX=$(GOARCH)
FILE_ARCH=IBM S/390
DOCKER_ARCH=s390x/
else ifeq ($(GOARCH), ppc64le)
ARCH_TAG_PREFIX=$(GOARCH)
FILE_ARCH=64-bit PowerPC
DOCKER_ARCH=ppc64le/
else ifeq ($(GOARCH), riscv64)
ARCH_TAG_PREFIX=$(GOARCH)
FILE_ARCH=UCB RISC-V, RVC, double-float ABI
DOCKER_ARCH=riscv64/
else
ARCH_TAG_PREFIX=amd64
FILE_ARCH=x86-64
DOCKER_ARCH=
endif

# Container build variables
DEV_SUFFIX?=-git
QEMU_IMAGE?=multiarch/qemu-user-static
IMG_NAMESPACE?=aauren
REGISTRY?=$(if $(IMG_FQDN),$(IMG_FQDN)/$(IMG_NAMESPACE)/$(NAME),$(IMG_NAMESPACE)/$(NAME))
REGISTRY_DEV?=$(REGISTRY)$(DEV_SUFFIX)
IMG_TAG?=$(if $(IMG_TAG_PREFIX),$(IMG_TAG_PREFIX)-)$(if $(ARCH_TAG_PREFIX),$(ARCH_TAG_PREFIX)-)$(GIT_BRANCH)
BUILDTIME_BASE?=$(DOCKER_BUILD_IMAGE)

.DEFAULT_GOAL := all
.PHONY: all clean test lint genmoqs instdeps gofmt gofmt-fix container container-test-bundle

clean:
	@echo Starting rtorrent-exporter clean
	rm -f rtorrent-exporter
	@echo Finished rtorrent-exporter clean

gofmt:
	@echo Starting rtorrent-exporter gofmt check
	@gofmt -l -s $(shell find . -not \( \( -wholename '*/vendor/*' \) -prune \) -name '*.go')
	@echo Finished rtorrent-exporter gofmt check

gofmt-fix:
	@echo Starting rtorrent-exporter gofmt fixing
	goimports -w $(shell find . -not \( \( -wholename '*/vendor/*' \) -prune \) -name '*.go')
	gofmt -s -w $(shell find . -not \( \( -wholename '*/vendor/*' \) -prune \) -name '*.go')
	@echo Finished rtorrent-exporter gofmt fixing

instdeps:
	@echo Starting rtorrent-exporter install go deps
	go get ./...
	@echo Finished rtorrent-exporter install go deps

updatedeps:
	@echo Starting rtorrent-exporter update go deps
	go get -u ./...
	@echo Finished rtorrent-exporter update go deps

lint: gofmt
ifeq "$(BUILD_IN_DOCKER)" "true"
	@echo Starting rtorrent-exporter linting in Docker
	$(DOCKER) run -v $(PWD):/go/src/github.com/aauren/rtorrent-exporter \
		-v $(GO_CACHE):/root/.cache/go-build \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-w /go/src/github.com/aauren/rtorrent-exporter $(DOCKER_LINT_IMAGE) \
		bash -c \
		'golangci-lint run ./...'
	@echo Finished rtorrent-exporter linting in Docker
else
	@echo Starting rtorrent-exporter linting
	golangci-lint run ./...
	@echo Finished rtorrent-exporter linting
endif

# The race detector is implemented in cgo, which is why the tests run with CGO_ENABLED=1 while everything we actually ship stays at 0. A
# race is a bug like any other, so there's no reason to make catching one opt in.
test: gofmt ## Runs code quality pipelines (gofmt, tests, coverage, etc)
ifeq "$(BUILD_IN_DOCKER)" "true"
	@echo Starting rtorrent-exporter unit tests in Docker
	$(DOCKER) run -v $(PWD):/go/src/github.com/aauren/rtorrent-exporter \
		-v $(GO_CACHE):/root/.cache/go-build \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-w /go/src/github.com/aauren/rtorrent-exporter $(DOCKER_BUILD_IMAGE) \
		sh -c \
		'CGO_ENABLED=1 go test -v -race $(GO_TEST_FLAGS) -timeout 60s github.com/aauren/rtorrent-exporter/pkg/...'
	@echo Finished rtorrent-exporter unit tests in Docker
else
	@echo Starting rtorrent-exporter unit tests
	CGO_ENABLED=1 go test -v -race $(GO_TEST_FLAGS) -timeout 60s github.com/aauren/rtorrent-exporter/pkg/...
	@echo Finished rtorrent-exporter unit tests
endif

rtorrent-exporter:
ifeq "$(BUILD_IN_DOCKER)" "true"
	@echo Starting rtorrent-exporter build for $(GOARCH) on $(shell go env GOHOSTARCH) in Docker
	$(DOCKER) run -v $(PWD):/go/src/github.com/aauren/rtorrent-exporter \
		-v $(GO_CACHE):/root/.cache/go-build \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-w /go/src/github.com/aauren/rtorrent-exporter $(DOCKER_BUILD_IMAGE) \
		sh -c \
		'CGO_ENABLED=0 go build -v \
		-ldflags "-X github.com/aauren/rtorrent-exporter/cmd.Version=$(GIT_COMMIT) -X github.com/aauren/rtorrent-exporter/cmd.BuildDate=$(BUILD_DATE)" \
		-o rtorrent-exporter main.go'
	@echo Finished rtorrent-exporter build for $(GOARCH) on $(shell go env GOHOSTARCH) in Docker
else
	@echo Starting rtorrent-exporter build for $(GOARCH) on $(shell go env GOHOSTARCH)
	CGO_ENABLED=0 go build -v \
		-ldflags "-X github.com/aauren/rtorrent-exporter/cmd.Version=$(GIT_COMMIT) -X github.com/aauren/rtorrent-exporter/cmd.BuildDate=$(BUILD_DATE)" \
		-o rtorrent-exporter main.go
	@echo Finished rtorrent-exporter build for $(GOARCH) on $(shell go env GOHOSTARCH)
endif

container: clean rtorrent-exporter
	@echo Starting rtorrent-exporter container image build for $(GOARCH) on $(shell go env GOHOSTARCH)
	@if [ "$(GOARCH)" != "$(shell go env GOHOSTARCH)" ]; then \
		echo "Using qemu to build non-native container"; \
		$(DOCKER) run --rm --privileged $(QEMU_IMAGE) --reset -p yes; \
	fi
	$(DOCKER) build -t "$(REGISTRY_DEV):$(subst /,,$(IMG_TAG))" -f Dockerfile --build-arg ARCH="$(DOCKER_ARCH)" \
		--build-arg BUILDTIME_BASE="$(BUILDTIME_BASE)" --build-arg RUNTIME_BASE="$(RUNTIME_BASE)" .
	@echo Finished rtorrent-exporter container image build: $(REGISTRY_DEV):$(subst /,,$(IMG_TAG))
	@if [ "$(GIT_BRANCH)" = "master" ]; then \
		$(DOCKER) tag "$(REGISTRY_DEV):$(IMG_TAG)" "$(REGISTRY_DEV)"; \
		echo Added additional tag: $(REGISTRY_DEV); \
	fi

container-test-bundle: container
	@echo Starting rtorrent-exporter container image test bundle for $(GOARCH) on $(shell go env GOHOSTARCH)
	$(DOCKER) save -o "$(NAME):$(subst /,,$(IMG_TAG)).tar" "$(REGISTRY_DEV):$(subst /,,$(IMG_TAG))"
	@echo Finished rtorrent-exporter container image test bundle.

all: lint test rtorrent-exporter
