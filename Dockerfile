# syntax=docker/dockerfile:1
#
# Eidos CI image: bundles the eidos generator, the Go toolchain, and GoReleaser
# in a single container so a CI job can turn an OpenAPI spec into a published
# Terraform provider end to end — generate the provider, build it, and release
# it — without installing anything on the runner.
#
# This image is for the eidos *generator* (the tool), not for generated
# providers. Generated providers are normal Go modules that GoReleaser
# cross-compiles into Terraform plugin binaries via their own generated
# .goreleaser.yml; this image supplies the tools (eidos + go + goreleaser) to
# drive that pipeline.
#
# Built and pushed to ghcr.io/signalbreak-labs/eidos by the Release workflow on
# every v* tag. Multi-arch (linux/amd64, linux/arm64) via buildx; the Go build
# step cross-compiles natively per platform, so it does not fall back to slow
# QEMU emulation for the compile.

# --- builder: compile the eidos binary from source ---
FROM golang:1.26-bookworm AS builder

# buildx sets TARGETOS/TARGETARCH per platform; Go cross-compiles natively, so a
# linux/arm64 image builds on an amd64 runner without emulating the Go toolchain.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
# VERSION is the release tag (vX.Y.Z) passed via --build-arg by the Release
# workflow so `eidos --version` reports the release. Defaults to dev for local builds.
ARG VERSION=dev

WORKDIR /src
# Cache module downloads separately from the source copy for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/eidos ./cmd/eidos

# --- runtime: Go toolchain + GoReleaser + eidos ---
FROM golang:1.26-bookworm AS runtime

# GoReleaser version is pinned to match the Release workflow's
# goreleaser-action version, so the image's goreleaser matches what eidos
# generates and what CI expects. Bump both together (see .github/workflows/release.yml).
ARG GORELEASER_VERSION=v2.17.0

# make: the generated GNUmakefile targets (build/lint/install/generate) call make.
# curl+ca-certificates: the GoReleaser install script fetches the binary.
# git is already present in the golang base image (GoReleaser needs a repo).
RUN apt-get update \
 && apt-get install -y --no-install-recommends make curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Install GoReleaser to /usr/local/bin via the official install script,
# version-pinned. Detects the platform's arch automatically.
RUN curl -sfL https://goreleaser.com/static/install | VERSION=${GORELEASER_VERSION} sh

COPY --from=builder /out/eidos /usr/local/bin/eidos

# A clean workspace for generated output; mount/spec your own at run time.
WORKDIR /workspace
# No ENTRYPOINT: this image is meant to run as a GitHub Actions `container:`,
# where the runner overrides the command to keep the container alive and execs
# each `run:` step inside it. An exec-form ENTRYPOINT would wrap that override as
# args to eidos and break the job. Invoke eidos explicitly on PATH, e.g.
# `docker run ghcr.io/signalbreak-labs/eidos eidos generate ...`.
CMD ["eidos", "--help"]