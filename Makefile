.PHONY: build test test-unit test-e2e lint lint-fix check clean install-lint app-bundle

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOLANGCI_LINT_VERSION ?= v2.8.0
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(DATE)

OUTPUT ?= dotpak

APP_NAME := Dotpak
BUNDLE_ID := dev.ospiem.dotpak

build:
	go build -ldflags "$(LDFLAGS)" -o $(OUTPUT) ./cmd/dotpak

test: build
	go test ./... -count=1

test-unit:
	go test ./internal/... -count=1

test-e2e: build
	go test ./tests/e2e/... -count=1

install-lint:
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: install-lint
	golangci-lint run

lint-fix: install-lint
	golangci-lint run --fix

check: lint test

define INFO_PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>dotpak</string>
    <key>CFBundleIdentifier</key>
    <string>$(BUNDLE_ID)</string>
    <key>CFBundleName</key>
    <string>$(APP_NAME)</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleVersion</key>
    <string>$(VERSION)</string>
    <key>CFBundleShortVersionString</key>
    <string>$(VERSION)</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
</dict>
</plist>
endef
export INFO_PLIST

app-bundle: build
	@echo "Creating macOS app bundle..."
	mkdir -p $(APP_NAME).app/Contents/MacOS
	cp $(OUTPUT) $(APP_NAME).app/Contents/MacOS/
	@echo "$$INFO_PLIST" > $(APP_NAME).app/Contents/Info.plist
	@echo "Created $(APP_NAME).app"

clean:
	rm -f dotpak coverage.out coverage.html
	rm -rf $(APP_NAME).app
	go clean -testcache
