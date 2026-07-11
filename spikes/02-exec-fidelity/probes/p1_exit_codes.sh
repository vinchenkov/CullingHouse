#!/bin/bash
# P1: exit codes {0, 1, 126, 127, 137, signal-death} propagate exactly
# through non-TTY docker exec into the warm helper.
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
. "$DIR/lib.sh"
FAILED=0

check_rc() { # desc, expected, actual
  if [ "$3" -eq "$2" ]; then pass "$1 -> rc=$3 (expected $2)"; else fail "$1 -> rc=$3 (expected $2)"; fi
}

mc_exec sh -c 'exit 0'; check_rc "exit 0" 0 $?
mc_exec sh -c 'exit 1'; check_rc "exit 1" 1 $?

# 126: file exists but is not executable. Shell-in-container semantics.
mc_exec sh -c 'printf "#!/bin/sh\ntrue\n" > /tmp/nonexec && chmod 644 /tmp/nonexec'
mc_exec sh -c '/tmp/nonexec' 2>/dev/null; check_rc "not-executable via in-container shell" 126 $?
# 126 direct spawn: docker execs the file itself (no shell). Record what the
# CLI reports -- this is the shape mc's self-delegation will see.
mc_exec /tmp/nonexec 2>/dev/null; rc=$?
check_rc "not-executable direct exec" 126 $rc

mc_exec sh -c 'no_such_command_xyz' 2>/dev/null; check_rc "command-not-found via shell" 127 $?
mc_exec no_such_command_xyz 2>/dev/null; rc=$?
check_rc "command-not-found direct exec" 127 $rc

# 137: process inside the exec killed by SIGKILL (128+9).
mc_exec sh -c 'kill -9 $$'; check_rc "SIGKILL self (137)" 137 $?

# signal-death, generic: SIGTERM self -> 143 (128+15).
mc_exec sh -c 'kill -TERM $$'; check_rc "SIGTERM self (143)" 143 $?

# 137 by external kill: kill the exec'd process from a second exec.
# The in-container sh waits on a sleep; SIGKILLing the sleep makes wait
# return 137, which must cross the exec channel intact.
mc_exec sh -c 'sleep 60 & echo $! > /tmp/p1pid; wait $!' &
BGPID=$!
sleep 2
mc_exec sh -c 'kill -9 "$(cat /tmp/p1pid)"'
wait $BGPID; rc=$?
check_rc "external SIGKILL of exec'd process (137)" 137 $rc

exit $FAILED
