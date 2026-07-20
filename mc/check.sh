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

# Same argument, one axis over: the fast lane runs on darwin, but the mc that
# opens the spine for real runs on linux. The lock-domain guard
# (substrate/lockdomain_linux.go, Inv. 24) therefore has its ONLY production
# implementation on a platform this suite never compiles by default — the
# invisible-rot shape the tagged vets above exist to prevent.
mise exec -- env GOOS=linux GOARCH=arm64 go vet ./...

# The ADR-021 D10a derivation guard reads docs/adr/017-mount-authorization.md at
# run time, so that a destination row added to ADR-017 fails a test instead of
# failing a container launch. Go's test cache does NOT track that read — the ADR
# lives outside this module — so `go test ./...` reports "ok (cached)" after
# ADR-017 changes and the guard never runs. Measured, not assumed: mutating the
# ADR's table left the cached PASS in place, while -count=1 caught it.
#
# A guard against silent drift that is itself silently skipped is worth less
# than no guard, because it also buys false confidence. Force it to run. It is
# one small package-local test; the rest of the suite keeps its cache.
mise exec -- go test -count=1 ./boundary/ -run 'TestTypedKindCoversEveryADR017Row|TestNoOrphanTypedKind|TestNoPhantomTableRows'

exec mise exec -- go test ./...
