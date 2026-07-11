#!/usr/bin/env bash
# S6 — dispatch decision table: machine-rerunnable check.
# Re-runs the whole probe end-to-end; exits nonzero on any failure.
# No Docker required. Requires Go via mise (repo-root mise config).
set -euo pipefail
cd "$(dirname "$0")"

echo "== S6 check: gofmt =="
UNFORMATTED=$(mise exec -- gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "gofmt failures:" "$UNFORMATTED"
  exit 1
fi

echo "== S6 check: go vet =="
mise exec -- go vet ./...

echo "== S6 check: decision-table + property suites =="
mise exec -- go test ./... -count=1

echo "== S6 check: GREEN =="
