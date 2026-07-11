#!/bin/bash
# P6: N=5 concurrent non-TTY execs through the one warm helper.
# Each lane round-trips 5 MB of distinct random bytes AND emits tagged
# text; verify per-lane hash fidelity, rc=0, and zero cross-lane bleed.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0
OUT="${OUT:-$DIR/../out}"
mkdir -p "$OUT"

N=5
SIZE=$((5 * 1024 * 1024))
pids=()

for i in $(seq 1 $N); do
  head -c "$SIZE" /dev/urandom > "$OUT/p6_src_$i.bin"
  (
    docker exec -i "$HELPER" sh -c "echo LANE-$i-BEGIN; cat; echo; echo LANE-$i-END" \
      < "$OUT/p6_src_$i.bin" > "$OUT/p6_dst_$i.bin"
    echo $? > "$OUT/p6_rc_$i"
  ) &
  pids+=($!)
done
for p in "${pids[@]}"; do wait "$p"; done

for i in $(seq 1 $N); do
  rc=$(cat "$OUT/p6_rc_$i")
  [ "$rc" = "0" ] && pass "lane $i rc=0" || fail "lane $i rc=$rc"

  head -n1 "$OUT/p6_dst_$i.bin" | grep -q "^LANE-$i-BEGIN$" \
    && pass "lane $i header intact" || fail "lane $i header corrupt/missing"
  tail -n1 "$OUT/p6_dst_$i.bin" | grep -q "^LANE-$i-END$" \
    && pass "lane $i trailer intact" || fail "lane $i trailer corrupt/missing"

  # No other lane's tag may appear in this lane's capture.
  for j in $(seq 1 $N); do
    [ "$j" = "$i" ] && continue
    if grep -q "LANE-$j-" "$OUT/p6_dst_$i.bin" 2>/dev/null; then
      fail "lane $i contains lane $j bytes (stream cross-bleed)"
    fi
  done

  # Payload fidelity: strip header line and trailing "\nLANE-i-END\n".
  want=$(shasum -a 256 "$OUT/p6_src_$i.bin" | awk '{print $1}')
  got=$(tail -c +$(( $(head -n1 "$OUT/p6_dst_$i.bin" | wc -c) + 1 )) "$OUT/p6_dst_$i.bin" \
        | head -c "$SIZE" | shasum -a 256 | awk '{print $1}')
  [ "$want" = "$got" ] && pass "lane $i payload sha256 match" || fail "lane $i payload sha256 mismatch"
done

rm -f "$OUT"/p6_src_*.bin "$OUT"/p6_dst_*.bin "$OUT"/p6_rc_*
exit $FAILED
