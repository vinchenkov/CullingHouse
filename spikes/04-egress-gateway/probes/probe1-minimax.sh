#!/usr/bin/env bash
# S4 probe 1 — minimax static-token leg through the gateway's REVERSE listener
# (:8081 -> https://api.minimax.io). NO TLS interception on the client hop:
# the container speaks plain HTTP to the gateway, the gateway terminates and
# re-originates real TLS upstream. The Authorization header is injected AT THE
# GATEWAY; the container env carries a dummy bearer and NO real key.
#
# LIVE: one no-op turn + one small STREAMING (SSE) turn against MINIMAX_MODEL.
# This is the ADR-005 evidence: base_url routing + gateway header injection
# replaces MITM entirely for static-token bindings.
set -euo pipefail

SPIKE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$SPIKE_DIR/out"; mkdir -p "$OUT"
FLOWS=/tmp/mcspike-04/logs/flows.jsonl
IMG="mcspike-08-base:latest"
OPINPUTS="${OPINPUTS:-/Users/vinchenkov/Documents/dev/ai/homie/OPERATOR-INPUTS.md}"
MODEL="$(grep -E '^MINIMAX_MODEL=' "$OPINPUTS" | head -1 | cut -d= -f2- | awk '{print $1}')"
[ -n "$MODEL" ] || { echo "FATAL: MINIMAX_MODEL not found in OPERATOR-INPUTS.md"; exit 1; }
echo "model=$MODEL (name only; the key never leaves the gateway process)"

nc -z 127.0.0.1 8081 || { echo "FATAL: gateway reverse listener :8081 not up (run start_gateway.sh)"; exit 1; }
SNAP=$(wc -l < "$FLOWS" | tr -d ' ')

# ANTHROPIC_BASE_URL shape: the harness/base_url points at the gateway; path
# /anthropic mirrors OPERATOR-INPUTS' ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic
docker run --rm \
  -e MC_BASE=http://host.docker.internal:8081/anthropic \
  -e MC_MODEL="$MODEL" \
  -e ANTHROPIC_BASE_URL=http://host.docker.internal:8081/anthropic \
  "$IMG" bash -c '
    set -e
    # forbidden-env guard (Inv. 13) + prove no real key is present
    for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY; do
      [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
    done
    echo "env clean: no ANTHROPIC_API_KEY / CODEX_API_KEY / OPENAI_API_KEY"

    echo "== no-op turn (buffered POST, dummy bearer swapped at gateway) =="
    curl -sS -X POST "$MC_BASE/v1/messages" \
      -H "Authorization: Bearer dummy-not-a-real-key" \
      -H "content-type: application/json" \
      -d "{\"model\":\"$MC_MODEL\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with the single word OK and nothing else.\"}]}" \
      | head -c 600; echo

    echo "== streaming turn (SSE) =="
    curl -sS -N -X POST "$MC_BASE/v1/messages" \
      -H "Authorization: Bearer dummy-not-a-real-key" \
      -H "accept: text/event-stream" \
      -H "content-type: application/json" \
      -d "{\"model\":\"$MC_MODEL\",\"max_tokens\":32,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Count from 1 to 5, digits only.\"}]}" \
      | grep -c "^data:" | xargs -I{} echo "client saw {} SSE data events"
  ' 2>&1 | tee "$OUT/probe1-minimax.log"

echo "== gateway flow evidence (new lines only) =="
NEW=$(tail -n +$((SNAP+1)) "$FLOWS")
echo "$NEW" | tee -a "$OUT/probe1-minimax.log"

echo "== assertions =="
FAIL=0
echo "$NEW" | grep -q '"event": "inject".*api.minimax.io' \
  && echo "PASS inject-at-gateway logged" || { echo "FAIL no inject event"; FAIL=1; }
echo "$NEW" | python3 -c '
import json,sys
ok_noop=ok_sse=False
for l in sys.stdin:
    r=json.loads(l)
    if r.get("event")!="response" or "minimax" not in (r.get("host") or ""): continue
    if r.get("status")==200 and not r.get("sse"): ok_noop=True
    if r.get("status")==200 and r.get("sse") and (r.get("sse_events") or 0)>0 and r.get("streamed"): ok_sse=True
print("PASS no-op 200 via gateway" if ok_noop else "FAIL no-op"); print("PASS streamed SSE 200 with events" if ok_sse else "FAIL sse")
sys.exit(0 if ok_noop and ok_sse else 1)' || FAIL=1
grep -q "env clean" "$OUT/probe1-minimax.log" || { echo "FAIL forbidden-env guard did not run"; FAIL=1; }
[ $FAIL -eq 0 ] && echo "PROBE1 PASS" || { echo "PROBE1 FAIL"; exit 1; }
