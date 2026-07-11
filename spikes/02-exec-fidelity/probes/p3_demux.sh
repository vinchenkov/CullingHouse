#!/bin/bash
# P3: interleaved stdout/stderr demux through non-TTY docker exec.
# A non-TTY exec uses Docker's multiplexed stream framing; the CLI must
# demux it back into two uncorrupted, internally-ordered streams.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0
OUT="${OUT:-$DIR/../out}"
mkdir -p "$OUT"

N=20000
SO="$OUT/p3_stdout.txt"; SE="$OUT/p3_stderr.txt"

# Tight interleave: each iteration writes one line to each stream.
mc_exec sh -c "i=1; while [ \$i -le $N ]; do echo \"OUT \$i abcdefghij\"; echo \"ERR \$i 0123456789\" >&2; i=\$((i+1)); done" \
  > "$SO" 2> "$SE"
rc=$?
[ $rc -eq 0 ] && pass "interleave exec rc=0" || fail "interleave exec rc=$rc"

so_n=$(wc -l < "$SO" | tr -d ' '); se_n=$(wc -l < "$SE" | tr -d ' ')
[ "$so_n" = "$N" ] && pass "stdout line count $so_n" || fail "stdout line count $so_n != $N"
[ "$se_n" = "$N" ] && pass "stderr line count $se_n" || fail "stderr line count $se_n != $N"

# No cross-contamination and no torn lines.
grep -cq "ERR" "$SO" && fail "stderr bytes leaked into stdout" || pass "no ERR lines in stdout"
grep -cq "OUT" "$SE" && fail "stdout bytes leaked into stderr" || pass "no OUT lines in stderr"
bad_so=$(grep -vc '^OUT [0-9]* abcdefghij$' "$SO" | tr -d ' ')
bad_se=$(grep -vc '^ERR [0-9]* 0123456789$' "$SE" | tr -d ' ')
[ "$bad_so" = "0" ] && pass "stdout lines all well-formed" || fail "$bad_so malformed stdout lines"
[ "$bad_se" = "0" ] && pass "stderr lines all well-formed" || fail "$bad_se malformed stderr lines"

# Per-stream ordering: sequence numbers strictly ascending.
awk '{ if ($2 != NR) { print "gap at line " NR ": got " $2; exit 1 } }' "$SO" \
  && pass "stdout sequence 1..$N in order" || fail "stdout sequence broken"
awk '{ if ($2 != NR) { print "gap at line " NR ": got " $2; exit 1 } }' "$SE" \
  && pass "stderr sequence 1..$N in order" || fail "stderr sequence broken"

rm -f "$SO" "$SE"
exit $FAILED
