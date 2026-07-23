#!/usr/bin/env bash
# build-prod.sh — the PRODUCTION image (contract §8: "the production image
# contains no fake route and has its own untagged build").
#
# It differs from build.sh in two closed ways: the mc binary omits
# `test_fake_routing`, and RUNTIME_DEPS installs the exact real Codex and
# Claude-SDK packages pinned by runner/agent-runner/bun.lock.
#
# Native arm64, no --platform (AGENTS.md env facts). The base layers are shared
# with mc-fake-e2e, so a rerun after that build is cheap.
set -euo pipefail
cd "$(dirname "$0")"

echo "build-prod.sh: compiling linux/arm64 mc WITHOUT test_fake_routing..." >&2
(cd ../../mc && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 mise exec -- go build -o ../runner/image/mc-prod-bin ./cmd/mc)
(cd ../.. && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 mise exec -- go build -o runner/image/mc-dispatch ./runner/image/mc_dispatch.go)
(cd ../.. && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 mise exec -- go build -o runner/image/mc-complete ./runner/image/mc_completion_wrapper.go)

echo "build-prod.sh: docker build -t mc-prod ..." >&2
docker build -f Dockerfile \
  --build-arg MC_BIN=mc-prod-bin \
  --build-arg RUNTIME_DEPS=1 \
  -t mc-prod ../..
