#!/bin/bash
# P2: 8-bit-clean round-trip of ~50 MB random bytes stdin -> stdout
# through non-TTY docker exec (sha256 compare, byte count compare).
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0
OUT="${OUT:-$DIR/../out}"
mkdir -p "$OUT"

SRC="$OUT/p2_random.bin"
DST="$OUT/p2_roundtrip.bin"
SIZE=$((50 * 1024 * 1024))

head -c "$SIZE" /dev/urandom > "$SRC"

# Round-trip: host stdin -> exec cat -> host stdout.
mc_exec cat < "$SRC" > "$DST"
rc=$?
[ $rc -eq 0 ] && pass "exec cat rc=0" || fail "exec cat rc=$rc"

sb=$(wc -c < "$SRC" | tr -d ' '); db=$(wc -c < "$DST" | tr -d ' ')
[ "$sb" = "$db" ] && pass "byte count $db == $sb" || fail "byte count $db != $sb"

sh1=$(shasum -a 256 "$SRC" | awk '{print $1}')
sh2=$(shasum -a 256 "$DST" | awk '{print $1}')
[ "$sh1" = "$sh2" ] && pass "sha256 match $sh1" || fail "sha256 mismatch $sh1 != $sh2"

# Also verify in-container hash agrees (stdin leg alone is 8-bit clean).
sh3=$(mc_exec sha256sum - < "$SRC" | awk '{print $1}')
[ "$sh1" = "$sh3" ] && pass "in-container sha256 of stdin matches" || fail "in-container sha256 $sh3 != $sh1"

rm -f "$SRC" "$DST"
exit $FAILED
