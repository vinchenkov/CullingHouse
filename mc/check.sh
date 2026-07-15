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

# The tagged suites do not RUN here (nightly is slow; docker_e2e needs Docker),
# but they must still COMPILE on every commit. Without this, a signature change
# rots a tagged suite invisibly while the fast lane stays green — which is
# exactly how ADR-020's domain.Cancel actor parameter broke the nightly
# lattice walk and went unnoticed until an adversarial review found it.
# vet is cheap and needs neither Docker nor a nightly budget.
for tag in nightly docker_e2e test_fake_routing; do
    mise exec -- go vet -tags "$tag" ./...
done

exec mise exec -- go test ./...
