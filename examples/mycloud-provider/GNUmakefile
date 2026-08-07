default: fmt lint install generate

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

.PHONY: fmt lint test testacc build install generate
