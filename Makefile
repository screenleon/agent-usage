PREFIX    ?= $(HOME)/.local
BIN       := bin/agent-usage
DIST      := dist
PKG       := ./cmd/agent-usage
AMD64_BIN := $(DIST)/agent-usage-linux-amd64
ARM64_BIN := $(DIST)/agent-usage-linux-arm64
GO_BUILD  := CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w"

.PHONY: build test install fmt release

build:
	mkdir -p bin
	$(GO_BUILD) -o $(BIN) $(PKG)

test:
	go test ./...

fmt:
	gofmt -w cmd internal

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/agent-usage

release:
	mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 $(GO_BUILD) -o $(AMD64_BIN) $(PKG)
	GOOS=linux GOARCH=arm64 $(GO_BUILD) -o $(ARM64_BIN) $(PKG)
	cd $(DIST) && sha256sum $(notdir $(AMD64_BIN)) $(notdir $(ARM64_BIN)) > SHA256SUMS
