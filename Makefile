GO ?= go
GOFMT ?= gofmt
BINARY := bin/plc-reader
VERSION ?= dev
GO_FILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: fmt fmt-check test race vet build check

fmt:
	$(GOFMT) -w $(GO_FILES)

fmt-check:
	@test -z "$$($(GOFMT) -l $(GO_FILES))" || \
		{ echo "Go files need formatting; run 'make fmt'"; $(GOFMT) -l $(GO_FILES); exit 1; }

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/plc-reader

check: fmt-check test vet build
