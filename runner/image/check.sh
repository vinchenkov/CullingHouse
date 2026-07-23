#!/bin/sh
# Docker-free fast tests for the baked landing boundary.
set -eu
cd "$(dirname "$0")"
sh build-prod.test.sh
exec mise exec -- bun test mc-land.test.ts
