#!/usr/bin/env bash
# S4 probe 2 — codex leg through the MITM forward proxy (:8080).
# Canonical codex cred dir (~/.mc-dev-home/cred/codex) bind-mounted as
# CODEX_HOME; ca.crt (cert ONLY, never ca.key) mounted at /ca/ca.crt.
#
# Matrix (pins the codex 0.144.1 custom-CA surface):
#   run A: CODEX_CA_CERTIFICATE=/ca/ca.crt (primary)      -> LIVE turn, must PASS
#   run B: SSL_CERT_FILE=/ca/ca.crt        (fallback)     -> LIVE turn, must PASS
#   run C: proxy set, NO CA var at all     (fail-closed)  -> must FAIL cleanly at TLS,
#          no silent direct-egress or metered fallback (no OPENAI_API_KEY exists to fall to).
set -euo pipefail

SPIKE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$SPIKE_DIR/out"; mkdir -p "$OUT"
FLOWS=/tmp/mcspike-04/logs/flows.jsonl
IMG="mcspike-08-base:latest"
CRED="${HOME}/.mc-dev-home/cred/codex"
CA="${HOME}/.mc-dev-home/ca/ca.crt"
LOG="$OUT/probe2-codex.log"; : > "$LOG"
[ -f "$CRED/auth.json" ] || { echo "no canonical codex auth.json"; exit 1; }
nc -z 127.0.0.1 8080 || { echo "FATAL: gateway :8080 not up (run start_gateway.sh)"; exit 1; }

run_codex() { # label extra-env... (passed as -e args)
  local label="$1"; shift
  local envargs=()
  for e in "$@"; do envargs+=(-e "$e"); done
  echo "== codex turn ($label) ==" | tee -a "$LOG"
  set +e
  docker run --rm \
    -v "$CRED":/codex-home \
    -v "$CA":/ca/ca.crt:ro \
    -e CODEX_HOME=/codex-home \
    -e HTTPS_PROXY=http://host.docker.internal:8080 \
    -e HTTP_PROXY=http://host.docker.internal:8080 \
    ${envargs[@]+"${envargs[@]}"} \
    -w /tmp "$IMG" bash -c '
      for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY; do
        [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
      done
      echo "env clean: no forbidden API keys; CODEX_CA_CERTIFICATE=${CODEX_CA_CERTIFICATE:-unset} SSL_CERT_FILE=${SSL_CERT_FILE:-unset}"
      codex --version
      timeout 120 codex exec --skip-git-repo-check --sandbox read-only \
        "Reply with the single word OK and nothing else." 2>&1 | tail -15
      exit ${PIPESTATUS[0]}
    ' 2>&1 | tee -a "$LOG"
  local rc=${PIPESTATUS[0]}
  set -e
  echo "exit=$rc ($label)" | tee -a "$LOG"
  return $rc
}

FAIL=0

SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
run_codex "A-CODEX_CA_CERTIFICATE" CODEX_CA_CERTIFICATE=/ca/ca.crt \
  && echo "PASS run A (CODEX_CA_CERTIFICATE honored @ codex 0.144.1)" | tee -a "$LOG" \
  || { echo "FAIL run A" | tee -a "$LOG"; FAIL=1; }
NEW_A=$(tail -n +$((SNAP+1)) "$FLOWS")

SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
run_codex "B-SSL_CERT_FILE-fallback" SSL_CERT_FILE=/ca/ca.crt \
  && echo "PASS run B (SSL_CERT_FILE fallback honored)" | tee -a "$LOG" \
  || { echo "FAIL run B" | tee -a "$LOG"; FAIL=1; }
NEW_B=$(tail -n +$((SNAP+1)) "$FLOWS")

SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
if run_codex "C-no-CA-fail-closed"; then
  echo "FAIL run C: turn succeeded WITHOUT CA trust (silent fallback?)" | tee -a "$LOG"; FAIL=1
else
  echo "PASS run C failed as required" | tee -a "$LOG"
fi
NEW_C=$(tail -n +$((SNAP+1)) "$FLOWS")

echo "== flow evidence ==" | tee -a "$LOG"
printf '%s\n' "$NEW_A" "$NEW_B" "$NEW_C" | tee -a "$LOG" >/dev/null

echo "== assertions over gateway flows ==" | tee -a "$LOG"
# Streaming evidence: codex 0.144.1 streams turns over a WebSocket
# (GET /backend-api/codex/responses -> 101 upgrade), with SSE as its own
# fallback transport. Accept either, but demand real streamed messages.
assert_stream() { # flows
  printf '%s\n' "$1" | python3 -c '
import json,sys
ok=False
for l in sys.stdin:
    l=l.strip()
    if not l: continue
    r=json.loads(l)
    if r.get("event")=="response" and r.get("status")==200 and r.get("sse") and (r.get("sse_events") or 0)>0 and r.get("streamed"):
        ok=True; print("  SSE evidence: host=%s events=%s bytes=%s" % (r["host"], r["sse_events"], r["sse_bytes"]))
    if r.get("event")=="websocket_end" and (r.get("ws_messages") or 0)>0:
        ok=True; print("  WebSocket evidence: host=%s path=%s messages=%s bytes=%s" % (r["host"], r["path"], r["ws_messages"], r["ws_bytes"]))
sys.exit(0 if ok else 1)'
}
assert_stream "$NEW_A" && echo "PASS run A streamed through MITM (ws or sse)" | tee -a "$LOG" || { echo "FAIL run A no streaming evidence" | tee -a "$LOG"; FAIL=1; }
assert_stream "$NEW_B" && echo "PASS run B streamed through MITM (ws or sse)" | tee -a "$LOG" || { echo "FAIL run B no streaming evidence" | tee -a "$LOG"; FAIL=1; }
if printf '%s\n' "$NEW_C" | grep -q '"tls_failed_client"'; then
  echo "PASS run C died at TLS handshake (tls_failed_client), nothing forwarded upstream" | tee -a "$LOG"
else
  echo "WARN run C: no tls_failed_client event; check client-side error text" | tee -a "$LOG"
fi
if printf '%s\n' "$NEW_C" | grep -q '"event": "response".*"status": 200'; then
  echo "FAIL run C: a 200 passed the gateway without CA trust" | tee -a "$LOG"; FAIL=1
else
  echo "PASS run C: no successful response crossed the gateway" | tee -a "$LOG"
fi

[ $FAIL -eq 0 ] && echo "PROBE2 PASS" | tee -a "$LOG" || { echo "PROBE2 FAIL" | tee -a "$LOG"; exit 1; }
