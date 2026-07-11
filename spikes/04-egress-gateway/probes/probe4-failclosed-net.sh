#!/usr/bin/env bash
# S4 probe 4 — fail-closed network shape: direct (non-proxy) egress refused.
#
# Mechanism (this exact shape is what Phase 3's gateway probe promotes):
#   1. docker network create --internal mcspike-04-int
#      -> containers on it get NO route to the host, the LAN, or the internet;
#         Docker's embedded DNS still resolves container names on the network.
#   2. A dual-homed relay container (alpine/socat) sits on BOTH the internal
#      network and the default bridge, forwarding TCP :8080 to the host-side
#      gateway via host.docker.internal (mapped with --add-host host-gateway).
#      The relay holds no secrets and no CA key — it is a dumb pipe; the
#      MITM/injection gateway itself stays host-side, where ca.key lives.
#   3. Agent containers run ONLY on the internal network with
#      HTTPS_PROXY=http://mcspike-04-relay:8080 — the single reachable egress.
#
# Checks:
#   a. direct DNS+TCP egress from the internal net FAILS (api.anthropic.com)
#   b. direct-by-IP egress FAILS too (1.1.1.1:443 — no DNS involved)
#   c. proxied curl through relay->gateway with the mounted CA works (404 root)
#   d. LIVE: one tiny claude no-op turn through relay->gateway succeeds
#   e. forbidden-env scan in the agent container
set -euo pipefail

SPIKE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$SPIKE_DIR/out"; mkdir -p "$OUT"
FLOWS=/tmp/mcspike-04/logs/flows.jsonl
IMG="mcspike-08-base:latest"
CRED="${HOME}/.mc-dev-home/cred/claude"
CA="${HOME}/.mc-dev-home/ca/ca.crt"
NET=mcspike-04-int
RELAY=mcspike-04-relay
LOG="$OUT/probe4-failclosed-net.log"; : > "$LOG"
nc -z 127.0.0.1 8080 || { echo "FATAL: gateway :8080 not up (run start_gateway.sh)"; exit 1; }

cleanup() {
  docker rm -f "$RELAY" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
cleanup
trap cleanup EXIT

docker network create --internal "$NET" >/dev/null
echo "created network $NET (internal=$(docker network inspect -f '{{.Internal}}' "$NET"))" | tee -a "$LOG"

docker run -d --name "$RELAY" --network bridge \
  --add-host=host.docker.internal:host-gateway \
  alpine/socat tcp-listen:8080,fork,reuseaddr tcp:host.docker.internal:8080 >/dev/null
docker network connect "$NET" "$RELAY"
echo "relay $RELAY up: bridge + $NET, forwards :8080 -> host gateway" | tee -a "$LOG"

FAIL=0
SNAP=$(wc -l < "$FLOWS" | tr -d ' ')

echo "== agent container on INTERNAL-ONLY network ==" | tee -a "$LOG"
set +e
docker run --rm --network "$NET" \
  -v "$CRED":/claude-cfg -v "$CA":/ca/ca.crt:ro \
  -e CLAUDE_CONFIG_DIR=/claude-cfg \
  -e NODE_EXTRA_CA_CERTS=/ca/ca.crt \
  -e HTTPS_PROXY=http://$RELAY:8080 -e HTTP_PROXY=http://$RELAY:8080 \
  -w /tmp "$IMG" bash -c '
    for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY CLAUDE_CODE_OAUTH_TOKEN; do
      [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
    done
    echo "env clean: no forbidden API keys"
    rc=0

    echo "-- a. direct egress by name (must fail)"
    out=$(curl -sS --noproxy "*" --max-time 8 https://api.anthropic.com/ 2>&1)
    if [ $? -ne 0 ]; then echo "PASS direct-by-name refused: $(echo "$out" | head -1)"; else echo "FAIL direct-by-name got through"; rc=1; fi

    echo "-- b. direct egress by IP (must fail)"
    out=$(curl -sS --noproxy "*" --max-time 8 https://1.1.1.1/ 2>&1)
    if [ $? -ne 0 ]; then echo "PASS direct-by-ip refused: $(echo "$out" | head -1)"; else echo "FAIL direct-by-ip got through"; rc=1; fi

    echo "-- c. proxied curl through relay -> gateway (must succeed)"
    code=$(curl -sS -o /dev/null -w "%{http_code}" --cacert /ca/ca.crt --max-time 20 https://api.anthropic.com/ 2>&1)
    if [ "$code" = 404 ]; then echo "PASS proxied path reachable (status=$code)"; else echo "FAIL proxied path (status=$code)"; rc=1; fi

    echo "-- d. LIVE claude no-op turn through relay -> gateway"
    reply=$(timeout 120 claude -p "Reply with the single word OK and nothing else." 2>&1 | tail -3)
    trc=$?
    echo "$reply"
    if [ $trc -eq 0 ]; then echo "PASS live turn on internal-only network"; else echo "FAIL live turn (exit=$trc)"; rc=1; fi
    exit $rc
  ' 2>&1 | tee -a "$LOG"
RC=${PIPESTATUS[0]}
set -e
[ "$RC" -eq 0 ] || FAIL=1

NEW=$(tail -n +$((SNAP+1)) "$FLOWS")
echo "== flow evidence ==" | tee -a "$LOG"
printf '%s\n' "$NEW" | tee -a "$LOG" >/dev/null

printf '%s\n' "$NEW" | python3 -c '
import json,sys
ok=False
for l in sys.stdin:
    l=l.strip()
    if not l: continue
    r=json.loads(l)
    if r.get("event")=="response" and "anthropic.com" in (r.get("host") or "") \
       and r.get("status")==200 and r.get("sse") and (r.get("sse_events") or 0)>0:
        ok=True; print("  SSE evidence via relay: host=%s events=%s bytes=%s" % (r["host"], r["sse_events"], r["sse_bytes"]))
sys.exit(0 if ok else 1)' \
  && echo "PASS live turn streamed SSE via relay->gateway" | tee -a "$LOG" \
  || { echo "FAIL no SSE evidence for the relay-path turn" | tee -a "$LOG"; FAIL=1; }

[ $FAIL -eq 0 ] && echo "PROBE4 PASS" | tee -a "$LOG" || { echo "PROBE4 FAIL" | tee -a "$LOG"; exit 1; }
