#!/bin/sh
# mcspike-07 tick probe — runs under launchd (LaunchAgent, label mcspike-07)
# with the minimal launchd PATH (/usr/bin:/bin:/usr/sbin:/sbin — no user
# shell env, no /usr/local/bin). Everything Docker-related is therefore
# resolved EXPLICITLY, the way mc must under launchd:
#   - docker CLI at its absolute install path (Docker Desktop symlink target)
#   - socket/context read from `docker context` (falls back to the
#     well-known Docker Desktop user socket path)
# One invocation = one "tick". Never exits nonzero (launchd must not see a
# crash; a Docker outage is an expected state, handled by bounded
# exponential backoff inside the tick and by the next StartInterval tick).

# NOTE (finding): launchd could NOT read this script from its home under
# ~/Documents — macOS TCC denies LaunchAgents access to Documents/Desktop/
# Downloads ("Operation not permitted", exit 126). The runtime copy therefore
# lives in a TCC-free path (~/Library/Caches/mcspike-07) and logs there too.
RUNTIME_DIR="$HOME/Library/Caches/mcspike-07"
LOG="$RUNTIME_DIR/tick.log"
DOCKER_BIN=/usr/local/bin/docker
FALLBACK_SOCK="$HOME/.docker/run/docker.sock"

ts() { /bin/date -u '+%Y-%m-%dT%H:%M:%SZ'; }
log() { echo "$(ts) $*" >> "$LOG"; }

log "tick pid=$$ PATH=$PATH"

# --- socket/context resolution (works with no daemon running: this is pure
# client-side config parsing) ---
CTX=$("$DOCKER_BIN" context show 2>/dev/null)
EP=$("$DOCKER_BIN" context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null)
if [ -n "$EP" ]; then
  log "resolve context=$CTX endpoint=$EP"
else
  log "resolve context-inspect-failed fallback=unix://$FALLBACK_SOCK sock_exists=$([ -S "$FALLBACK_SOCK" ] && echo yes || echo no)"
fi

# --- daemon liveness with bounded exponential backoff (1s,2s,4s,8s) ---
attempt=1
delay=1
max_attempts=4
while :; do
  # FINDING: `docker info --format` (CLI 29.4.0) exits 0 even when the socket
  # answers 500 (daemon starting/stopping) — the error text lands in stdout.
  # Liveness must therefore be judged on the OUTPUT SHAPE, never the exit code.
  OUT=$("$DOCKER_BIN" info --format '{{.ServerVersion}}' 2>&1)
  rc=$?
  ok=no
  case "$OUT" in
    *" "*|*"
"*|"") ok=no ;;          # error text / multiline / empty
    [0-9]*) [ $rc -eq 0 ] && ok=yes ;;
  esac
  if [ "$ok" = yes ]; then
    log "docker OK server=$OUT attempt=$attempt"
    # VM-vs-host clock sample (only when daemon is up): epoch seconds as
    # reported by a container (busybox date) vs the host, bounded by the
    # round-trip. |skew| <= 1s + rtt is agreement at this resolution.
    H0=$(/bin/date +%s)
    C=$("$DOCKER_BIN" run --rm mcspike-05-base date +%s 2>/dev/null)
    H1=$(/bin/date +%s)
    [ -n "$C" ] && log "clock host_before=$H0 container=$C host_after=$H1 skew_bound=[$((C - H1)),$((C + 1 - H0))]s"
    exit 0
  fi
  ERR=$(echo "$OUT" | head -1 | cut -c1-120)
  log "docker DOWN attempt=$attempt next_delay=${delay}s err=$ERR"
  if [ "$attempt" -ge "$max_attempts" ]; then
    log "backoff exhausted for this tick; yielding to next StartInterval tick"
    exit 0
  fi
  sleep "$delay"
  attempt=$((attempt + 1))
  delay=$((delay * 2))
done
