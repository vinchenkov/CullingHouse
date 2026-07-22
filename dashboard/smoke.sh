#!/bin/sh
# Dashboard smoke lane (ADR-024 D7): real server + real Chromium + real mc
# against a scratch spine — Docker-free, token-free. Not part of the fast
# suite; run at phase completion alongside the Docker lanes.
set -eu
cd "$(dirname "$0")"
mise exec -- bun install --frozen-lockfile
mise exec -- bunx playwright install chromium
SCRATCH="$(mktemp -d /private/tmp/mc-dash-smoke-bin.XXXXXX)"
trap 'rm -rf "$SCRATCH"' EXIT
(cd ../mc && mise exec -- go build -tags test_fake_routing -o "$SCRATCH/mc" ./cmd/mc)
MC_SMOKE_BIN="$SCRATCH/mc" mise exec -- bun test smoke/
