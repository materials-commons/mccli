.PHONY: bin test all fmt cli clean

BINARY_NAME := mc2
CMD_PACKAGE := ./cmd/mc2
BIN_DIR := bin
BIN_PATH := $(BIN_DIR)/$(BINARY_NAME)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_TAG ?= $(shell git describe --tags --exact-match 2>/dev/null || true)
GIT_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GIT_DATE ?= $(shell git show -s --format=%cI HEAD 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_DIRTY ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo false || echo true)

LDFLAGS := \
	-X 'main.version=$(VERSION)' \
	-X 'main.gitTag=$(GIT_TAG)' \
	-X 'main.gitBranch=$(GIT_BRANCH)' \
	-X 'main.gitCommit=$(GIT_COMMIT)' \
	-X 'main.gitDate=$(GIT_DATE)' \
	-X 'main.gitDirty=$(GIT_DIRTY)'

all: fmt cli

fmt:
	go fmt ./...

test:
	go test ./...

bin:
	mkdir -p $(BIN_DIR)

cli: bin
	go build -ldflags "$(LDFLAGS)" -o $(BIN_PATH) $(CMD_PACKAGE)

clean:
	rm -rf $(BIN_DIR)