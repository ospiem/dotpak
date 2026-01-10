.PHONY: build test test-unit test-e2e lint lint-fix check clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(DATE)

OUTPUT ?= dotpak

build:
	go build -ldflags "$(LDFLAGS)" -o $(OUTPUT) ./cmd/dotpak

test: build
	go test ./... -count=1

test-unit:
	go test ./internal/... -count=1

test-e2e: build
	go test ./tests/e2e/... -count=1

lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

check: lint test

clean:
	rm -f dotpak coverage.out coverage.html
	go clean -testcache
