PREFIX ?= $(HOME)/.local
BIN    := bin/agent-usage

.PHONY: build test install fmt

build:
	mkdir -p bin
	go build -buildvcs=false -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/agent-usage

test:
	go test ./...

fmt:
	gofmt -w cmd internal

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/agent-usage
