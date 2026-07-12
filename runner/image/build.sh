#!/usr/bin/env bash
# build.sh — builds the linux/arm64 mc (CGO_ENABLED=0, pure Go — §11.2
# "baked, never bind-mounted") and the mc-fake-e2e image (contract §1).
# Native arm64, no --platform (AGENTS.md env facts). Idempotent: Docker's
# build cache makes reruns cheap; the e2e calls this once per run.
set -euo pipefail
cd "$(dirname "$0")"

echo "build.sh: compiling linux/arm64 mc..." >&2
(cd ../../mc && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 mise exec -- go build -o ../runner/image/mc ./cmd/mc)

echo "build.sh: docker build -t mc-fake-e2e ..." >&2
docker build -t mc-fake-e2e .
