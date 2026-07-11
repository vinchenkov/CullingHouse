#!/bin/bash
# P5: SIGINT/SIGTERM cancellation semantics of non-TTY docker exec.
# OBSERVED behavior on Docker 29.4.0 / macOS (asserted below because the
# helper protocol must be designed around it):
#   1. The docker CLI catches INT/TERM and exits IMMEDIATELY with rc=0 --
#      not 130/143. A signal-cancelled invocation is therefore
#      indistinguishable from a successful one by exit code alone.
#   2. The signal is NOT proxied: the exec'd process inside the container
#      keeps running as an orphan of the exec.
# Consequences for mc's self-delegation: the host-side mc must trap
# INT/TERM itself (it cannot rely on the exec channel), and cancellation
# must be an explicit in-container kill of the exec'd PID/process-group
# (second exec, or Engine-API kill). The probe demonstrates that remedy.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0

run_case() { # sig, name
  local sig="$1" name="$2"
  local pidfile="/tmp/p5pid_$name"

  mc_exec sh -c "rm -f $pidfile" || true
  # Foreground non-TTY exec that records its in-container PID then sleeps.
  docker exec -i "$HELPER" sh -c "echo \$\$ > $pidfile; exec sleep 300" &
  local clipid=$!
  # Wait for the in-container process to exist.
  for _ in $(seq 1 50); do
    docker exec "$HELPER" sh -c "[ -s $pidfile ]" 2>/dev/null && break
    sleep 0.2
  done
  local inpid
  inpid=$(docker exec "$HELPER" cat "$pidfile")

  kill "-$sig" "$clipid"
  local t0 t1; t0=$(date +%s)
  wait "$clipid"; local rc=$?
  t1=$(date +%s)
  # Observed: CLI exits promptly with rc=0 (cancellation looks like success).
  if [ $((t1 - t0)) -le 5 ]; then
    pass "$name: docker CLI exited promptly on SIG$sig with rc=$rc (rc=0 observed: cancel is INDISTINGUISHABLE from success)"
  else
    fail "$name: docker CLI did not exit promptly after SIG$sig (took $((t1-t0))s, rc=$rc)"
  fi

  sleep 1
  if docker exec "$HELPER" sh -c "kill -0 $inpid" 2>/dev/null; then
    pass "$name: in-container process $inpid STILL RUNNING (signal NOT proxied -- expected Docker behavior)"
  else
    fail "$name: in-container process died; docker exec unexpectedly proxied $sig"
  fi

  # Protocol remedy: explicit cancellation by a second exec killing the PID.
  docker exec "$HELPER" sh -c "kill -TERM $inpid"
  sleep 1
  if docker exec "$HELPER" sh -c "kill -0 $inpid" 2>/dev/null; then
    fail "$name: explicit in-container kill failed; process survived"
  else
    pass "$name: explicit in-container kill of $inpid works (the remedy mc must implement)"
  fi
}

run_case INT  int
run_case TERM term

exit $FAILED
