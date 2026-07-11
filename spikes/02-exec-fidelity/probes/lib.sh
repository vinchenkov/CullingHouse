# shellcheck shell=bash
# Shared helpers for S2 exec-fidelity probes. Sourced, not executed.
HELPER="${HELPER:-mcspike-02-helper}"
HELPER_IMG="${HELPER_IMG:-mcspike-02-helper-img:latest}"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; FAILED=1; }

# Ensure the helper container exists and is running (lazy recreate).
# This is the wrapper behavior probe 7 exercises.
ensure_helper() {
  local state
  state="$(docker inspect -f '{{.State.Running}}' "$HELPER" 2>/dev/null || true)"
  if [ "$state" = "true" ]; then
    return 0
  fi
  docker rm -f "$HELPER" >/dev/null 2>&1 || true
  docker run -d --name "$HELPER" \
    --label mc-managed=true --label mc-tier=helper \
    "$HELPER_IMG" sleep infinity >/dev/null
}

# mc stand-in: every host-side invocation funnels through docker exec into
# the warm helper, lazily recreating it first. Non-TTY, stdin attached.
mc_exec() {
  ensure_helper
  docker exec -i "$HELPER" "$@"
}
