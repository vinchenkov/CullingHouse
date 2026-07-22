#!/bin/sh
# Dashboard fast lane: unit tests only — Docker-free, browser-free, no
# install step (the unit lane imports nothing from node_modules). The
# Playwright browser smoke is dashboard/smoke.sh, run at phase completion.
set -eu
cd "$(dirname "$0")"
exec mise exec -- bun test src/
