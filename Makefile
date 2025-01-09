.DEFAULT_GOAL := all
BUILD_DATE?=$(shell date +%Y-%m-%dT%H:%M:%S%z)
GIT_COMMIT=$(shell git describe --tags --dirty)
BUILD_IN_DOCKER?=true
GO_TEST_NO_CACHE?=false
GO_TEST_FLAGS?=$(if $(GO_TEST_NO_CACHE),-count=1)
IS_ROOT=$(filter 0,$(shell id -u))
IN_DOCKER_GROUP=$(filter docker,$(shell groups))
DOCKER=$(if $(or $(IN_DOCKER_GROUP),$(IS_ROOT),$(OSX)),docker,sudo docker)
GO_MOD_CACHE?=$(shell go env GOMODCACHE)
GO_CACHE?=$(shell go env GOCACHE)
DOCKER_LINT_IMAGE?=golangci/golangci-lint:v1.63.4
DOCKER_BUILD_IMAGE?=golang:1.23.4-alpine3.21

.PHONY: all clean test lint genmoqs instdeps gofmt gofmt-fix

clean:
	rm -f rtorrent-exporter

gofmt:
	@gofmt -l -s $(shell find . -not \( \( -wholename '*/vendor/*' \) -prune \) -name '*.go')

gofmt-fix:
	goimports -w $(shell find . -not \( \( -wholename '*/vendor/*' \) -prune \) -name '*.go')
	gofmt -s -w $(shell find . -not \( \( -wholename '*/vendor/*' \) -prune \) -name '*.go')

instdeps:
	go get ./...

updatedeps:
	go get -u ./...

lint: gofmt
ifeq "$(BUILD_IN_DOCKER)" "true"
	$(DOCKER) run -v $(PWD):/go/src/github.com/aauren/rtorrent-exporter \
		-v $(GO_CACHE):/root/.cache/go-build \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-w /go/src/github.com/aauren/rtorrent-exporter $(DOCKER_LINT_IMAGE) \
		bash -c \
		'golangci-lint run ./...'
else
	golangci-lint run ./...
endif

test: gofmt ## Runs code quality pipelines (gofmt, tests, coverage, etc)
ifeq "$(BUILD_IN_DOCKER)" "true"
	$(DOCKER) run -v $(PWD):/go/src/github.com/aauren/rtorrent-exporter \
		-v $(GO_CACHE):/root/.cache/go-build \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-w /go/src/github.com/aauren/rtorrent-exporter $(DOCKER_BUILD_IMAGE) \
		sh -c \
		'CGO_ENABLED=0 go test -v $(GO_TEST_FLAGS) -timeout 30s github.com/aauren/rtorrent-exporter/pkg/...'
else
	CGO_ENABLED=0 go test -v $(GO_TEST_FLAGS) -timeout 30s github.com/aauren/rtorrent-exporter/pkg/...
endif

rtorrent-exporter:
ifeq "$(BUILD_IN_DOCKER)" "true"
	$(DOCKER) run -v $(PWD):/go/src/github.com/aauren/rtorrent-exporter \
		-v $(GO_CACHE):/root/.cache/go-build \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-w /go/src/github.com/aauren/rtorrent-exporter $(DOCKER_BUILD_IMAGE) \
		sh -c \
		'CGO_ENABLED=0 go build -v \
		-ldflags "-X github.com/aauren/rtorrent-exporter/cmd.Version=$(GIT_COMMIT) -X github.com/aauren/rtorrent-exporter/cmd.BuildDate=$(BUILD_DATE)" \
		-o rtorrent-exporter main.go'
else
	CGO_ENABLED=0 go build -v \
		-ldflags "-X github.com/aauren/rtorrent-exporter/cmd.Version=$(GIT_COMMIT) -X github.com/aauren/rtorrent-exporter/cmd.BuildDate=$(BUILD_DATE)" \
		-o rtorrent-exporter main.go
endif

all: lint test rtorrent-exporter
