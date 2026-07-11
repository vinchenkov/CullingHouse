#!/bin/bash
# S2 exec-fidelity spike: end-to-end rerun of the whole probe matrix.
# Exits nonzero on any failure.
#
# Requires: Docker Desktop running (arm64, runc). Creates/uses:
#   image     mcspike-02-helper-img:latest  (a tag of alpine:3.22)
#   container mcspike-02-helper             (long-lived warm helper)
# Both are left IN PLACE afterwards for the restart drill.
#
# Runtime: ~2 min + STREAM_SECS (default 480 s for the P4 stream hold).
# For a quick smoke pass: STREAM_SECS=30 ./check.sh
# The full 30-min hold (STREAM_SECS=1800) is deferred to Phase 3.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
export HELPER="${HELPER:-mcspike-02-helper}"
export HELPER_IMG="${HELPER_IMG:-mcspike-02-helper-img:latest}"
export STREAM_SECS="${STREAM_SECS:-480}"
export OUT="$DIR/out"
mkdir -p "$OUT"

# Provision the helper image + container if absent (idempotent).
if ! docker image inspect "$HELPER_IMG" >/dev/null 2>&1; then
  docker pull alpine:3.22
  docker tag alpine:3.22 "$HELPER_IMG"
fi
. "$DIR/probes/lib.sh"
ensure_helper

overall=0
run_probe() {
  echo "=== $1 ==="
  "$DIR/probes/$1"
  local rc=$?
  if [ $rc -ne 0 ]; then overall=1; echo "*** $1 FAILED (rc=$rc)"; fi
  echo
}

run_probe p1_exit_codes.sh
run_probe p2_binary_roundtrip.sh
run_probe p3_demux.sh
run_probe p5_signals.sh
run_probe p6_concurrent.sh
run_probe p4_long_stream.sh      # long: holds a stream for STREAM_SECS
run_probe p7_kill_recreate.sh    # last: kills and recreates the helper

# Leave the helper warm for the drill.
ensure_helper

if [ $overall -eq 0 ]; then
  echo "S2 CHECK: ALL PROBES PASSED (P4 held ${STREAM_SECS}s; 30-min hold deferred to Phase 3; Docker-restart rewarm deferred to drill)"
else
  echo "S2 CHECK: FAILURES DETECTED"
fi
exit $overall
