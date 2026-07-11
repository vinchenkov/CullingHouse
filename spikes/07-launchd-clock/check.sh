#!/bin/sh
# S7 — launchd + clock drill: machine-rerunnable check.
#
# Loads a REAL LaunchAgent (label mcspike-07; the sanctioned spike exception
# to "no launchd during dev"), verifies ticks fire with the minimal launchd
# PATH, verifies Docker socket/context resolution from the launchd context,
# verifies VM-vs-host clock agreement, then boots the agent out, removes the
# plist, and verifies it is gone. The agent is ALWAYS removed, even on
# failure (trap). Exits nonzero on any failure.
#
# Requires: Docker Desktop RUNNING; image tag mcspike-05-base present
# (any local alpine tag works — override with S7_CLOCK_IMAGE).
#
# Opt-in destructive phase (quits and restarts Docker Desktop to observe
# backoff-and-retry + recovery):   S7_DRILL_DOCKER_QUIT=1 ./check.sh
set -u

SPIKE_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNTIME_DIR="$HOME/Library/Caches/mcspike-07"
PLIST_INSTALL="$HOME/Library/LaunchAgents/mcspike-07.plist"
UID_N="$(id -u)"
LOG="$RUNTIME_DIR/tick.log"
FAIL=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; FAIL=1; }
note() { echo; echo "== $*"; }

cleanup() {
  launchctl bootout "gui/$UID_N/mcspike-07" 2>/dev/null
  rm -f "$PLIST_INSTALL"
  # keep a copy of the tick log next to the spike for the record
  [ -f "$LOG" ] && cp "$LOG" "$SPIKE_DIR/tick.log" 2>/dev/null
}
trap cleanup EXIT

note "0. preflight"
plutil -lint "$SPIKE_DIR/mcspike-07.plist" >/dev/null || { fail "plist lint"; exit 1; }
pass "plist lints"
docker info >/dev/null 2>&1 || { fail "Docker Desktop must be running to start this check"; exit 1; }
pass "docker daemon reachable from the interactive shell"

note "1. stage runtime copy (TCC: launchd may NOT read ~/Documents — exit 126 otherwise)"
mkdir -p "$RUNTIME_DIR"
cp "$SPIKE_DIR/tick.sh" "$RUNTIME_DIR/tick.sh" && chmod 755 "$RUNTIME_DIR/tick.sh"
rm -f "$LOG" "$RUNTIME_DIR/launchd-stdout.log" "$RUNTIME_DIR/launchd-stderr.log"
pass "runtime copy staged in $RUNTIME_DIR"

note "2. bootstrap the LaunchAgent (label mcspike-07)"
launchctl bootout "gui/$UID_N/mcspike-07" 2>/dev/null  # tolerate leftovers
cp "$SPIKE_DIR/mcspike-07.plist" "$PLIST_INSTALL"
launchctl bootstrap "gui/$UID_N" "$PLIST_INSTALL" || { fail "launchctl bootstrap"; exit 1; }
pass "bootstrapped gui/$UID_N/mcspike-07"

note "3. ticks fire under launchd (RunAtLoad + StartInterval=20)"
i=0
while [ $i -lt 60 ]; do
  [ "$(grep -c 'tick pid' "$LOG" 2>/dev/null || echo 0)" -ge 2 ] && break
  sleep 2; i=$((i+1))
done
TICKS=$(grep -c 'tick pid' "$LOG" 2>/dev/null || echo 0)
if [ "$TICKS" -ge 2 ]; then pass "$TICKS ticks fired"; else fail "expected >=2 ticks, got $TICKS"; fi

note "4. minimal PATH (no user shell env)"
if grep -q 'PATH=/usr/bin:/bin:/usr/sbin:/sbin$' "$LOG"; then
  pass "ticks ran with PATH=/usr/bin:/bin:/usr/sbin:/sbin (docker resolved by absolute path)"
else
  fail "minimal PATH not observed in tick log"
fi

note "5. Docker socket/context resolution from the launchd context"
if grep -q 'resolve context=desktop-linux endpoint=unix://' "$LOG"; then
  pass "context+endpoint resolved: $(grep 'resolve context=' "$LOG" | tail -1 | sed 's/.*resolve //')"
else
  fail "context/endpoint resolution not observed"
fi
if grep -q 'docker OK server=' "$LOG"; then
  pass "daemon reachable from launchd tick: $(grep 'docker OK' "$LOG" | tail -1 | sed 's/.*docker //')"
else
  fail "no successful docker call from launchd tick"
fi

note "6. VM clock vs host clock agreement (bound = [container-host_after, container+1-host_before])"
SKEW_LINE=$(grep 'clock host_before=' "$LOG" | tail -1)
if [ -n "$SKEW_LINE" ]; then
  LO=$(echo "$SKEW_LINE" | sed 's/.*skew_bound=\[\(-*[0-9]*\),.*/\1/')
  HI=$(echo "$SKEW_LINE" | sed 's/.*skew_bound=\[-*[0-9]*,\(-*[0-9]*\)\].*/\1/')
  if [ "$LO" -ge -2 ] && [ "$HI" -le 2 ]; then
    pass "clocks agree within measurement bound [$LO,$HI]s"
  else
    fail "clock skew bound [$LO,$HI]s exceeds +/-2s — VM clock drift"
  fi
else
  fail "no clock sample in tick log"
fi

if [ "${S7_DRILL_DOCKER_QUIT:-0}" = "1" ]; then
  note "7. (opt-in) Docker quit -> backoff; Docker start -> recovery"
  osascript -e 'quit app "Docker"'
  i=0; while [ $i -lt 36 ] && docker info >/dev/null 2>&1; do sleep 5; i=$((i+1)); done
  BASE_DOWN=$(grep -c 'docker DOWN' "$LOG" 2>/dev/null || echo 0)
  i=0
  while [ $i -lt 60 ]; do
    [ "$(grep -c 'docker DOWN' "$LOG" 2>/dev/null || echo 0)" -ge $((BASE_DOWN + 4)) ] && break
    sleep 5; i=$((i+1))
  done
  if [ "$(grep -c 'docker DOWN' "$LOG")" -ge $((BASE_DOWN + 4)) ] \
     && grep -q 'backoff exhausted for this tick' "$LOG"; then
    pass "backoff-and-retry observed while daemon down (no crash-loop: launchd last exit stays 0)"
  else
    fail "backoff lines not observed while daemon down"
  fi
  open -a Docker
  i=0; while [ $i -lt 110 ]; do docker ps >/dev/null 2>&1 && break; sleep 5; i=$((i+1)); done
  MARK=$(wc -l < "$LOG")
  i=0
  while [ $i -lt 30 ]; do
    tail -n "+$MARK" "$LOG" | grep -q 'docker OK server=' && break
    sleep 5; i=$((i+1))
  done
  if tail -n "+$MARK" "$LOG" | grep -q 'docker OK server='; then
    pass "recovery observed after Docker restart"
  else
    fail "no recovery tick after Docker restart"
  fi
fi

note "8. bootout + removal (sanctioned agent MUST be gone afterwards)"
launchctl bootout "gui/$UID_N/mcspike-07" || fail "bootout failed"
rm -f "$PLIST_INSTALL"
if launchctl print "gui/$UID_N/mcspike-07" >/dev/null 2>&1; then
  fail "agent still loaded after bootout"
else
  pass "agent unloaded"
fi
if [ -e "$PLIST_INSTALL" ]; then fail "plist still installed"; else pass "plist removed from ~/Library/LaunchAgents"; fi

echo
[ "$FAIL" -eq 0 ] && echo "S7 CHECK: ALL PASS" || echo "S7 CHECK: FAILURES PRESENT"
exit "$FAIL"
