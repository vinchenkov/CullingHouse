#!/bin/bash
# P7: helper killed -> the host-side wrapper (mc stand-in) detects the dead
# helper and lazily recreates it; the next invocation succeeds.
# Covers both `docker kill` (container present but exited) and full removal.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0

ensure_helper
id_before=$(docker inspect -f '{{.Id}}' "$HELPER")

# Case A: docker kill (dead but still listed).
docker kill "$HELPER" >/dev/null
state=$(docker inspect -f '{{.State.Running}}' "$HELPER")
[ "$state" = "false" ] && pass "helper killed (state Running=false)" || fail "helper still running after kill"

out=$(mc_exec echo alive-after-kill); rc=$?
[ $rc -eq 0 ] && [ "$out" = "alive-after-kill" ] \
  && pass "next invocation lazily recreated helper and succeeded" \
  || fail "post-kill invocation failed rc=$rc out='$out'"

id_after=$(docker inspect -f '{{.Id}}' "$HELPER")
[ "$id_before" != "$id_after" ] && pass "helper is a NEW container (recreate, not revive)" \
                                 || fail "helper container id unchanged after recreate"

# Case B: helper removed entirely.
docker rm -f "$HELPER" >/dev/null
out=$(mc_exec echo alive-after-rm); rc=$?
[ $rc -eq 0 ] && [ "$out" = "alive-after-rm" ] \
  && pass "invocation after full removal recreated helper and succeeded" \
  || fail "post-rm invocation failed rc=$rc out='$out'"

# Sanity: exit-code fidelity still holds through the recreated helper.
mc_exec sh -c 'exit 7'; rc=$?
[ $rc -eq 7 ] && pass "recreated helper propagates exit codes (rc=7)" || fail "recreated helper rc=$rc != 7"

exit $FAILED
