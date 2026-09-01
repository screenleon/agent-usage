PREFIX    ?= $(HOME)/.local
BIN       := bin/agent-usage
DIST      ?= dist
PKG       := ./cmd/agent-usage
AMD64_BIN := $(DIST)/agent-usage-linux-amd64
ARM64_BIN := $(DIST)/agent-usage-linux-arm64
WIN_AMD64_BIN := $(DIST)/agent-usage-windows-amd64.exe
WIN_ARM64_BIN := $(DIST)/agent-usage-windows-arm64.exe
GO_BUILD  := CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w"

.PHONY: build test test-go test-release install fmt release

build:
	mkdir -p bin
	$(GO_BUILD) -o $(BIN) $(PKG)

test: test-go test-release

test-go:
	go test ./...

test-release:
	bash tests/release_test.sh

fmt:
	gofmt -w cmd internal

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/agent-usage

release:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -o $(AMD64_BIN) $(PKG)
	GOOS=linux GOARCH=arm64 $(GO_BUILD) -o $(ARM64_BIN) $(PKG)
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -o $(WIN_AMD64_BIN) $(PKG)
	GOOS=windows GOARCH=arm64 $(GO_BUILD) -o $(WIN_ARM64_BIN) $(PKG)
	cd $(DIST) && sha256sum $(notdir $(AMD64_BIN)) $(notdir $(ARM64_BIN)) $(notdir $(WIN_AMD64_BIN)) $(notdir $(WIN_ARM64_BIN)) > SHA256SUMS
