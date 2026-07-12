#!/bin/sh
# Phase 1 fast-suite entrypoint for the Go side (phase1b-contract.md §8):
# substrate + dispatch + verb/CLI tests against temp spine files, no Docker.
# The Docker e2e is compiled only under -tags docker_e2e and never runs here.
# gofmt/vet cleanliness is part of the definition of done (AGENTS.md §3).
set -eu
cd "$(dirname "$0")"
fmt="$(mise exec -- gofmt -l .)"
if [ -n "$fmt" ]; then
    echo "gofmt needed on:" >&2
    echo "$fmt" >&2
    exit 1
fi
mise exec -- go vet ./...
exec mise exec -- go test ./...
