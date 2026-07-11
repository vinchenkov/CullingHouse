#!/bin/bash
# P4: long-lived exec stream (`mc history --follow` shape).
# Hold a continuously-emitting non-TTY exec stream for STREAM_SECS
# (default 480 s = 8 min) and verify no drop, no gap, no corruption.
# The full 30-minute hold is DEFERRED to the Phase 3 promoted suite.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0
OUT="${OUT:-$DIR/../out}"
mkdir -p "$OUT"

SECS="${STREAM_SECS:-480}"
LOG="$OUT/p4_stream.txt"

start=$(date +%s)
# Emitter: one sequenced, fixed-payload line every ~50 ms until the deadline.
# timeout(1) is busybox's; it SIGTERMs the shell, we treat 143/124 as clean end.
mc_exec sh -c "end=\$(( \$(date +%s) + $SECS )); i=1; while [ \$(date +%s) -lt \$end ]; do echo \"SEQ \$i PAYLOAD-0123456789abcdef\"; i=\$((i+1)); sleep 0.05; done; echo \"DONE \$((i-1))\"" > "$LOG"
rc=$?
elapsed=$(( $(date +%s) - start ))

[ $rc -eq 0 ] && pass "stream exec ended cleanly rc=0 after ${elapsed}s" || fail "stream exec rc=$rc after ${elapsed}s"
[ $elapsed -ge $(( SECS - 5 )) ] && pass "stream held >= $((SECS-5))s" || fail "stream ended early: ${elapsed}s < ${SECS}s"

total=$(grep -c '^SEQ ' "$LOG" | tr -d ' ')
done_n=$(awk '/^DONE /{print $2}' "$LOG")
[ -n "$done_n" ] && pass "DONE marker present (emitter reached deadline)" || fail "no DONE marker (stream truncated)"
[ "$total" = "${done_n:-!}" ] && pass "received all $total emitted lines (DONE=$done_n)" || fail "received $total lines but emitter wrote ${done_n:-?}"

bad=$(grep -c -v -e '^SEQ [0-9]* PAYLOAD-0123456789abcdef$' -e '^DONE [0-9]*$' "$LOG" | tr -d ' ')
[ "$bad" = "0" ] && pass "no malformed/torn lines" || fail "$bad malformed lines"

awk '/^SEQ /{ if ($2 != ++n) { print "gap: expected " n " got " $2; exit 1 } }' "$LOG" \
  && pass "sequence contiguous 1..$total, no gaps" || fail "sequence gap detected"

rm -f "$LOG"
exit $FAILED
