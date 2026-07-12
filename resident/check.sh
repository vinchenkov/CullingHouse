#!/bin/sh
# Phase 1b resident check — the definition-of-done command, exactly.
# Docker-free by construction: the suite runs on fake timer/exec/fs only.
set -eu
cd "$(cd "$(dirname "$0")" && pwd)"
exec mise exec -- bun test
