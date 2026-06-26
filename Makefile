.PHONY: help test build install

BIN ?= $(HOME)/.local/bin

help:
	@echo "Targets:"
	@echo "  test     Run browserauth unit tests"
	@echo "  build    Build browserauth binary"
	@echo "  install  Install browserauth to $(BIN)"

test:
	go test ./...

build:
	go build -o browserauth .

install: build
	install -d $(BIN)
	install -m 755 browserauth $(BIN)/browserauth
