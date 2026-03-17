# ────────────────────────────────────────────────────────────────────────────
# downwash — DJI drone post-flight analysis toolkit
# Makefile
# ────────────────────────────────────────────────────────────────────────────

BINARY   := downwash
CMD      := ./cmd/downwash
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.version=$(VERSION)

# Output directory for local cross-compiled binaries.
DIST := dist

.PHONY: all build install test test-integration lint vet tidy clean \
        cross-build sample release-snapshot

# ── default ──────────────────────────────────────────────────────────────────

## all: build the binary for the current platform
all: build

# ── local build ──────────────────────────────────────────────────────────────

## build: compile downwash for the current platform
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

## install: install downwash into ~/bin
install:
	go build -ldflags "$(LDFLAGS)" -o ~/bin/$(BINARY) $(CMD)

# ── tests ────────────────────────────────────────────────────────────────────

## test: run unit tests (no external tools required)
test:
	go test ./...

## test-verbose: run unit tests with verbose output
test-verbose:
	go test -v ./...

## test-integration: run integration tests that require exiftool and ffmpeg
##   (set env DJI_TEST_VIDEO to a real MP4 to exercise the full pipeline)
test-integration:
	go test -v -tags integration ./...

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

# ── dependencies ─────────────────────────────────────────────────────────────

## tidy: tidy go.mod and go.sum
tidy:
	go mod tidy

# ── cross-platform builds ────────────────────────────────────────────────────

## cross-build: build binaries for all supported platforms
cross-build: cross-darwin-arm64 cross-darwin-amd64 \
             cross-linux-amd64 \
             cross-windows-amd64 cross-windows-arm64

cross-darwin-arm64:
	GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 \
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)_darwin_arm64 $(CMD)

cross-darwin-amd64:
	GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 \
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)_darwin_amd64 $(CMD)

cross-linux-amd64:
	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 \
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)_linux_amd64 $(CMD)

cross-windows-amd64:
	GOOS=windows GOARCH=amd64  CGO_ENABLED=0 \
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)_windows_amd64.exe $(CMD)

cross-windows-arm64:
	GOOS=windows GOARCH=arm64  CGO_ENABLED=0 \
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)_windows_arm64.exe $(CMD)

# ── packaging (requires nfpm: https://nfpm.goreleaser.com) ───────────────────

## package-deb: build a .deb package for linux/amd64
package-deb: cross-linux-amd64
	nfpm package --config nfpm.yml --packager deb --target $(DIST)/

## package-rpm: build an .rpm package for linux/amd64
package-rpm: cross-linux-amd64
	nfpm package --config nfpm.yml --packager rpm --target $(DIST)/

# ── goreleaser ───────────────────────────────────────────────────────────────

## release-snapshot: dry-run full release with goreleaser (no tag required)
release-snapshot:
	goreleaser release --snapshot --clean

## release: publish a tagged release (requires GITHUB_TOKEN)
release:
	goreleaser release --clean

# ── sample generation ────────────────────────────────────────────────────────

## sample: regenerate sample artefacts in samples/
sample:
	go run ./samples/generate.go

# ── clean ────────────────────────────────────────────────────────────────────

## clean: remove build artefacts
clean:
	rm -rf $(DIST) $(BINARY)

# ── help ─────────────────────────────────────────────────────────────────────

## help: list all available targets with descriptions
help:
	@grep -E '^## ' Makefile | sed 's/^## /  /' | column -t -s ':'
