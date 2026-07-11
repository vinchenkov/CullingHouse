#!/bin/sh
# Phase 1a substrate check — the definition-of-done command, exactly.
set -eu
cd "$(cd "$(dirname "$0")/.." && pwd)"
exec mise exec -- go test ./substrate/
