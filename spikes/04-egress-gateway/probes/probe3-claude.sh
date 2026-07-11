#!/usr/bin/env bash
# S4 probe 3 — claude leg through the MITM forward proxy (:8080).
# Canonical claude cred dir (~/.mc-dev-home/cred/claude) bind-mounted as
# CLAUDE_CONFIG_DIR; ca.crt (cert ONLY, never ca.key) at /ca/ca.crt.
#
# CA trust MUST be in the REAL process env: NODE_EXTRA_CA_CERTS (trusted
# prior: settings-file CA vars are reported-but-not-honored — not re-derived
# here; we run the positive leg and the removed-CA fail-closed leg).
#
#   run A: NODE_EXTRA_CA_CERTS=/ca/ca.crt -> LIVE no-op turn, must PASS
#   run B: NODE_EXTRA_CA_CERTS=/ca/ca.crt -> LIVE small streaming turn, must PASS
#   run C: proxy set, CA NOT mounted, no CA env -> clean TLS failure, no fallback
set -euo pipefail

SPIKE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$SPIKE_DIR/out"; mkdir -p "$OUT"
FLOWS=/tmp/mcspike-04/logs/flows.jsonl
IMG="mcspike-08-base:latest"
CRED="${HOME}/.mc-dev-home/cred/claude"
CA="${HOME}/.mc-dev-home/ca/ca.crt"
LOG="$OUT/probe3-claude.log"; : > "$LOG"
[ -f "$CRED/.credentials.json" ] || { echo "no canonical claude .credentials.json"; exit 1; }
nc -z 127.0.0.1 8080 || { echo "FATAL: gateway :8080 not up (run start_gateway.sh)"; exit 1; }

run_claude() { # label with_ca prompt
  local label="$1" with_ca="$2" prompt="$3"
  local mounts=(-v "$CRED":/claude-cfg) envs=(-e CLAUDE_CONFIG_DIR=/claude-cfg \
    -e HTTPS_PROXY=http://host.docker.internal:8080 -e HTTP_PROXY=http://host.docker.internal:8080)
  if [ "$with_ca" = yes ]; then
    mounts+=(-v "$CA":/ca/ca.crt:ro); envs+=(-e NODE_EXTRA_CA_CERTS=/ca/ca.crt)
  fi
  echo "== claude turn ($label) ==" | tee -a "$LOG"
  set +e
  docker run --rm "${mounts[@]}" "${envs[@]}" -w /tmp "$IMG" bash -c '
    for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY CLAUDE_CODE_OAUTH_TOKEN; do
      [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
    done
    echo "env clean: no forbidden API keys; NODE_EXTRA_CA_CERTS=${NODE_EXTRA_CA_CERTS:-unset}"
    claude --version
    timeout 120 claude -p "'"$prompt"'" 2>&1 | tail -8
    exit ${PIPESTATUS[0]}
  ' 2>&1 | tee -a "$LOG"
  local rc=${PIPESTATUS[0]}
  set -e
  echo "exit=$rc ($label)" | tee -a "$LOG"
  return $rc
}

assert_stream() { # flows (SSE from api.anthropic.com with streamed events)
  printf '%s\n' "$1" | python3 -c '
import json,sys
ok=False
for l in sys.stdin:
    l=l.strip()
    if not l: continue
    r=json.loads(l)
    if r.get("event")=="response" and "anthropic.com" in (r.get("host") or "") \
       and r.get("status")==200 and r.get("sse") and (r.get("sse_events") or 0)>0 and r.get("streamed"):
        ok=True; print("  SSE evidence: host=%s path=%s events=%s bytes=%s" % (r["host"], r["path"], r["sse_events"], r["sse_bytes"]))
sys.exit(0 if ok else 1)'
}

FAIL=0

SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
run_claude "A-noop" yes "Reply with the single word OK and nothing else." \
  && echo "PASS run A (NODE_EXTRA_CA_CERTS in real env honored)" | tee -a "$LOG" \
  || { echo "FAIL run A" | tee -a "$LOG"; FAIL=1; }
NEW_A=$(tail -n +$((SNAP+1)) "$FLOWS")

SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
run_claude "B-streaming" yes "Count from 1 to 5, digits only, one per line." \
  && echo "PASS run B" | tee -a "$LOG" || { echo "FAIL run B" | tee -a "$LOG"; FAIL=1; }
NEW_B=$(tail -n +$((SNAP+1)) "$FLOWS")

SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
if run_claude "C-no-CA-fail-closed" no "Reply with the single word OK and nothing else."; then
  echo "FAIL run C: turn succeeded WITHOUT CA trust (silent fallback?)" | tee -a "$LOG"; FAIL=1
else
  echo "PASS run C failed as required" | tee -a "$LOG"
fi
NEW_C=$(tail -n +$((SNAP+1)) "$FLOWS")

echo "== flow evidence ==" | tee -a "$LOG"
printf '%s\n' "$NEW_A" "$NEW_B" "$NEW_C" | tee -a "$LOG" >/dev/null

echo "== assertions over gateway flows ==" | tee -a "$LOG"
assert_stream "$NEW_A" && echo "PASS run A streamed SSE through MITM" | tee -a "$LOG" || { echo "FAIL run A no SSE evidence" | tee -a "$LOG"; FAIL=1; }
assert_stream "$NEW_B" && echo "PASS run B streamed SSE through MITM" | tee -a "$LOG" || { echo "FAIL run B no SSE evidence" | tee -a "$LOG"; FAIL=1; }
if printf '%s\n' "$NEW_C" | grep -q '"tls_failed_client"'; then
  echo "PASS run C died at TLS handshake (tls_failed_client)" | tee -a "$LOG"
else
  echo "WARN run C: no tls_failed_client event; check client error text" | tee -a "$LOG"
fi
if printf '%s\n' "$NEW_C" | grep -qE '"event": "response".*anthropic.com.*"status": 200'; then
  echo "FAIL run C: a 200 from anthropic passed without CA trust" | tee -a "$LOG"; FAIL=1
else
  echo "PASS run C: no successful anthropic response crossed the gateway" | tee -a "$LOG"
fi

[ $FAIL -eq 0 ] && echo "PROBE3 PASS" | tee -a "$LOG" || { echo "PROBE3 FAIL" | tee -a "$LOG"; exit 1; }
