#!/bin/sh
# Resident fast check. Docker-free by construction: unit tests use fake
# timer/exec/fs dependencies, while split-brain acceptance uses temp host
# state plus the real test-tagged mc binary through the same injected seams.
set -eu
cd "$(cd "$(dirname "$0")" && pwd)"
exec mise exec -- bun test
