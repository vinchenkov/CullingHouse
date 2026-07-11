#!/usr/bin/env bash
# Start the throwaway host-side MITM egress gateway (mitmproxy) for S4.
#
# Runs ONE mitmdump process with two listeners:
#   :8080  regular  -> HTTPS forward proxy w/ TLS interception (codex + claude legs)
#   :8081  reverse  -> https://api.minimax.io, header injected at edge, NO interception
#
# Signs intercepted leaf certs with the Mission Control Dev CA (confdir bundle
# built from ~/.mc-dev-home/ca). The MiniMax key is read at runtime from
# OPERATOR-INPUTS.md into this process's env and NEVER written to any file.
set -euo pipefail

SPIKE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK=/tmp/mcspike-04
CONFDIR="$WORK/mitm-confdir"
LOGDIR="$WORK/logs"
OPINPUTS="${OPINPUTS:-/Users/vinchenkov/Documents/dev/ai/homie/OPERATOR-INPUTS.md}"
FWD_PORT="${FWD_PORT:-8080}"
REV_PORT="${REV_PORT:-8081}"

mkdir -p "$LOGDIR"
: > "$LOGDIR/flows.jsonl"

# --- build the mitmproxy confdir from the MC dev CA (idempotent) ---------
# mitmproxy-ca.pem = key+cert bundle mitmproxy signs leaf certs with.
# HOST-SIDE ONLY: lives in /tmp, never mounted into any container (only
# ~/.mc-dev-home/ca/ca.crt is ever mounted).
CA_SRC="${CA_SRC:-$HOME/.mc-dev-home/ca}"
if [ ! -f "$CONFDIR/mitmproxy-ca.pem" ]; then
  mkdir -p "$CONFDIR"
  umask 077
  cat "$CA_SRC/ca.key" "$CA_SRC/ca.crt" > "$CONFDIR/mitmproxy-ca.pem"
  umask 022
  cp "$CA_SRC/ca.crt" "$CONFDIR/mitmproxy-ca-cert.pem"
  echo "gateway: built confdir bundle from $CA_SRC"
fi

# --- pull the MiniMax key into env at runtime (never onto disk in the repo) ---
MC_MINIMAX_KEY="$(grep -E '^MINIMAX_KEY=' "$OPINPUTS" | head -1 | cut -d= -f2-)"
if [ -z "$MC_MINIMAX_KEY" ]; then
  echo "FATAL: could not read MINIMAX_KEY from $OPINPUTS" >&2; exit 1
fi
export MC_MINIMAX_KEY
export MC_INJECT_AUTH=1
export MC_LOG_PATH="$LOGDIR/flows.jsonl"
echo "gateway: minimax key loaded, prefix=${MC_MINIMAX_KEY:0:8}...REDACTED"

# Kill any prior instance
pkill -f "mitmdump.*mcspike-04" 2>/dev/null || true
sleep 1

# NOTE: no stream_large_bodies — SSE responses are streamed explicitly by
# the addon's responseheaders hook (pass-through counter); everything else
# is small enough to buffer in a spike.
nohup uvx --from mitmproxy mitmdump \
  --set confdir="$CONFDIR" \
  --mode "regular@${FWD_PORT}" \
  --mode "reverse:https://api.minimax.io@${REV_PORT}" \
  -s "$SPIKE_DIR/gateway_addon.py" \
  --set termlog_verbosity=info \
  > "$LOGDIR/mitmdump.out" 2>&1 &

echo $! > "$WORK/gateway.pid"
echo "gateway: pid=$(cat "$WORK/gateway.pid") fwd=:$FWD_PORT rev=:$REV_PORT confdir=$CONFDIR"

# Wait for the forward-proxy port to accept connections
for i in $(seq 1 30); do
  if nc -z 127.0.0.1 "$FWD_PORT" 2>/dev/null; then echo "gateway: up on :$FWD_PORT"; exit 0; fi
  sleep 1
done
echo "FATAL: gateway did not come up" >&2
tail -20 "$LOGDIR/mitmdump.out" >&2 || true
exit 1
