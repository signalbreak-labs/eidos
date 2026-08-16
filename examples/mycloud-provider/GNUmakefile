default: help

all: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

# Run go generate only when a tools/ directory is present. Eidos does not emit
# one, so on a fresh checkout this target is a no-op rather than a hard failure
# (M-9); users who add a tools/ directory for code generation get the usual
# behavior.
generate:
	@if [ -d tools ]; then cd tools && go generate ./...; fi

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

# help is the default target so a bare "make" prints the available targets
# instead of silently running fmt+lint+install+generate.
help:
	@echo "Eidos-generated Terraform provider Makefile"
	@echo ""
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  build      Build the provider (go build)"
	@echo "  install    Build and install the provider (go install)"
	@echo "  fmt        Format Go sources (gofmt -s)"
	@echo "  lint       Run golangci-lint"
	@echo "  generate   Run go generate (no-op without a tools/ dir)"
	@echo "  test       Run unit tests"
	@echo "  testacc    Run acceptance tests (requires TF_ACC=1)"
	@echo "  all        fmt, lint, install, and generate in one pass"

.PHONY: default all help build install fmt lint generate test testacc
