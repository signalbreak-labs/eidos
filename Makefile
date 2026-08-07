# Makefile for eidos — a Go CLI that generates Terraform providers from OpenAPI specs.
#
# Default target: `make` (or `make help`) prints a color-coded help window.
# Commands mirror the canonical recipes in AGENTS.md.

.DEFAULT_GOAL := help

# ----------------------------------------------------------------------------
# Variables
# ----------------------------------------------------------------------------
BINARY       := eidos
MAIN_PKG     := ./cmd/eidos
COVERAGE_OUT := coverage.out
GO           := go
SPECS_DIR    := test/specs
MYCLOUD_SPEC:= $(SPECS_DIR)/mycloud.yaml

# Output directory for `make generate`. Override with: make generate OUT=/tmp/eidos
OUT          ?= ./generated

# Color codes (disabled when stdout is not a TTY to keep logs clean in CI).
ifeq ($(shell tty -s 2>/dev/null; echo $$?),0)
	C_RESET := \033[0m
	C_TITLE := \033[1;36m
	C_HELP  := \033[36m
	C_BUILD := \033[32m
	C_TEST  := \033[33m
	C_LINT  := \033[35m
	C_GEN   := \033[34m
	C_RUN   := \033[36m
	C_MUTE  := \033[2m
else
	C_RESET :=
	C_TITLE :=
	C_HELP  :=
	C_BUILD :=
	C_TEST  :=
	C_LINT  :=
	C_GEN   :=
	C_RUN   :=
	C_MUTE  :=
endif

# ----------------------------------------------------------------------------
# Help
# ----------------------------------------------------------------------------
# Target comment format: `## <group>: <description>` — grouped & color-coded.
# Groups are emitted in the order they first appear in the Makefile.
.PHONY: help
help: ## help: Show this help window
	@printf "$(C_TITLE)%s$(C_RESET)\n" "eidos — Makefile targets"
	@printf "$(C_MUTE)%s$(C_RESET)\n\n" "Run 'make <target>'. Default target is 'help'."
	@awk -v RST="$(C_RESET)" -v HLP="$(C_HELP)" -v BLD="$(C_BUILD)" -v TST="$(C_TEST)" -v LNT="$(C_LINT)" -v GEN="$(C_GEN)" -v RUN="$(C_RUN)" \
	'BEGIN{color["help"]=HLP;color["build"]=BLD;color["test"]=TST;color["lint"]=LNT;color["gen"]=GEN;color["run"]=RUN} function pad(s,n,i,o){o=s;for(i=length(s)+1;i<=n;i++)o=o" ";return o} /^[a-zA-Z0-9_.-]+:.*##/{t=$$0;sub(/:.*/,"",t);d=$$0;sub(/^[^#]*## /,"",d);g=d;sub(/:.*/,"",g);sub(g": ","",d);if(!(g in color))g="help";if(!(g in seen)){seen[g]=1;order[++n]=g;}rows[g]=rows[g] sprintf("  %s%s%s  %s\n",color[g],pad(t,18),RST,d)} END{for(i=1;i<=n;i++){g=order[i];printf "%s=== %s ===%s\n",color[g],toupper(g),RST;printf "%s",rows[g];}}' $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# Build
# ----------------------------------------------------------------------------
.PHONY: build
build: ## build: Compile the eidos binary into ./eidos
	@printf "$(C_BUILD)▸ building %s$(C_RESET)\n" "$(BINARY)"
	@$(GO) build -o $(BINARY) $(MAIN_PKG)

.PHONY: install
install: ## build: Install the eidos binary to $$GOPATH/bin
	@printf "$(C_BUILD)▸ installing %s to GOPATH/bin$(C_RESET)\n" "$(BINARY)"
	@$(GO) install $(MAIN_PKG)

.PHONY: clean
clean: ## build: Remove the compiled binary and coverage file
	@printf "$(C_BUILD)▸ removing build artifacts$(C_RESET)\n"
	@rm -f $(BINARY) $(COVERAGE_OUT)

# ----------------------------------------------------------------------------
# Test
# ----------------------------------------------------------------------------
.PHONY: test
test: ## test: Run all tests (go test ./...)
	@printf "$(C_TEST)▸ running tests$(C_RESET)\n"
	@$(GO) test ./...

.PHONY: test-race
test-race: ## test: Run tests with race detection + coverage (matches CI)
	@printf "$(C_TEST)▸ running tests with -race -coverprofile$(C_RESET)\n"
	@$(GO) test -v -race -coverprofile=$(COVERAGE_OUT) ./...

.PHONY: test-gen
test-gen: ## test: Run generator golden-file tests only
	@printf "$(C_TEST)▸ running generator golden tests$(C_RESET)\n"
	@$(GO) test -run TestGoldenFiles ./pkg/generator

.PHONY: test-mycloud
test-mycloud: ## test: Run the mycloud golden subtest only
	@printf "$(C_TEST)▸ running mycloud golden subtest$(C_RESET)\n"
	@$(GO) test -run 'TestGoldenFiles/mycloud' ./pkg/generator

.PHONY: golden-update
golden-update: ## test: Regenerate checked-in golden snapshots (EIDOS_UPDATE_GOLDEN=1)
	@printf "$(C_TEST)▸ regenerating golden snapshots$(C_RESET)\n"
	@EIDOS_UPDATE_GOLDEN=1 $(GO) test -run TestGoldenFiles ./pkg/generator

.PHONY: cover
cover: test-race ## test: Run tests with coverage then open the HTML report
	@printf "$(C_TEST)▸ opening coverage report$(C_RESET)\n"
	@$(GO) tool cover -html=$(COVERAGE_OUT)

# ----------------------------------------------------------------------------
# Lint / format
# ----------------------------------------------------------------------------
.PHONY: lint
lint: ## lint: Run golangci-lint v2 across the repo
	@printf "$(C_LINT)▸ running golangci-lint$(C_RESET)\n"
	@golangci-lint run ./...

.PHONY: fmt
fmt: ## lint: Apply gofmt to all Go sources
	@printf "$(C_LINT)▸ running gofmt$(C_RESET)\n"
	@gofmt -w .

.PHONY: vet
vet: ## lint: Run go vet across the repo
	@printf "$(C_LINT)▸ running go vet$(C_RESET)\n"
	@$(GO) vet ./...

# ----------------------------------------------------------------------------
# Generate (eidos CLI)
# ----------------------------------------------------------------------------
.PHONY: generate
generate: build ## gen: Run eidos generate --output $$OUT (default ./generated)
	@printf "$(C_GEN)▸ generating provider into %s$(C_RESET)\n" "$(OUT)"
	@./$(BINARY) generate --spec $(MYCLOUD_SPEC) --output $(OUT)

.PHONY: dry-run
dry-run: build ## gen: Run eidos generate --dry-run against the mycloud spec
	@printf "$(C_GEN)▸ dry-run generate against %s$(C_RESET)\n" "$(MYCLOUD_SPEC)"
	@./$(BINARY) generate --spec $(MYCLOUD_SPEC) --dry-run

.PHONY: gen-config
gen-config: build ## gen: Emit a generator.yaml for the mycloud spec
	@printf "$(C_GEN)▸ emitting generator.yaml$(C_RESET)\n"
	@./$(BINARY) generate-config --spec $(MYCLOUD_SPEC) --output mycloud-generator.yaml

.PHONY: examples
examples: build ## gen: Regenerate the sample generated provider (examples/mycloud-provider)
	@printf "$(C_GEN)▸ regenerating sample provider into examples/mycloud-provider$(C_RESET)\n"
	@rm -rf examples/mycloud-provider
	@./$(BINARY) generate --spec $(MYCLOUD_SPEC) --output examples/mycloud-provider

# ----------------------------------------------------------------------------
# Run (local servers)
# ----------------------------------------------------------------------------
.PHONY: api
api: build ## run: Serve the eidos HTTP API on :8080
	@printf "$(C_RUN)▸ serving HTTP API on :8080$(C_RESET)\n"
	@./$(BINARY) api --port 8080

.PHONY: mcp
mcp: build ## run: Run the eidos MCP server over stdio
	@printf "$(C_RUN)▸ running MCP server over stdio$(C_RESET)\n"
	@./$(BINARY) mcp

# ----------------------------------------------------------------------------
# Release
# ----------------------------------------------------------------------------
.PHONY: release-snapshot
release-snapshot: ## build: Build a local snapshot via goreleaser (skip publish)
	@printf "$(C_BUILD)▸ goreleaser snapshot (no publish)$(C_RESET)\n"
	@goreleaser release --snapshot --clean --skip=publish